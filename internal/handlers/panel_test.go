package handlers

// Tests of the embedded management panel, against the fake TagoIO API: the session (the group_id rule), the
// overview, the changes (through the pool actions, as events for the CSMS and bucket records), the history and the
// live stream.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/jsonx"
	"github.com/dgarciacampelo/tagoio-handler/internal/panelauth"
	"github.com/dgarciacampelo/tagoio-handler/internal/poolactions"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

// region Harness

const (
	labDevice   = "637f4de7e7223300208aa890" // Pool 220001
	labToken    = "lab-device-token"
	otherDevice = "6304b5ee1ffc9500130e0b66" // Pool 221006
)

// fakeIdentities answers the identity calls of panelauth: a CPO of 220001, a CPO of 221006 and the account.
type fakeIdentities struct{}

func (fakeIdentities) RunUserID(_ context.Context, _, token string) (string, error) {
	switch token {
	case "cpo-220001":
		return "user-lab", nil
	case "cpo-221006":
		return "user-other", nil
	}
	return "", &tagoio.APIError{StatusCode: 401, Message: "Authorization denied"}
}

func (fakeIdentities) AccountID(_ context.Context, token string) (string, error) {
	if token == "admin" {
		return "account", nil
	}
	return "", &tagoio.APIError{StatusCode: 400, Message: "Invalid token"}
}

func (fakeIdentities) OwnAccountID(context.Context) (string, error) { return "account", nil }

func (fakeIdentities) User(_ context.Context, id string) (tagoio.RunUser, error) {
	group := map[string]string{"user-lab": "220001", "user-other": "221006"}[id]
	return tagoio.RunUser{ID: id, Active: true, Tags: []tagoio.Tag{{Key: "group_id", Value: group}}}, nil
}

type panelHarness struct {
	app      *App
	tago     *testutil.FakeTagoIO
	handler  http.Handler
	events   <-chan events.Message
	stations *chargepoints.Service
}

func newPanelHarness(t *testing.T) *panelHarness {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "panel.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	fake := testutil.NewFakeTagoIO(t)
	fake.AddDevice(labDevice, "MASTER-BUSINESS-220001", labToken)
	fake.AddDevice(otherDevice, "MASTER-BUSINESS-221006", "other-device-token")
	client := fake.Client()
	registry := devices.NewRegistry(db, client, "MASTER-", log)
	if err := registry.Load(ctx); err != nil {
		t.Fatal(err)
	}

	stations := chargepoints.New(db, noBucket{}, log)
	buckets := bucket.New(client, registry, stations, log)
	configs := pools.NewCache(buckets, log)
	broker := events.NewBroker(log, false)
	sub, unsubscribe := broker.Subscribe("")
	t.Cleanup(unsubscribe)

	madrid, _ := time.LoadLocation("Europe/Madrid")
	cfg := &config.Config{CookieSecret: []byte("0123456789abcdef0123456789abcdef"), Location: madrid}
	app := New(Deps{
		Log: log, Config: cfg, Store: db, Broker: broker, ChargePoints: stations, Pools: configs, Devices: registry,
		Bucket: buckets, UI: webui.Disabled(errors.New("no UI in tests")), Jobs: ctx,
		Actions:   poolactions.New(buckets, configs, broker, log),
		PanelAuth: panelauth.New(fakeIdentities{}, "neos.tago.run", cfg.CookieSecret, log),
		PanelData: buckets,
	})

	for _, u := range []chargepoints.StatusUpdate{
		{PoolCode: 220001, StationName: "ST1", ConnectorID: jsonx.Int(1), ConnectionStatus: "Online", ChargePointStatus: "Available"},
		{PoolCode: 220001, StationName: "ST1", ConnectorID: jsonx.Int(2), ConnectionStatus: "Online", ChargePointStatus: "Charging"},
		{PoolCode: 220001, StationName: "ST2", ConnectorID: jsonx.Int(1), ConnectionStatus: "Offline", ChargePointStatus: ""},
		{PoolCode: 221006, StationName: "OTHER", ConnectorID: jsonx.Int(1), ConnectionStatus: "Online", ChargePointStatus: "Available"},
	} {
		if _, err := stations.UpdateStatus(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/panel/session", app.PanelSessionAPI)
	mux.HandleFunc("GET /api/panel/{device_id}/overview", app.PanelOverviewAPI)
	mux.HandleFunc("GET /api/panel/{device_id}/stream", app.PanelStreamAPI)
	mux.HandleFunc("GET /api/panel/{device_id}/history", app.PanelHistoryAPI)
	mux.HandleFunc("GET /api/panel/{device_id}/history.csv", app.PanelHistoryCSVAPI)
	mux.HandleFunc("GET /api/panel/{device_id}/cards", app.PanelCardsAPI)
	mux.HandleFunc("POST /api/panel/{device_id}/cards", app.PanelCreateCardAPI)
	mux.HandleFunc("PUT /api/panel/{device_id}/cards/{card_id}", app.PanelUpdateCardAPI)
	mux.HandleFunc("DELETE /api/panel/{device_id}/cards/{card_id}", app.PanelDeleteCardAPI)
	mux.HandleFunc("PUT /api/panel/{device_id}/rates", app.PanelRatesAPI)
	mux.HandleFunc("PUT /api/panel/{device_id}/max-power", app.PanelMaxPowerAPI)
	mux.HandleFunc("PUT /api/panel/{device_id}/load-balancing-mode", app.PanelModeAPI)
	mux.HandleFunc("PUT /api/panel/{device_id}/cpo", app.PanelCPOAPI)
	mux.HandleFunc("POST /api/panel/{device_id}/stations/{station}/reset", app.PanelResetAPI)
	return &panelHarness{app: app, tago: fake, handler: mux, events: sub.Events, stations: stations}
}

// do sends a panel request; session is the bearer session ("" for none).
func (h *panelHarness) do(method, target, session, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if session != "" {
		r.Header.Set("Authorization", "Bearer "+session)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w
}

// session starts a panel session of a token on a device, failing the test unless it is granted.
func (h *panelHarness) session(t *testing.T, token, device string) string {
	t.Helper()
	w := h.do(http.MethodPost, "/api/panel/session", "", `{"token":"`+token+`","device_id":"`+device+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("session: %d %s", w.Code, w.Body)
	}
	var s dto.PanelSession
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	return s.Session
}

// event waits for the next published event and returns its data (the event without its envelope).
func (h *panelHarness) event(t *testing.T) map[string]any {
	t.Helper()
	select {
	case m := <-h.events:
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		_ = json.Unmarshal(m.Data, &envelope)
		return envelope.Data
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
		return nil
	}
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %s: %v", w.Body, err)
	}
	return v
}

// endregion

func TestPanelSessionAppliesTheGroupIDRule(t *testing.T) {
	h := newPanelHarness(t)
	tests := map[string]struct {
		token, device string
		want          int
		kind          dto.PanelUserKind
	}{
		"CPO on its pool":     {"cpo-220001", labDevice, http.StatusOK, dto.PanelUserKindRunUser},
		"CPO on another pool": {"cpo-221006", labDevice, http.StatusForbidden, ""},
		"admin on any pool":   {"admin", otherDevice, http.StatusOK, dto.PanelUserKindAdmin},
		"invalid token":       {"made-up", labDevice, http.StatusUnauthorized, ""},
		"device of no pool":   {"admin", "aaaaaaaaaaaaaaaaaaaaaaaa", http.StatusForbidden, ""},
		"not a device ID":     {"cpo-220001", "../../etc", http.StatusBadRequest, ""},
		"no token":            {"", labDevice, http.StatusBadRequest, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			w := h.do(http.MethodPost, "/api/panel/session", "", `{"token":"`+tt.token+`","device_id":"`+tt.device+`"}`)
			if w.Code != tt.want {
				t.Fatalf("status = %d %s; want %d", w.Code, w.Body, tt.want)
			}
			if tt.want == http.StatusOK {
				s := decodeBody[dto.PanelSession](t, w)
				if s.Session == "" || s.UserKind != tt.kind {
					t.Errorf("session = %+v; want kind %s", s, tt.kind)
				}
			}
		})
	}
}

func TestPanelCallsNeedASessionOfTheirPool(t *testing.T) {
	h := newPanelHarness(t)
	lab := h.session(t, "cpo-220001", labDevice)
	tests := map[string]struct {
		target, session string
		want            int
	}{
		"session of the pool":     {"/api/panel/" + labDevice + "/overview", lab, http.StatusOK},
		"no session":              {"/api/panel/" + labDevice + "/overview", "", http.StatusUnauthorized},
		"forged session":          {"/api/panel/" + labDevice + "/overview", lab + "x", http.StatusUnauthorized},
		"session of another pool": {"/api/panel/" + otherDevice + "/overview", lab, http.StatusUnauthorized},
		"not a device ID":         {"/api/panel/220001/overview", lab, http.StatusNotFound},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if w := h.do(http.MethodGet, tt.target, tt.session, ""); w.Code != tt.want {
				t.Errorf("status = %d %s; want %d", w.Code, w.Body, tt.want)
			}
		})
	}
}

func TestPanelOverview(t *testing.T) {
	h := newPanelHarness(t)
	h.tago.AddRecord(labToken, tagoio.DataRecord{Variable: "max_grid_power_consumption", Value: json.Number("30000")})
	h.tago.AddRecord(labToken, tagoio.DataRecord{Variable: "rate_costs", Value: "0",
		Metadata: map[string]any{"valle": 0.18, "llanas": 0.27, "punta": 0.39, "IVA": 0.21}})
	session := h.session(t, "cpo-220001", labDevice)

	w := h.do(http.MethodGet, "/api/panel/"+labDevice+"/overview", session, "")
	o := decodeBody[dto.PanelOverview](t, w)
	if o.PoolCode != 220001 || o.MaxPowerW != 30000 || o.Rates.Peak != 0.39 || o.Rates.VATPercent != 21 {
		t.Errorf("overview = %+v", o)
	}
	if strings.Join(o.Stations, ",") != "ST1,ST2" || len(o.BalancingModes) != 4 || len(o.Bands) != 6 {
		t.Errorf("stations %v, modes %v, bands %v", o.Stations, o.BalancingModes, o.Bands)
	}
}

func TestPanelCards(t *testing.T) {
	h := newPanelHarness(t)
	session := h.session(t, "cpo-220001", labDevice)
	cards := "/api/panel/" + labDevice + "/cards"

	w := h.do(http.MethodPost, cards, session, `{"id":"04a1b2","alias":"van 1","email":"fleet@example.es","linked_stations":["ST1"]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if e := h.event(t); e["event_type"] != string(events.TypeManageRFID) || e["action"] != "create" || e["card_id"] != "04a1b2" {
		t.Errorf("create event = %v", e)
	}
	if w := h.do(http.MethodPost, cards, session, `{"id":"04A1B2","linked_stations":["ST1"]}`); w.Code != http.StatusConflict {
		t.Errorf("duplicate create: %d; want 409", w.Code)
	}
	for name, body := range map[string]string{
		"unknown station":  `{"id":"x1","linked_stations":["NOPE"]}`,
		"no station":       `{"id":"x1","linked_stations":[]}`,
		"bad email":        `{"id":"x1","email":"nope","linked_stations":["ST1"]}`,
		"comma in alias":   `{"id":"x1","alias":"a,b","linked_stations":["ST1"]}`,
		"spaces in the ID": `{"id":"x 1","linked_stations":["ST1"]}`,
	} {
		if w := h.do(http.MethodPost, cards, session, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d; want 400", name, w.Code)
		}
	}

	w = h.do(http.MethodGet, cards, session, "")
	list := decodeBody[[]dto.PanelCard](t, w)
	if len(list) != 1 || list[0].ID != "04A1B2" || list[0].Alias != "VAN 1" || list[0].LinkedStations[0] != "ST1" {
		t.Fatalf("cards = %+v", list)
	}

	if w := h.do(http.MethodPut, cards+"/04A1B2", session, `{"alias":"van 2","linked_stations":["ST1","ST2"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	if e := h.event(t); e["action"] != "create" || e["alias"] != "VAN 2" {
		t.Errorf("update event = %v; want an upsert", e)
	}
	if w := h.do(http.MethodPut, cards+"/NOPE", session, `{"linked_stations":["ST1"]}`); w.Code != http.StatusNotFound {
		t.Errorf("update of an unknown card: %d; want 404", w.Code)
	}

	if w := h.do(http.MethodDelete, cards+"/04a1b2", session, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if e := h.event(t); e["action"] != "delete" || e["card_id"] != "04a1b2" {
		t.Errorf("delete event = %v", e)
	}
	if records := h.tago.Records(labToken, "card_id"); len(records) != 0 {
		t.Errorf("card records left: %v", records)
	}
}

func TestPanelConfigurationChanges(t *testing.T) {
	h := newPanelHarness(t)
	session := h.session(t, "cpo-220001", labDevice)
	base := "/api/panel/" + labDevice

	w := h.do(http.MethodPut, base+"/load-balancing-mode", session, `{"mode":"Dinámico boost"}`)
	if o := decodeBody[dto.PanelOverview](t, w); o.LoadBalancingMode != "Dinámico boost" {
		t.Errorf("mode: %d %+v", w.Code, o)
	}
	if e := h.event(t); e["selected_mode"] != "Dinámico boost" {
		t.Errorf("mode event = %v", e)
	}
	if w := h.do(http.MethodPut, base+"/load-balancing-mode", session, `{"mode":"turbo"}`); w.Code != http.StatusBadRequest {
		t.Errorf("unknown mode: %d; want 400", w.Code)
	}

	w = h.do(http.MethodPut, base+"/max-power", session, `{"max_power_w":45000}`)
	if o := decodeBody[dto.PanelOverview](t, w); o.MaxPowerW != 45000 {
		t.Errorf("power: %d %+v", w.Code, o)
	}
	if e := h.event(t); e["max_power_watts"] != 45000.0 {
		t.Errorf("power event = %v", e)
	}
	if records := h.tago.Records(labToken, "max_grid_power_consumption"); len(records) != 1 {
		t.Errorf("power records = %v; want one", records)
	}

	w = h.do(http.MethodPut, base+"/rates", session, `{"off_peak":0.2,"flat":0.3,"peak":0.4,"vat_percent":21,"withholding":40}`)
	if o := decodeBody[dto.PanelOverview](t, w); o.Rates.Flat != 0.3 || o.Rates.VATPercent != 21 {
		t.Errorf("rates: %d %+v", w.Code, o.Rates)
	}
	if e := h.event(t); e["vat"] != 21.0 || e["rate_peak"] != 0.4 {
		t.Errorf("rates event = %v", e)
	}
	if w := h.do(http.MethodPut, base+"/rates", session, `{"off_peak":9,"flat":0.3,"peak":0.4,"vat_percent":21}`); w.Code != http.StatusBadRequest {
		t.Errorf("rate out of range: %d; want 400", w.Code)
	}

	w = h.do(http.MethodPut, base+"/cpo", session, `{"name":"Velo","fiscal_id":"B-74.454 166","email":"info@velo.es"}`)
	if o := decodeBody[dto.PanelOverview](t, w); o.CPO.FiscalID != "B-74454166" {
		t.Errorf("cpo: %d %+v", w.Code, o.CPO)
	}
	if records := h.tago.Records(labToken, "operator_info"); len(records) != 1 || records[0].Metadata["nombre"] != "Velo" {
		t.Errorf("operator records = %v", records)
	}
}

func TestPanelResetIsLimitedPerStation(t *testing.T) {
	h := newPanelHarness(t)
	session := h.session(t, "cpo-220001", labDevice)
	reset := "/api/panel/" + labDevice + "/stations/ST1/reset"

	if w := h.do(http.MethodPost, reset, session, ""); w.Code != http.StatusAccepted {
		t.Fatalf("reset: %d %s", w.Code, w.Body)
	}
	e := h.event(t)
	payload, _ := e["payload"].(map[string]any)
	if e["event_type"] != string(events.TypeOCPPRequest) || payload["reset_type"] != "Soft" || payload["request"] != "reset" {
		t.Errorf("reset event = %v", e)
	}
	if w := h.do(http.MethodPost, reset, session, ""); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Errorf("second reset: %d; want 429 with Retry-After", w.Code)
	}
	for name, station := range map[string]string{"unknown station": "NOPE", "station of another pool": "OTHER"} {
		if w := h.do(http.MethodPost, "/api/panel/"+labDevice+"/stations/"+station+"/reset", session, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d; want 404", name, w.Code)
		}
	}
}

func TestPanelHistory(t *testing.T) {
	h := newPanelHarness(t)
	add := func(tx int, meta map[string]any) {
		h.tago.AddRecord(labToken, tagoio.DataRecord{Variable: chargepoints.HistoryVariable,
			Value: json.Number(strconv.Itoa(tx)), Group: strconv.Itoa(tx), Metadata: meta})
	}
	// Dates that sort wrong as text (DD/MM/YYYY), the keys of the Python service and a band past midnight
	add(1, map[string]any{"start_date": "02/12/2025", "time_band": "10:00 - 11:30", "display_id": "ST1 [1]",
		"card_alias": "VAN", "energy": 10.5, "cost": 3.2})
	add(2, map[string]any{"date": "15/11/2026", "time_band": "23:30 - 00:45", "serial_id": "ST2 [1]",
		"alias": "OFFICE", "energy": "4,25", "cost": "1.5"})
	add(3, map[string]any{"start_date": "01/11/2026", "time_band": "08:00 - 09:00", "display_id": "ST1 [2]",
		"card_alias": "VAN", "energy": 7.0, "cost": 2.0, "stop_motive": "CAR"})
	session := h.session(t, "admin", labDevice) // The account sees the whole history (TestPanelHistoryWindow)
	base := "/api/panel/" + labDevice + "/history"

	page := decodeBody[dto.PanelHistoryPage](t, h.do(http.MethodGet, base, session, ""))
	var order []int64
	for _, item := range page.Items {
		order = append(order, item.TransactionID)
	}
	if len(order) != 3 || order[0] != 2 || order[1] != 3 || order[2] != 1 {
		t.Errorf("default order = %v; want newest first [2 3 1]", order)
	}
	if page.Totals.Sessions != 3 || page.Totals.Cost != 6.7 || page.Totals.EnergyKWh != 21.75 {
		t.Errorf("totals = %+v", page.Totals)
	}
	if item := page.Items[0]; item.Minutes != 75 || item.Card != "OFFICE" || item.Station != "ST2 [1]" || item.StartedAt != "2026-11-15T23:30" {
		t.Errorf("Python-era record = %+v", item)
	}

	filtered := decodeBody[dto.PanelHistoryPage](t, h.do(http.MethodGet, base+"?from=2026-11-01&to=2026-11-14&q=van", session, ""))
	if filtered.Total != 1 || filtered.Items[0].TransactionID != 3 {
		t.Errorf("filtered = %+v", filtered)
	}
	byCost := decodeBody[dto.PanelHistoryPage](t, h.do(http.MethodGet, base+"?sort=cost&dir=asc&page=2&page_size=2", session, ""))
	if byCost.Total != 3 || len(byCost.Items) != 1 || byCost.Items[0].Cost != 3.2 {
		t.Errorf("by cost, page 2 = %+v", byCost)
	}
	if w := h.do(http.MethodGet, base+"?sort=nope", session, ""); w.Code != http.StatusBadRequest {
		t.Errorf("unknown sort: %d; want 400", w.Code)
	}

	w := h.do(http.MethodGet, base+".csv?q=office", session, "")
	csv := strings.TrimPrefix(w.Body.String(), "ï»¿")
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "15/11/2026;23:30 - 00:45;ST2 [1];OFFICE;75;4,250;1,50;2;") {
		t.Errorf("CSV = %q", csv)
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "recargas_220001_") {
		t.Errorf("Content-Disposition = %q", w.Header().Get("Content-Disposition"))
	}
}

func TestPanelHistoryWindow(t *testing.T) {
	h := newPanelHarness(t)
	madrid, _ := time.LoadLocation("Europe/Madrid")
	now := time.Now().In(madrid)
	for tx, daysAgo := range map[int]int{1: 10, 2: HistoryWindowDays - 1, 3: HistoryWindowDays + 5, 4: 400} {
		h.tago.AddRecord(labToken, tagoio.DataRecord{Variable: chargepoints.HistoryVariable, Value: json.Number(strconv.Itoa(tx)),
			Group: strconv.Itoa(tx), Metadata: map[string]any{"start_date": now.AddDate(0, 0, -daysAgo).Format("02/01/2006"),
				"time_band": "00:05 - 01:00", "display_id": "ST1 [1]", "card_alias": "VAN", "energy": 1.0, "cost": 1.0}})
	}
	count := func(token, query string) int {
		session := h.session(t, token, labDevice)
		return decodeBody[dto.PanelHistoryPage](t, h.do(http.MethodGet, "/api/panel/"+labDevice+"/history"+query, session, "")).Total
	}
	if got := count("cpo-220001", ""); got != 2 {
		t.Errorf("CPO, no filter: %d sessions; want the 2 of the last %d days", got, HistoryWindowDays)
	}
	if got := count("cpo-220001", "?from=2000-01-01"); got != 2 {
		t.Errorf("CPO asking for older sessions: %d; want still 2", got)
	}
	if got := count("admin", ""); got != 4 {
		t.Errorf("admin: %d sessions; want all 4", got)
	}
	session := h.session(t, "cpo-220001", labDevice)
	csv := h.do(http.MethodGet, "/api/panel/"+labDevice+"/history.csv?from=2000-01-01", session, "").Body.String()
	if lines := len(strings.Split(strings.TrimSpace(csv), "\n")) - 1; lines != 2 {
		t.Errorf("CPO CSV: %d sessions; want 2", lines)
	}
}

func TestPanelCardsStoredUnderOtherSpellings(t *testing.T) {
	h := newPanelHarness(t)
	// As the Python service stored them: grouped by the ID as typed, several times
	for range 3 {
		h.tago.AddRecord(labToken, tagoio.DataRecord{Variable: "card_id", Value: "payter", Group: "payter",
			Metadata: map[string]any{"alias": "PAYTER", "email": "", "cps": "ST1"}})
	}
	session := h.session(t, "cpo-220001", labDevice)
	cards := "/api/panel/" + labDevice + "/cards"

	if list := decodeBody[[]dto.PanelCard](t, h.do(http.MethodGet, cards, session, "")); len(list) != 1 || list[0].ID != "PAYTER" {
		t.Fatalf("cards = %+v; want PAYTER once", list)
	}
	if w := h.do(http.MethodPut, cards+"/PAYTER", session, `{"alias":"payter","linked_stations":["ST1","ST2"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	h.event(t)
	if records := h.tago.Records(labToken, "card_id"); len(records) != 1 || records[0].Group != "PAYTER" {
		t.Errorf("after the update: %+v; want one record, grouped PAYTER", records)
	}

	h.tago.AddRecord(labToken, tagoio.DataRecord{Variable: "card_id", Value: "payter", Group: "payter"})
	if w := h.do(http.MethodDelete, cards+"/PAYTER", session, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	h.event(t)
	if records := h.tago.Records(labToken, "card_id"); len(records) != 0 {
		t.Errorf("after the delete: %+v; want none", records)
	}
	if list := decodeBody[[]dto.PanelCard](t, h.do(http.MethodGet, cards, session, "")); len(list) != 0 {
		t.Errorf("cards after the delete = %+v", list)
	}
}

func TestPanelStreamSendsTheStateAndItsChanges(t *testing.T) {
	h := newPanelHarness(t)
	session := h.session(t, "cpo-220001", labDevice)
	server := httptest.NewServer(h.handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/panel/"+labDevice+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)
	next := func() dto.PanelLive {
		for lines.Scan() {
			if data, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				var live dto.PanelLive
				if err := json.Unmarshal([]byte(data), &live); err != nil {
					t.Fatal(err)
				}
				return live
			}
		}
		t.Fatalf("stream ended: %v", lines.Err())
		return dto.PanelLive{}
	}

	live := next()
	if len(live.Stations) != 2 || live.Stations[1].Name != "ST2" || live.Stations[1].Connection != dto.PanelConnectionOffline {
		t.Fatalf("first state = %+v", live.Stations)
	}
	if c := live.Stations[0].Connectors; len(c) != 2 || c[1].Status != dto.ConnectorStatusCharging || c[1].Since == nil {
		t.Errorf("ST1 connectors = %+v", c)
	}

	_ = h.stations.UpdateSession(context.Background(), chargepoints.SessionUpdate{
		PoolCode: 220001, StationName: "ST1", ConnectorID: 2, TransactionID: 77, CardAlias: "VAN", Step: chargepoints.StepInProgress,
		LastMeterTS: "2026-11-23T10:00:00+01:00", Power: 7200, Energy: 3.5, Cost: 1.2, CurrentTariffBand: "Peak",
		EnergyPeak: 3500,
	})
	live = next()
	if len(live.Sessions) != 1 || live.TotalPowerW != 7200 || live.Sessions[0].CurrentBand != dto.TariffBandPeak ||
		live.Sessions[0].EnergyBandsWh[2] != 3500 {
		t.Errorf("state after the session update = %+v", live)
	}
}

func TestPanelStreamNeedsASession(t *testing.T) {
	h := newPanelHarness(t)
	if w := h.do(http.MethodGet, "/api/panel/"+labDevice+"/stream", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", w.Code)
	}
}

func TestSessionStart(t *testing.T) {
	madrid, _ := time.LoadLocation("Europe/Madrid")
	tests := map[string]struct {
		date, band string
		start      string
		minutes    int
	}{
		"same day":       {"20/11/2026", "12:17 - 15:24", "2026-11-20T12:17", 187},
		"past midnight":  {"20/11/2026", "23:50 - 00:10", "2026-11-20T23:50", 20},
		"no band":        {"20/11/2026", "", "2026-11-20T00:00", 0},
		"no date":        {"", "10:00 - 11:00", "", 0},
		"malformed date": {"2026-11-20", "10:00 - 11:00", "", 0},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			start, minutes := sessionStart(tt.date, tt.band, madrid)
			got := ""
			if !start.IsZero() {
				got = start.Format("2006-01-02T15:04")
			}
			if got != tt.start || minutes != tt.minutes {
				t.Errorf("sessionStart = %q, %d; want %q, %d", got, minutes, tt.start, tt.minutes)
			}
		})
	}
}
