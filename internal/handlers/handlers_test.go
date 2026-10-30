package handlers

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

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/provisioning"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

// region Harness

type noBucket struct{}

func (noBucket) Insert(context.Context, int, ...tagoio.DataInput) error { return nil }

type noReader struct{}

func (noReader) LastValue(context.Context, int, string) (*tagoio.DataRecord, error) {
	return nil, errors.New("TagoIO unavailable in tests")
}
func (noReader) List(context.Context, int, string, int) ([]tagoio.DataRecord, error) { return nil, nil }

func newTestApp(t *testing.T) *App {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := &config.Config{
		ServiceAPI:   config.Credentials{User: "svc", Secret: "svc-secret"},
		Admin:        config.Credentials{User: "admin", Secret: "admin-secret"},
		CookieSecret: []byte("0123456789abcdef0123456789abcdef"),
		ShortLinkURL: "https://qr.example.test",
	}
	return New(Deps{
		Log: log, Config: cfg, Store: db, Broker: events.NewBroker(log, false),
		ChargePoints: chargepoints.New(db, noBucket{}, log), Pools: pools.NewCache(noReader{}, log),
		UI: webui.Disabled(errors.New("no UI in tests")), Jobs: context.Background(),
	})
}

func serviceRequest(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.SetBasicAuth("svc", "svc-secret")
	return r
}

// endregion

func TestRequireService(t *testing.T) {
	app := newTestApp(t)
	handler := app.RequireService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))

	tests := map[string]struct {
		user, secret string
		want         int
	}{
		"valid":        {"svc", "svc-secret", http.StatusTeapot},
		"wrong secret": {"svc", "nope", http.StatusUnauthorized},
		"admin creds":  {"admin", "admin-secret", http.StatusUnauthorized},
		"none":         {"", "", http.StatusUnauthorized},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/stream", nil)
			if tt.user != "" {
				r.SetBasicAuth(tt.user, tt.secret)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != tt.want {
				t.Fatalf("status = %d; want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("401 without WWW-Authenticate")
			}
		})
	}
}

func TestAdminLoginSessionAndLogout(t *testing.T) {
	app := newTestApp(t)
	protected := app.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	// Without a session: redirect to the login page, which comes back.
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/emsp-dashboard/audits/261001", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?redirect=%2Femsp-dashboard%2Faudits%2F261001" {
		t.Fatalf("no session: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// Wrong password.
	rec = httptest.NewRecorder()
	app.LoginAPI(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"user":"admin","password":"x","redirect":"/emsp-dashboard"}`)))
	if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("wrong password: %d, cookies %v", rec.Code, rec.Result().Cookies())
	}

	// Right password: a cookie and a safe redirect.
	rec = httptest.NewRecorder()
	app.LoginAPI(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"user":"admin","password":"admin-secret","redirect":"https://evil.example/x"}`)))
	var result dto.LoginResult
	_ = json.NewDecoder(rec.Body).Decode(&result)
	cookies := rec.Result().Cookies()
	if rec.Code != http.StatusOK || result.Redirect != adminLandingURL || len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("login: %d %+v cookies %v", rec.Code, result, cookies)
	}

	r := httptest.NewRequest(http.MethodGet, "/emsp-dashboard", nil)
	r.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	protected.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the session cookie: %d", rec.Code)
	}

	// A tampered cookie is rejected.
	tampered := *cookies[0]
	tampered.Value = "9999999999." + strings.SplitN(cookies[0].Value, ".", 2)[1]
	r = httptest.NewRequest(http.MethodGet, "/emsp-dashboard", nil)
	r.AddCookie(&tampered)
	rec = httptest.NewRecorder()
	app.RequireAdminAPI(protected).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered cookie accepted: %d", rec.Code)
	}

	// Logout clears it.
	rec = httptest.NewRecorder()
	app.LogoutAPI(rec, httptest.NewRequest(http.MethodPost, "/api/logout", nil))
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Fatalf("logout cookies = %v", c)
	}
}

func TestSafeRedirectPath(t *testing.T) {
	tests := map[string]string{
		"/emsp-dashboard/audits/1?x=2": "/emsp-dashboard/audits/1?x=2",
		"":                             adminLandingURL,
		"https://evil.example":         adminLandingURL,
		"//evil.example/x":             adminLandingURL,
		"/\\evil.example":              adminLandingURL,
		"/login":                       adminLandingURL,
		"/x\r\nSet-Cookie: a=b":        adminLandingURL,
	}
	for in, want := range tests {
		if got := SafeRedirectPath(in); got != want {
			t.Errorf("SafeRedirectPath(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestStreamSendsEnvelopesOnSingleDataLines(t *testing.T) {
	app := newTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(app.Stream))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}

	reader := bufio.NewReader(resp.Body)
	if line, _ := reader.ReadString('\n'); !strings.HasPrefix(line, ":") {
		t.Fatalf("first line = %q; want a comment", line)
	}
	go func() {
		time.Sleep(50 * time.Millisecond) // Let the subscription register
		app.Broker.Publish(events.VPOSStop{Header: events.NewHeader(events.TypeVPOSStopTransaction, 261001), StationName: "ST1", ConnectorID: 1})
	}()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		data, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "data: ")
		if !ok {
			continue
		}
		// neos-ocpp: json.loads of the line, unwrap "data", dispatch on "event_type".
		var envelope struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			t.Fatalf("data line is not one JSON object: %q", data)
		}
		if envelope.Event != "virtual_POS_stop_transaction" || envelope.Data["event_type"] != envelope.Event || envelope.Data["pool_code"] != float64(261001) {
			t.Fatalf("envelope = %+v", envelope)
		}
		return
	}
}

func TestChargePointUpdateContract(t *testing.T) {
	app := newTestApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/charge-point-update/{pool_code}/{station_name}/{connector_id}", app.ChargePointUpdate)

	// The body as neos-ocpp sends it (ChargePointUpdateBody.dict()); the URL's connector wins.
	body := `{"connector_id": 9, "connection_status": "Online", "charge_point_status": "Charging",
		"availability_type": "Operative", "charge_point_error_code": "NoError", "has_public_dashboard": false}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, serviceRequest(http.MethodPost, "/v1/charge-point-update/261001/22HBS0452HBS22/1", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var state map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &state)
	for _, key := range []string{"pool_code", "station_name", "connector_id", "charge_point_status", "is_quarantined", "quarantine_end", "last_update", "omitted_updates"} {
		if _, ok := state[key]; !ok {
			t.Errorf("answer misses %q: %s", key, rec.Body)
		}
	}
	if state["connector_id"] != float64(1) || state["charge_point_status"] != "Charging" {
		t.Errorf("answer = %s", rec.Body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, serviceRequest(http.MethodPost, "/v1/charge-point-update/abc/ST/1", body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric pool code: %d", rec.Code)
	}
}

func TestProvisioningFormAcceptsBothKeyStylesAndEchoesSpanishKeys(t *testing.T) {
	snake := `{"user_email":"a@b.es","company_name":"ACME","cpo_fiscal_id":"B1","company_address":"C/ 1",
		"contact_phone":"985","contact_email":"c@b.es","manager_name":"Ana","manager_dni":"1X","iban":"ES00",
		"payment_system":"TPV y RFID","description":"Garaje"}`
	spanish := `{"Dirección de correo electrónico":"a@b.es","Nombre de la Empresa":"ACME","CIF de la Empresa":"B1",
		"Dirección de la Empresa":"C/ 1","Teléfono de contacto":"985","Correo electrónico de contacto":"c@b.es",
		"Nombre del Apoderado de la Empresa":"Ana","DNI del Apoderado de la Empresa":"1X","IBAN Bancario":"ES00",
		"Añadir sistema de Pago en los Puntos de Recarga":"TPV y RFID","Descripción del sitio para la instalación.":"Garaje",
		"Marca temporal":"2026-10-22"}`
	for name, raw := range map[string]string{"snake_case": snake, "Spanish": spanish} {
		t.Run(name, func(t *testing.T) {
			var form provisioning.Form
			if err := json.Unmarshal([]byte(raw), &form); err != nil {
				t.Fatal(err)
			}
			if form.UserEmail != "a@b.es" || form.PaymentSystem != "TPV y RFID" || form.ManagerName != "Ana" {
				t.Fatalf("form = %+v", form)
			}
			echoed, _ := json.Marshal(form)
			if !strings.Contains(string(echoed), `"Nombre de la Empresa":"ACME"`) {
				t.Fatalf("echo must use the Spanish keys: %s", echoed)
			}
		})
	}

	var form provisioning.Form
	err := json.Unmarshal([]byte(`{"user_email":"not-an-email","company_name":"ACME"}`), &form)
	if err == nil || !strings.Contains(err.Error(), "user_email is not a valid email") || !strings.Contains(err.Error(), "iban is required") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestChargeRequestValidation(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, err := app.ChargePoints.UpdateStatus(ctx, chargepoints.StatusUpdate{
		PoolCode: 261001, StationName: "ST1", ConnectorID: 1, ConnectionStatus: "Online", ChargePointStatus: "Preparing",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := app.Broker.Subscribe()
	defer unsubscribe()

	// In order: the first published event is checked below.
	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", `{"pool_code":261001,"station_name":"ST1","connector_id":1,"email":"d@x.es","requires_invoice":false}`, http.StatusOK},
		{"invalid email", `{"pool_code":261001,"station_name":"ST1","connector_id":1,"email":"nope","requires_invoice":false}`, http.StatusUnprocessableEntity},
		{"incomplete invoice", `{"pool_code":261001,"station_name":"ST1","connector_id":1,"email":"d@x.es","requires_invoice":true,"receipt_name":"A"}`, http.StatusUnprocessableEntity},
		{"unknown connector", `{"pool_code":261001,"station_name":"ST1","connector_id":7,"email":"d@x.es","requires_invoice":false}`, http.StatusNotFound},
		{"invoice with fields", `{"pool_code":261001,"station_name":"ST1","connector_id":1,"email":"d@x.es","requires_invoice":true,"receipt_fiscal_id":"B1","receipt_name":"A","receipt_address":"C","receipt_email":"f@x.es"}`, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			app.ChargeRequest(rec, httptest.NewRequest(http.MethodPost, "/api/charge-request", strings.NewReader(tt.body)))
			if rec.Code != tt.want {
				t.Fatalf("status = %d; want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}

	// The amount comes from the pool configuration (defaults here), never from the client.
	data := <-events
	if !strings.Contains(string(data), `"amount":40`) || !strings.Contains(string(data), `"receipt_email":null`) {
		t.Fatalf("published event = %s", data)
	}
}

func TestStopRequestNeedsAPaidSession(t *testing.T) {
	app := newTestApp(t)
	rec := httptest.NewRecorder()
	app.StopRequest(rec, httptest.NewRequest(http.MethodPost, "/api/stop-request", strings.NewReader(`{"pool_code":261001,"station_name":"ST1","connector_id":1}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d; want 409 without a paid session", rec.Code)
	}
}

func TestAuditExportIsPerHistoryRow(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)

	// Two charge points that both reach transaction 7783; only the first one has meter values.
	for _, station := range []string{"ST1", "ST2"} {
		_, err := app.Store.InsertSession(ctx, store.SessionRecord{PoolCode: 261001, StationName: station, ConnectorID: 1,
			TransactionID: 7783, CardAlias: "CARD", StartDate: "22/10/2026", TimeBand: "10:00 - 11:00", LastMeterValue: 1000})
		if err != nil {
			t.Fatal(err)
		}
	}
	err := app.Store.InsertTelemetry(ctx, store.TelemetryRecord{PoolCode: 261001, StationName: "ST1", ConnectorID: 1,
		TransactionID: 7783, Timestamp: "2026-10-22T10:00:00Z", MeterValue: 1000, CurrentTariffBand: "Flat"})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := app.Store.RecentSessions(ctx, 261001, 10)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("RecentSessions = %+v, %v", sessions, err)
	}

	export := func(id string) int {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/admin/audits/"+id+"/export", nil)
		r.SetPathValue("session_id", id)
		app.AuditExport(rec, r)
		return rec.Code
	}
	for _, s := range sessions {
		want := http.StatusOK
		if s.StationName == "ST2" {
			want = http.StatusNotFound // The meter values of ST1 are not ST2's
		}
		if got := export(strconv.FormatInt(s.ID, 10)); got != want {
			t.Errorf("export of %s = %d; want %d", s.StationName, got, want)
		}
	}
	if got := export("999"); got != http.StatusNotFound {
		t.Errorf("export of a missing session = %d; want 404", got)
	}
	if got := export("abc"); got != http.StatusBadRequest {
		t.Errorf("export of a non-integer id = %d; want 400", got)
	}
}

func TestFallbackRoutesStationPages(t *testing.T) {
	app := newTestApp(t)
	tests := map[string]int{
		"/":                         http.StatusFound,
		"/261001/22HBS0452HBS22":    http.StatusServiceUnavailable, // The page (the UI is disabled in tests)
		"/not-a-pool/22HBS0452HBS2": http.StatusNotFound,
		"/261001/ST/extra":          http.StatusNotFound,
	}
	for path, want := range tests {
		rec := httptest.NewRecorder()
		app.Fallback(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d; want %d", path, rec.Code, want)
		}
	}
}

func TestQRCodePNG(t *testing.T) {
	image, err := qrPNG("https://qr.example.test/261001/22HBS0452HBS22")
	if err != nil || len(image) < 100 || string(image[1:4]) != "PNG" {
		t.Fatalf("qrPNG = %d bytes, %v", len(image), err)
	}
}
