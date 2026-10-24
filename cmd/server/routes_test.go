package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/handlers"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

type noBucket struct{}

func (noBucket) Insert(context.Context, int, ...tagoio.DataInput) error { return nil }

type noReader struct{}

func (noReader) LastValue(context.Context, int, string) (*tagoio.DataRecord, error) {
	return nil, errors.New("TagoIO unavailable in tests")
}
func (noReader) List(context.Context, int, string, int) ([]tagoio.DataRecord, error) { return nil, nil }

// testRoutes returns the service's real routes, with the services the unauthenticated paths reach. The ones only
// reached after authentication (devices, bucket, provisioning, maintenance) stay nil: a request that gets past
// the authentication of those routes fails loudly.
func testRoutes(t *testing.T) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "routes.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ui := webui.Disabled(errors.New("no UI in tests"))
	app := &application{ui: ui, handlers: handlers.New(handlers.Deps{
		Log: log, Store: db, Broker: events.NewBroker(log, false), UI: ui, Jobs: context.Background(),
		Config: &config.Config{
			ServiceAPI:   config.Credentials{User: "svc", Secret: "svc-secret"},
			Admin:        config.Credentials{User: "admin", Secret: "admin-secret"},
			CookieSecret: []byte("0123456789abcdef0123456789abcdef"),
			ShortLinkURL: "https://qr.example.test",
		},
		ChargePoints: chargepoints.New(db, noBucket{}, log), Pools: pools.NewCache(noReader{}, log),
	})}
	return app.routes()
}

type route struct{ method, path string }

// Every route of routes.go, by the authentication it needs. Keep in sync when a route is added.
var (
	serviceRoutes = []route{
		{"GET", "/api/stream"},
		{"POST", "/v1/charge-point-update/261001/ST1/1"},
		{"POST", "/v1/charging-session-update/261001/ST1/1"},
		{"GET", "/api/pools/261001"},
		{"POST", "/api/provision-cpo"},
		{"POST", "/v1/trigger-task/pool-variable-cleanup"},
		{"POST", "/v1/trigger-task/backup-to-telegram"},
		{"DELETE", "/v1/trigger-task/single-variable/261001/state"},
	}
	adminAPIRoutes = []route{
		{"GET", "/api/admin/station-link?pool_code=261001&station_name=ST1"},
		{"GET", "/api/admin/qr/261001/ST1"},
		{"DELETE", "/api/admin/stations/261001/ST1"},
		{"GET", "/api/admin/audits/4321/export"},
	}
	adminPages = []route{
		{"GET", "/emsp-dashboard"},
		{"GET", "/emsp-dashboard/audits"},
		{"GET", "/emsp-dashboard/audits/261001"},
		{"GET", "/emsp-dashboard/poster/261001/ST1"},
	}
	publicRoutes = []route{
		{"GET", "/healthz"},
		{"GET", "/api/station/261001/ST1"},
		{"POST", "/api/charge-request"},
		{"POST", "/api/stop-request"},
		{"GET", "/login"},
		{"POST", "/api/login"},
		{"POST", "/api/logout"},
		{"GET", "/261001/ST1"}, // Station page (fallback route), opened from the QR codes
	}
)

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestMachineRoutesNeedTheServiceCredentials(t *testing.T) {
	h := testRoutes(t)
	for _, rt := range serviceRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if rec := serve(h, httptest.NewRequest(rt.method, rt.path, nil)); rec.Code != http.StatusUnauthorized {
				t.Errorf("without credentials: %d", rec.Code)
			}
			r := httptest.NewRequest(rt.method, rt.path, nil)
			r.SetBasicAuth("admin", "admin-secret")
			if rec := serve(h, r); rec.Code != http.StatusUnauthorized {
				t.Errorf("with the admin credentials: %d", rec.Code)
			}
		})
	}
}

func TestAdminAPIsNeedAnAdminSession(t *testing.T) {
	h := testRoutes(t)
	for _, rt := range adminAPIRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			if rec := serve(h, httptest.NewRequest(rt.method, rt.path, nil)); rec.Code != http.StatusUnauthorized {
				t.Errorf("without a session: %d", rec.Code)
			}
			for _, creds := range [][2]string{{"svc", "svc-secret"}, {"admin", "admin-secret"}} {
				r := httptest.NewRequest(rt.method, rt.path, nil)
				r.SetBasicAuth(creds[0], creds[1]) // Basic auth is not accepted for the admin APIs
				if rec := serve(h, r); rec.Code != http.StatusUnauthorized {
					t.Errorf("with HTTP Basic %s: %d", creds[0], rec.Code)
				}
			}
		})
	}
}

func TestAdminPagesRedirectToTheLogin(t *testing.T) {
	h := testRoutes(t)
	for _, rt := range adminPages {
		t.Run(rt.path, func(t *testing.T) {
			rec := serve(h, httptest.NewRequest(rt.method, rt.path, nil))
			if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/login?redirect=") {
				t.Errorf("without a session: %d, Location %q", rec.Code, rec.Header().Get("Location"))
			}
		})
	}
}

func TestPublicRoutesNeedNoCredentials(t *testing.T) {
	h := testRoutes(t)
	for _, rt := range publicRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := serve(h, httptest.NewRequest(rt.method, rt.path, strings.NewReader("{}")))
			// POST /api/login answers 401 to wrong credentials: that is the login itself, not a protection.
			unauthorized := rec.Code == http.StatusUnauthorized && rt.path != "/api/login"
			if unauthorized || rec.Code == http.StatusFound || rec.Code == http.StatusNotFound {
				t.Errorf("status = %d: %s", rec.Code, rec.Body)
			}
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Errorf("asks for HTTP Basic credentials: %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestEveryAnswerCarriesTheSecurityHeaders(t *testing.T) {
	h := testRoutes(t)
	all := append(append(append(append([]route{}, serviceRoutes...), adminAPIRoutes...), adminPages...), publicRoutes...)
	for _, rt := range all {
		rec := serve(h, httptest.NewRequest(rt.method, rt.path, nil))
		for header, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "strict-origin-when-cross-origin",
		} {
			if got := rec.Header().Get(header); got != want {
				t.Errorf("%s %s: %s = %q; want %q", rt.method, rt.path, header, got, want)
			}
		}
	}
}

func TestCrossSiteStateChangesAreRejected(t *testing.T) {
	h := testRoutes(t)
	for _, path := range []string{"/api/stop-request", "/api/charge-request", "/api/login"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		if rec := serve(h, r); rec.Code != http.StatusForbidden {
			t.Errorf("cross-site POST %s: %d", path, rec.Code)
		}
	}
}

func TestLegacyGetTriggerTasksNoLongerRun(t *testing.T) {
	h := testRoutes(t)
	// The legacy service took GET for the trigger tasks; the Go one only POST, so a cron or script still using GET
	// must be changed. The GET falls through to the fallback route: 404, not 405.
	for _, path := range []string{"/v1/trigger-task/backup-to-telegram", "/v1/trigger-task/pool-variable-cleanup"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.SetBasicAuth("svc", "svc-secret")
		if rec := serve(h, r); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d", path, rec.Code)
		}
	}
}
