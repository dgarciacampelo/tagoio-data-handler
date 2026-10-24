package handlers

// Contract tests with the two services that call this one, using the bodies they really send:
//   - neos-ocpp: tagoio_data_handler.py (send_charging_session_update, ChargingSessionUpdate.dict());
//   - the public station page, which must not reach TagoIO for pools the service doesn't know.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

// neosSessionUpdate is a ChargingSessionUpdate.dict() as neos-ocpp posts it, for the given step.
func neosSessionUpdate(step, stopMotive, timeBand string) string {
	optional := func(s string) string {
		if s == "" {
			return "null"
		}
		return `"` + s + `"`
	}
	return `{"pool_code": 261001, "station_name": "22HBS0452HBS22", "connector_id": 1, "transaction_id": 4321,
		"card_alias": "Pago QR", "card_code": "1261001tggcdt22hbs22", "display_id": "22HBS0452HBS22 [1]",
		"start_date": "21/10/2026", "start_time": "09:00", "step": "` + step + `",
		"start_meter_value": 10000, "last_meter_value": 22345, "last_meter_ts": "2026-10-21T10:15:30.123456+02:00",
		"current_tariff_band": "Peak", "rate_off_peak": 0.25, "rate_flat": 0.35, "rate_peak": 0.45,
		"energy_off_peak": 0, "energy_flat": 1000, "energy_peak": 11345, "energy": 12.345, "energy_unit": "KWh",
		"cost": 5.46, "cost_unit": "€", "power": 7360, "power_unit": "W", "time": "1 h 15 min",
		"with_payment": true, "has_public_dashboard": true,
		"stop_motive": ` + optional(stopMotive) + `, "time_band": ` + optional(timeBand) + `}`
}

func TestChargingSessionUpdateContract(t *testing.T) {
	app := newTestApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/charging-session-update/{pool_code}/{station_name}/{connector_id}", app.ChargingSessionUpdate)
	key := chargepoints.ConnectorKey{PoolCode: 261001, StationName: "22HBS0452HBS22", ConnectorID: 1}
	const target = "/v1/charging-session-update/261001/22HBS0452HBS22/1"

	post := func(body string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, serviceRequest(http.MethodPost, target, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		var answer map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &answer)
		return answer
	}

	// A running session: the station page offers the stop button of a paid session
	if answer := post(neosSessionUpdate("INPROGRESS", "", "")); answer["status"] != "success" {
		t.Fatalf("answer = %v", answer)
	}
	session, active := app.ChargePoints.ActiveSession(key)
	if !active || !session.WithPayment || int64(session.TransactionID) != 4321 {
		t.Fatalf("active session = %+v (active: %v)", session, active)
	}

	// The end of the session, with the fields neos-ocpp adds only then
	post(neosSessionUpdate("COMPLETED", "Cost", "09:00 - 10:15"))
	if _, active := app.ChargePoints.ActiveSession(key); active {
		t.Fatal("the session is still active after its COMPLETED update")
	}
}

func TestChargingSessionUpdateRejectsMalformedBodies(t *testing.T) {
	app := newTestApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/charging-session-update/{pool_code}/{station_name}/{connector_id}", app.ChargingSessionUpdate)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, serviceRequest(http.MethodPost, "/v1/charging-session-update/261001/ST/1", `{"transaction_id": "x"`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("truncated body: status = %d", rec.Code)
	}
}

// countingReader is a pools.Reader that counts the TagoIO reads and fails them, as an unknown pool would.
type countingReader struct{ reads atomic.Int32 }

func (c *countingReader) LastValue(context.Context, int, string) (*tagoio.DataRecord, error) {
	c.reads.Add(1)
	return nil, context.DeadlineExceeded
}

func (c *countingReader) List(context.Context, int, string, int) ([]tagoio.DataRecord, error) {
	c.reads.Add(1)
	return nil, context.DeadlineExceeded
}

func TestStationPageOfAnUnknownPoolDoesNotReadTagoIO(t *testing.T) {
	// The station page is public and takes any 1-9 digit pool code. In production, each read of an unknown pool
	// rescans the whole TagoIO account (devices.Registry.ByPool), so bots walking codes cost account-wide calls.
	app := newTestApp(t)
	reader := &countingReader{}
	app.Pools = pools.NewCache(reader, app.Log)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/station/{pool_code}/{station_name}", app.StationLiveAPI)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/station/999999/XMLRPC", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"config_loaded":false`) {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	testutil.KnownBug(t, reader.reads.Load() == 0,
		"the public station page loads the configuration of any pool code, reaching TagoIO (and a full account rescan) for unknown pools")
}
