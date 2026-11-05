package csms

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/config"
)

var creds = config.Credentials{User: "svc", Secret: "svc-secret"}

// csmsServer answers GET /api/payments with status and body, and counts its requests.
func csmsServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if user, secret, ok := r.BasicAuth(); !ok || user != creds.User || secret != creds.Secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/payments" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testQuery() PaymentsQuery {
	madrid, _ := time.LoadLocation("Europe/Madrid")
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, madrid)
	return PaymentsQuery{Since: since, Until: since.AddDate(0, 0, 5), PoolCode: 221006, Limit: 2000}
}

func TestPaymentsComeFromTheFirstCSMSThatAnswers(t *testing.T) {
	down, downCalls := csmsServer(t, http.StatusServiceUnavailable, `{"error": "the database is not available"}`)
	up, _ := csmsServer(t, http.StatusOK, `{"payments": [{"id": 7, "status": "ERROR", "error_code": 562}], "truncated": true}`)
	client := New([]string{down.URL, up.URL}, creds, nil)

	result, err := client.Payments(context.Background(), testQuery())

	if err != nil {
		t.Fatal(err)
	}
	if downCalls.Load() != 1 || result.Source != up.URL || !result.Truncated {
		t.Fatalf("result %+v, calls to the first CSMS %d", result, downCalls.Load())
	}
	if len(result.Payments) != 1 || result.Payments[0].ID != 7 || *result.Payments[0].ErrorCode != 562 {
		t.Fatalf("payments %+v", result.Payments)
	}
}

func TestTheSecondCSMSIsNotAskedWhenTheFirstAnswers(t *testing.T) {
	first, _ := csmsServer(t, http.StatusOK, `{"payments": [], "truncated": false}`)
	second, secondCalls := csmsServer(t, http.StatusOK, `{"payments": [{"id": 1}]}`)

	result, err := New([]string{first.URL, second.URL}, creds, nil).Payments(context.Background(), testQuery())

	if err != nil || len(result.Payments) != 0 || secondCalls.Load() != 0 {
		t.Fatalf("result %+v, err %v, calls to the second CSMS %d", result, err, secondCalls.Load())
	}
}

func TestARejectedQueryIsNotSentToTheNextCSMS(t *testing.T) {
	first, _ := csmsServer(t, http.StatusBadRequest, `{"error": "since must be before until"}`)
	second, secondCalls := csmsServer(t, http.StatusOK, `{"payments": []}`)

	_, err := New([]string{first.URL, second.URL}, creds, nil).Payments(context.Background(), testQuery())

	if !errors.Is(err, ErrBadQuery) || secondCalls.Load() != 0 {
		t.Fatalf("err %v, calls to the second CSMS %d", err, secondCalls.Load())
	}
}

func TestEveryFailureIsReportedWhenNoCSMSAnswers(t *testing.T) {
	old, _ := csmsServer(t, http.StatusNotFound, "") // A CSMS deployed before the API
	broken, _ := csmsServer(t, http.StatusServiceUnavailable, `{"error": "the database is not available"}`)

	_, err := New([]string{old.URL, broken.URL}, creds, nil).Payments(context.Background(), testQuery())

	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{old.URL, broken.URL, "HTTP 404", "HTTP 503 the database is not available"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestTheQueryIsSentAsTheCSMSReadsIt(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"payments": []}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := New([]string{srv.URL}, creds, nil).Payments(context.Background(), testQuery()); err != nil {
		t.Fatal(err)
	}

	const want = "limit=2000&pool_code=221006&since=2026-11-01T00%3A00%3A00%2B01%3A00&until=2026-11-06T00%3A00%3A00%2B01%3A00"
	if got != want {
		t.Fatalf("query %q; want %q", got, want)
	}
}

func TestOtherCredentialsAreRefused(t *testing.T) {
	srv, _ := csmsServer(t, http.StatusOK, `{"payments": []}`)

	_, err := New([]string{srv.URL}, config.Credentials{User: "svc", Secret: "old"}, nil).
		Payments(context.Background(), testQuery())

	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err %v", err)
	}
}

func TestAnUnconfiguredClientAsksNoOne(t *testing.T) {
	var nilClient *Client
	for _, client := range []*Client{nilClient, New(nil, creds, nil)} {
		if client.Configured() {
			t.Fatal("configured without URLs")
		}
		if _, err := client.Payments(context.Background(), testQuery()); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("err %v", err)
		}
	}
}
