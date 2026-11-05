package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

func TestSessionSeriesContract(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{pool_code}/{station_name}/{connector_id}/{transaction_id}/series", app.SessionSeries)

	// Transaction 4321 at two charge points: ST1 charges at 6 kW for 30 min (a meter value a minute, with the pool's
	// offset, as neos-ocpp sends them); ST2's meter values must not reach ST1's series
	madrid := time.FixedZone("CET", 3600)
	start := time.Date(2026, 11, 4, 19, 0, 0, 0, madrid)
	for m := 1; m <= 30; m++ {
		for _, station := range []string{"ST1", "ST2"} {
			energy := int64(100 * m)
			if station == "ST2" {
				energy *= 5
			}
			err := app.Store.InsertTelemetry(ctx, store.TelemetryRecord{PoolCode: 261001, StationName: station, ConnectorID: 1,
				TransactionID: 4321, Timestamp: start.Add(time.Duration(m) * time.Minute).Format("2006-01-02T15:04:05.000000-07:00"),
				MeterValue: 50_000 + energy, Power: 6000, CurrentTariffBand: "Peak", EnergyFlat: energy / 2, EnergyPeak: energy - energy/2})
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	get := func(target string) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, serviceRequest(http.MethodGet, target, ""))
		var answer map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatalf("%s: %d, not JSON: %s", target, rec.Code, rec.Body)
		}
		return rec.Code, answer
	}
	const path = "/api/sessions/261001/ST1/1/4321/series"

	// Only the stored meter values
	code, answer := get(path)
	if code != http.StatusOK || answer["start"] != "2026-11-04T18:01:00Z" || answer["step"] != 60.0 {
		t.Fatalf("stored only: %d %v", code, answer)
	}
	power, energy := answer["power"].([]any), answer["energy"].([]any)
	if len(power) != 29 || power[0] != 6000.0 || energy[28] != 3000.0 {
		t.Fatalf("stored only: power %v, energy %v; want 29 minutes of 6 kW up to 3000 Wh", power, energy)
	}

	// With the session's start and the final reading the telemetry doesn't hold yet (neos-ocpp sends UTC)
	query := url.Values{"started_at": {"2026-11-04T18:00:00Z"}, "stopped_at": {"2026-11-04T18:31:30Z"}, "energy": {"3150"}}
	code, answer = get(path + "?" + query.Encode())
	power, energy = answer["power"].([]any), answer["energy"].([]any)
	if code != http.StatusOK || answer["start"] != "2026-11-04T17:59:30Z" || len(power) != 32 {
		t.Fatalf("with the added readings: %d %v; want 32 minutes up to the stop", code, answer)
	}
	if energy[31] != 3150.0 || power[31] != 6000.0 {
		t.Errorf("last step: power %v, energy %v; want 6000 W and the final 3150 Wh", power[31], energy[31])
	}

	for name, target := range map[string]string{
		"a transaction without telemetry": "/api/sessions/261001/ST1/1/9999/series",
		"another connector":               "/api/sessions/261001/ST1/2/4321/series",
	} {
		if code, answer := get(target); code != http.StatusNotFound || answer["code"] != "not_found" {
			t.Errorf("%s: %d %v; want 404 not_found", name, code, answer)
		}
	}
	for name, query := range map[string]string{
		"a timestamp without offset":   "started_at=2026-11-04T18:00:00",
		"a stop without its energy":    "stopped_at=2026-11-04T18:31:30Z",
		"energy without its stop":      "energy=3150",
		"a negative energy":            "stopped_at=2026-11-04T18:31:30Z&energy=-1",
		"a non-integer transaction id": "",
	} {
		target := path + "?" + query
		if query == "" {
			target = "/api/sessions/261001/ST1/1/abc/series"
		}
		if code, answer := get(target); code != http.StatusBadRequest || answer["code"] != "invalid_request" {
			t.Errorf("%s: %d %v; want 400 invalid_request", name, code, answer)
		}
	}
}
