package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/csms"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

// csmsPayments is a CSMS's GET /api/payments answer, as neos-ocpp builds it (list_payments): one committed Paycomet
// payment and one whose preauth failed.
const csmsPayments = `{"payments": [
	{"id": 12, "created_at": "2026-11-05T10:02:03.123000+00:00", "updated_at": "2026-11-05T10:02:05+00:00",
	 "pool_code": 221006, "station_name": "22V20537CCPOS", "connector_id": 1, "platform": "PAYCOMET",
	 "category": "LIVE", "status": "ERROR", "limit": 4000, "amount": null, "currency": "EUR",
	 "transaction_id": null, "session_energy": null, "session_cost": null, "order": "1221006ABCDEF0000SNA",
	 "card_brand": null, "terminal": null, "error_stage": "PREAUTH", "error_code": 562,
	 "error_message": "Paycomet refused the preauth", "error_at": "2026-11-05T10:02:05+00:00"},
	{"id": 11, "created_at": "2026-11-04T18:00:00+00:00", "updated_at": "2026-11-04T19:10:00+00:00",
	 "pool_code": 221006, "station_name": "22V20538CCPOS", "connector_id": 2, "platform": "PAYCOMET",
	 "category": "LIVE", "status": "COMMITTED", "limit": 4000, "amount": 1234, "currency": "EUR",
	 "transaction_id": 4321, "session_energy": 9870, "session_cost": "12.34", "order": "1221006ABCDEF0000SNB",
	 "card_brand": "VISA", "terminal": null, "error_stage": null, "error_code": null, "error_message": null,
	 "error_at": null}
], "truncated": false}`

// paymentsApp is a test app whose CSMS answers with body, recording the query it gets.
func paymentsApp(t *testing.T, status int, body string) (*App, *string) {
	t.Helper()
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, secret, ok := r.BasicAuth(); !ok || user != "svc" || secret != "svc-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		query = r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	app := newTestApp(t)
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	app.Config.Location = madrid
	app.CSMS = csms.New([]string{srv.URL}, app.Config.ServiceAPI, srv.Client())
	return app, &query
}

func getPayments(app *App, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.PaymentsAPI(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestPaymentsAreReadFromTheCSMS(t *testing.T) {
	app, query := paymentsApp(t, http.StatusOK, csmsPayments)

	rec := getPayments(app, "/api/admin/payments?from=2026-11-01&to=2026-11-05&pool_code=221006")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var result dto.PaymentsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Payments) != 2 || result.Truncated || !strings.HasPrefix(result.Source, "127.0.0.1:") {
		t.Fatalf("result %+v", result)
	}
	failed, committed := result.Payments[0], result.Payments[1]
	if failed.Status != "ERROR" || *failed.ErrorStage != "PREAUTH" || *failed.ErrorCode != 562 || failed.Amount != nil {
		t.Errorf("failed payment %+v", failed)
	}
	if committed.Status != "COMMITTED" || *committed.Amount != 1234 || *committed.TransactionID != 4321 {
		t.Errorf("committed payment %+v", committed)
	}

	// Madrid days, both included: from midnight on the 1st to midnight after the 5th (CET, +01:00)
	const want = "limit=2000&pool_code=221006&since=2026-11-01T00%3A00%3A00%2B01%3A00&until=2026-11-06T00%3A00%3A00%2B01%3A00"
	if *query != want {
		t.Errorf("query %q; want %q", *query, want)
	}
}

func TestPaymentsServeNoPersonalField(t *testing.T) {
	app, _ := paymentsApp(t, http.StatusOK, csmsPayments)

	body := getPayments(app, "/api/admin/payments?from=2026-11-01&to=2026-11-05").Body.String()

	for _, field := range []string{"email", "masked_pan", "card_hash", "fiscal", "receipt", "address"} {
		if strings.Contains(body, field) {
			t.Errorf("the answer has %q: %s", field, body)
		}
	}
}

func TestPaymentsRangeAcrossTheOctoberDSTChange(t *testing.T) {
	app, query := paymentsApp(t, http.StatusOK, `{"payments": []}`)

	if rec := getPayments(app, "/api/admin/payments?from=2026-10-25&to=2026-10-25"); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	// 25 hours: CEST until 03:00, CET after it
	const want = "limit=2000&since=2026-10-25T00%3A00%3A00%2B02%3A00&until=2026-10-26T00%3A00%3A00%2B01%3A00"
	if *query != want {
		t.Errorf("query %q; want %q", *query, want)
	}
}

func TestPaymentsRejectAnInvalidRange(t *testing.T) {
	app, query := paymentsApp(t, http.StatusOK, `{"payments": []}`)

	for _, target := range []string{
		"/api/admin/payments",
		"/api/admin/payments?from=2026-11-01",
		"/api/admin/payments?from=01/11/2026&to=05/11/2026",
		"/api/admin/payments?from=2026-11-05&to=2026-11-01",
		"/api/admin/payments?from=2025-01-01&to=2026-11-05",
		"/api/admin/payments?from=2026-11-01&to=2026-11-05&pool_code=abc",
	} {
		if rec := getPayments(app, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", target, rec.Code)
		}
	}
	if *query != "" {
		t.Errorf("the CSMS was asked %q", *query)
	}
}

func TestPaymentsWithoutAnyCSMS(t *testing.T) {
	app := newTestApp(t) // No CSMS_URLS

	rec := getPayments(app, "/api/admin/payments?from=2026-11-01&to=2026-11-05")

	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "service_unavailable") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestPaymentsWhenTheCSMSFails(t *testing.T) {
	tests := map[string]struct {
		status int
		want   int
	}{
		"down":     {http.StatusServiceUnavailable, http.StatusServiceUnavailable},
		"rejected": {http.StatusBadRequest, http.StatusBadRequest},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			app, _ := paymentsApp(t, tc.status, `{"error": "nope"}`)

			if rec := getPayments(app, "/api/admin/payments?from=2026-11-01&to=2026-11-05"); rec.Code != tc.want {
				t.Fatalf("status %d; want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestACSMSThatDidNotAnswerIsShownAndLogged(t *testing.T) {
	app := newTestApp(t)
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(csmsPayments))
	}))
	t.Cleanup(answering.Close)
	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close() // Refuses connections, as a CSMS whose name, port or network is wrong
	app.CSMS = csms.New([]string{unreachable.URL, answering.URL}, app.Config.ServiceAPI, nil)
	var logged bytes.Buffer
	app.Log = slog.New(slog.NewTextHandler(&logged, nil))

	rec := getPayments(app, "/api/admin/payments?from=2026-11-01&to=2026-11-05")

	var result dto.PaymentsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	down, up := strings.TrimPrefix(unreachable.URL, "http://"), strings.TrimPrefix(answering.URL, "http://")
	if len(result.Payments) != 2 || result.Source != up || len(result.Unanswered) != 1 || result.Unanswered[0] != down {
		t.Fatalf("result %+v; want the payments of %s, %s unanswered", result, up, down)
	}
	if line := logged.String(); !strings.Contains(line, "level=WARN") || !strings.Contains(line, "csms="+down) ||
		!strings.Contains(line, "answered="+up) || !strings.Contains(line, "error=") {
		t.Fatalf("log %q; want a warning naming the CSMS and why", line)
	}

	// When the first CSMS answers, nothing is unanswered: an empty list, not null
	app.CSMS = csms.New([]string{answering.URL}, app.Config.ServiceAPI, nil)
	rec = getPayments(app, "/api/admin/payments?from=2026-11-01&to=2026-11-05")
	if !strings.Contains(rec.Body.String(), `"unanswered":[]`) {
		t.Fatalf("body %s", rec.Body)
	}
}
