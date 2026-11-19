package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/provisioning"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

// withProvisioning gives the test app a provisioning service on a fake TagoIO.
func withProvisioning(t *testing.T, app *App) *testutil.FakeTagoIO {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tago := testutil.NewFakeTagoIO(t)
	registry := devices.NewRegistry(app.Store, tago.Client(), "MASTER-", log)
	madrid, _ := time.LoadLocation("Europe/Madrid")
	app.Provisioning = provisioning.New(tago.Client(), registry, app.Store, discardBucket{}, log, madrid)
	return tago
}

type discardBucket struct{}

func (discardBucket) Insert(context.Context, int, ...tagoio.DataInput) error { return nil }

func TestProvisionCPOAnswersAnExistingUserWithoutAPassword(t *testing.T) {
	app := newTestApp(t)
	tago := withProvisioning(t, app)
	tago.AddUser(testutil.FakeUser{ID: "user-1", Email: "a@b.es", Active: true, Tags: []tagoio.Tag{{Key: "group_id", Value: "251001"}}})

	body := `{"user_email":"a@b.es","company_name":"ACME","cpo_fiscal_id":"B1","company_address":"C/ 1",
		"contact_phone":"985","contact_email":"c@b.es","manager_name":"Ana","manager_dni":"1X","iban":"ES00",
		"payment_system":"No","description":"Garaje"}`
	rec := httptest.NewRecorder()
	app.ProvisionCPO(rec, serviceRequest(http.MethodPost, "/api/provision-cpo", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var answer map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer["user_created"] != false || answer["user_existed"] != true || answer["user_linked"] != true ||
		answer["device_created"] != true || answer["generated_password"] != nil || answer["error_message"] != nil ||
		answer["user_error"] != nil || answer["pool_code"] == nil || answer["device_id"] == nil {
		t.Errorf("answer = %s", rec.Body)
	}
}

func TestLinkPoolUserAPI(t *testing.T) {
	app := newTestApp(t)
	tago := withProvisioning(t, app)
	tago.AddDevice("dev-5", "MASTER-BUSINESS-261005", "token-5")
	tago.AddUser(testutil.FakeUser{ID: "user-1", Email: "a@b.es", Active: true, Tags: []tagoio.Tag{{Key: "group_id", Value: "261005"}}})

	link := func(pool, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/admin/pools/"+pool+"/users", strings.NewReader(body))
		r.SetPathValue("pool_code", pool)
		rec := httptest.NewRecorder()
		app.LinkPoolUserAPI(rec, r)
		return rec
	}

	rec := link("261006", `{"email": "a@b.es"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var answer linkPoolUserResult
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.PoolCode != 261006 || !answer.DeviceCreated || answer.DeviceID == "" || answer.UserID != "user-1" ||
		!answer.TagAdded || strings.Join(answer.GroupIDs, ",") != "261005,261006" {
		t.Errorf("answer = %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "token") {
		t.Errorf("the answer carries a token: %s", rec.Body)
	}

	for name, tc := range map[string]struct {
		pool, body string
		want       int
	}{
		"unknown email": {"261006", `{"email": "x@b.es"}`, http.StatusNotFound},
		"not an email":  {"261006", `{"email": "Ana <a@b.es>"}`, http.StatusUnprocessableEntity},
		"no email":      {"261006", `{}`, http.StatusUnprocessableEntity},
		"bad pool code": {"0", `{"email": "a@b.es"}`, http.StatusBadRequest},
		"non-numeric":   {"abc", `{"email": "a@b.es"}`, http.StatusBadRequest},
		"invalid JSON":  {"261006", `{"email":`, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := link(tc.pool, tc.body); rec.Code != tc.want {
				t.Errorf("status %d; want %d: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
