package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

// sessionsMux serves the session pages and the payments API of app, rendering the page shells (in development mode,
// so no build is needed) to read their view model.
func sessionsMux(t *testing.T, app *App) *http.ServeMux {
	t.Helper()
	ui, err := webui.New(webui.Config{DevServerURL: "http://localhost:5173"})
	if err != nil {
		t.Fatal(err)
	}
	app.UI = ui
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emsp-dashboard/sessions/{transaction_id}", app.SessionByTransactionPage)
	mux.HandleFunc("GET /emsp-dashboard/sessions/{pool_code}/{station_name}/{connector_id}/{transaction_id}", app.SessionPage)
	mux.HandleFunc("GET /api/admin/sessions/{pool_code}/{station_name}/{connector_id}/{transaction_id}/payments", app.SessionPaymentsAPI)
	return mux
}

// sessionView requests a session page and returns its view model.
func sessionView(t *testing.T, mux *http.ServeMux, target string) *dto.SessionPage {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", target, rec.Code, rec.Body)
	}
	_, rest, _ := strings.Cut(rec.Body.String(), `<script id="bootstrap" type="application/json">`)
	data, _, found := strings.Cut(rest, "</script>")
	if !found {
		t.Fatalf("GET %s: no bootstrap in the page", target)
	}
	var bootstrap struct {
		Page *dto.SessionPage `json:"page"`
	}
	if err := json.Unmarshal([]byte(data), &bootstrap); err != nil {
		t.Fatalf("bootstrap %s: %v", data, err)
	}
	return bootstrap.Page
}

// addSharedTransaction stores transaction 4321 at two charge points of a made-up pool, each with its own meter
// values, and transaction 4322 at one of them.
func addSharedTransaction(t *testing.T, app *App) {
	t.Helper()
	ctx := context.Background()
	start := time.Date(2026, 11, 4, 19, 0, 0, 0, time.FixedZone("CET", 3600))
	sessions := []store.SessionRecord{
		{StationName: "ST1", TransactionID: 4321, LastMeterValue: 3000, StopMotive: "CAR"},
		{StationName: "ST2", TransactionID: 4321, LastMeterValue: 9000, StopMotive: "USER"},
		{StationName: "ST1", TransactionID: 4322, StartMeterValue: 3000, LastMeterValue: 4000},
	}
	for _, s := range sessions {
		s.PoolCode, s.ConnectorID, s.CardAlias, s.StartDate, s.TimeBand = 990001, 1, "Pago QR", "04/11/2026", "19:00 - 19:30"
		if _, err := app.Store.InsertSession(ctx, s); err != nil {
			t.Fatal(err)
		}
		for m := 1; m <= 30; m++ {
			energy := (s.LastMeterValue - s.StartMeterValue) * int64(m) / 30
			err := app.Store.InsertTelemetry(ctx, store.TelemetryRecord{PoolCode: 990001, StationName: s.StationName,
				ConnectorID: 1, TransactionID: s.TransactionID,
				Timestamp:  start.Add(time.Duration(m) * time.Minute).Format("2006-01-02T15:04:05.000000-07:00"),
				MeterValue: s.StartMeterValue + energy, Power: 6000, CurrentTariffBand: "Peak", EnergyPeak: energy})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSessionPageShowsTheSessionOfItsChargePoint(t *testing.T) {
	app := newTestApp(t)
	app.Config.Location = time.FixedZone("CET", 3600)
	addSharedTransaction(t, app)
	mux := sessionsMux(t, app)

	view := sessionView(t, mux, "/emsp-dashboard/sessions/990001/ST2/1/4321")
	if view == nil || view.Session == nil {
		t.Fatalf("view = %+v; want the session", view)
	}
	s := view.Session
	if s.StationName != "ST2" || s.EnergyWh != 9000 || s.StopMotive != "USER" || s.MeterValueCount != 30 {
		t.Errorf("session %s: %d Wh, stopped by %s, %d meter values; want ST2's: 9000 Wh, USER, 30", s.StationName,
			s.EnergyWh, s.StopMotive, s.MeterValueCount)
	}
	if last := s.MeterValues[len(s.MeterValues)-1]; last.PeakWh != 9000 {
		t.Errorf("last meter value %+v; want ST2's own (9000 Wh), not ST1's", last)
	}
	if s.Series == nil || s.StartedAt == nil || *s.StartedAt != "2026-11-04T19:00:00+01:00" {
		t.Errorf("series %v, started %v", s.Series != nil, s.StartedAt)
	}
	if len(view.Matches) != 0 {
		t.Errorf("matches = %d; want none when the session is found", len(view.Matches))
	}
}

func TestSessionPageOfAnotherChargePointListsTheTransaction(t *testing.T) {
	app := newTestApp(t)
	addSharedTransaction(t, app)
	mux := sessionsMux(t, app)

	view := sessionView(t, mux, "/emsp-dashboard/sessions/990001/ST9/1/4321")
	if view == nil || view.Session != nil || view.TransactionID != 4321 || len(view.Matches) != 2 {
		t.Fatalf("view = %+v; want no session and the two of transaction 4321", view)
	}
	for _, m := range view.Matches {
		if want := "/emsp-dashboard/sessions/990001/" + m.StationName + "/1/4321"; m.SessionURL != want {
			t.Errorf("match url %s; want %s", m.SessionURL, want)
		}
	}
}

func TestSessionByTransactionGoesToItsOnlySession(t *testing.T) {
	app := newTestApp(t)
	addSharedTransaction(t, app)
	mux := sessionsMux(t, app)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/emsp-dashboard/sessions/4322", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/emsp-dashboard/sessions/990001/ST1/1/4322" {
		t.Fatalf("one session: status %d, location %q; want a redirect to it", rec.Code, rec.Header().Get("Location"))
	}

	if view := sessionView(t, mux, "/emsp-dashboard/sessions/4321"); view.Session != nil || len(view.Matches) != 2 {
		t.Errorf("shared id: %+v; want the list of its two sessions", view)
	}
	if view := sessionView(t, mux, "/emsp-dashboard/sessions/9999"); view.Session != nil || len(view.Matches) != 0 || view.TransactionID != 9999 {
		t.Errorf("unknown id: %+v; want an empty list", view)
	}
	for _, bad := range []string{"abc", "0", "-3"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/emsp-dashboard/sessions/"+bad, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("/emsp-dashboard/sessions/%s: status %d; want 404", bad, rec.Code)
		}
	}
}

// csmsSessionPayments is a CSMS's GET /api/payments answer around transaction 4321: its own payment, one of the same
// transaction id at another station, and one whose preauth failed before any session.
const csmsSessionPayments = `{"payments": [
	{"id": 31, "created_at": "2026-11-04T17:58:40+00:00", "updated_at": "2026-11-04T18:31:00+00:00",
	 "pool_code": 990001, "station_name": "ST1", "connector_id": 1, "platform": "PAYCOMET", "category": "LIVE",
	 "status": "COMMITTED", "limit": 4000, "amount": 105, "currency": "EUR", "transaction_id": 4321,
	 "session_energy": 3000, "session_cost": "1.05", "order": "ORDER-A", "card_brand": "VISA", "terminal": null,
	 "error_stage": null, "error_code": null, "error_message": null, "error_at": null},
	{"id": 32, "created_at": "2026-11-04T17:59:00+00:00", "updated_at": "2026-11-04T18:31:00+00:00",
	 "pool_code": 990001, "station_name": "ST2", "connector_id": 1, "platform": "PAYCOMET", "category": "LIVE",
	 "status": "COMMITTED", "limit": 4000, "amount": 315, "currency": "EUR", "transaction_id": 4321,
	 "session_energy": 9000, "session_cost": "3.15", "order": "ORDER-B", "card_brand": "VISA", "terminal": null,
	 "error_stage": null, "error_code": null, "error_message": null, "error_at": null},
	{"id": 33, "created_at": "2026-11-04T17:50:00+00:00", "updated_at": "2026-11-04T17:50:02+00:00",
	 "pool_code": 990001, "station_name": "ST1", "connector_id": 1, "platform": "PAYCOMET", "category": "LIVE",
	 "status": "ERROR", "limit": 4000, "amount": null, "currency": "EUR", "transaction_id": null,
	 "session_energy": null, "session_cost": null, "order": "ORDER-C", "card_brand": null, "terminal": null,
	 "error_stage": "PREAUTH", "error_code": 111, "error_message": "Refused", "error_at": "2026-11-04T17:50:02+00:00"}
], "truncated": false}`

func TestSessionPaymentsKeepTheSessionsOwn(t *testing.T) {
	app, query := paymentsApp(t, http.StatusOK, csmsSessionPayments)
	addSharedTransaction(t, app)
	mux := sessionsMux(t, app)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/sessions/990001/ST1/1/4321/payments", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var result dto.SessionPayments
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Payments) != 1 || result.Payments[0].ID != 31 || !strings.HasPrefix(result.Source, "127.0.0.1:") {
		t.Fatalf("payments %+v from %s; want only payment 31", result.Payments, result.Source)
	}

	// The pool's payments from the day before the start (Madrid) to the end of the start day
	values, _ := url.ParseQuery(*query)
	if values.Get("pool_code") != "990001" || values.Get("since") != "2026-11-03T00:00:00+01:00" ||
		values.Get("until") != "2026-11-05T00:00:00+01:00" {
		t.Errorf("CSMS query %s", *query)
	}
}

func TestSessionPaymentsErrors(t *testing.T) {
	app, _ := paymentsApp(t, http.StatusOK, csmsSessionPayments)
	addSharedTransaction(t, app)
	mux := sessionsMux(t, app)
	get := func(target string) int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Code
	}

	if code := get("/api/admin/sessions/990001/ST9/1/4321/payments"); code != http.StatusNotFound {
		t.Errorf("unknown session: %d; want 404", code)
	}
	if code := get("/api/admin/sessions/990001/ST1/x/4321/payments"); code != http.StatusBadRequest {
		t.Errorf("non-integer connector: %d; want 400", code)
	}

	app.CSMS = nil
	if code := get("/api/admin/sessions/990001/ST1/1/4321/payments"); code != http.StatusServiceUnavailable {
		t.Errorf("without CSMS_URLS: %d; want 503", code)
	}
}
