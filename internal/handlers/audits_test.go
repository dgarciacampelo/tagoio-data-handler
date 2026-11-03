package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

// historyRecord is a session of the TagoIO history, as pushSession writes it.
func historyRecord(id string, transactionID int, startDate, cardAlias string) tagoio.DataRecord {
	return tagoio.DataRecord{
		ID: id, Variable: chargepoints.HistoryVariable, Value: transactionID, Group: strconv.Itoa(transactionID),
		Metadata: map[string]any{"card_alias": cardAlias, "start_date": startDate, "time_band": "07:37 - 10:13", "cost": 2.43},
	}
}

// auditAliasApp is a test app whose pool 261001 has a TagoIO device with the history of two sessions that share
// transaction 11297 (another charge point's, on another day), and the audit row of the first one.
func auditAliasApp(t *testing.T) (*App, *testutil.FakeTagoIO, int64) {
	t.Helper()
	ctx := context.Background()
	app := newTestApp(t)

	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")
	tago.AddRecord("token-1", historyRecord("r1", 11297, "02/10/2026", "RFID3A"))
	tago.AddRecord("token-1", historyRecord("r2", 11297, "03/11/2026", "Carga libre"))
	registry := devices.NewRegistry(app.Store, tago.Client(), "MASTER-", app.Log)
	if err := registry.Register(ctx, devices.Device{PoolCode: 261001, DeviceID: "dev-1", Token: "token-1"}); err != nil {
		t.Fatal(err)
	}
	app.Bucket = bucket.New(tago.Client(), registry, app.ChargePoints, app.Log)

	_, err := app.Store.InsertSession(ctx, store.SessionRecord{PoolCode: 261001, StationName: "22V20537CCPOS",
		ConnectorID: 2, TransactionID: 11297, CardAlias: "Carga libre", StartDate: "03/11/2026", TimeBand: "07:37 - 10:13"})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := app.Store.RecentSessions(ctx, 261001, 1)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("RecentSessions = %+v, %v", sessions, err)
	}
	return app, tago, sessions[0].ID
}

func correctAlias(app *App, sessionID, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/admin/audits/"+sessionID+"/card-alias", strings.NewReader(body))
	r.SetPathValue("session_id", sessionID)
	app.CorrectAuditCardAliasAPI(rec, r)
	return rec
}

// aliases returns the card alias of each history record of the fake bucket, by record ID.
func aliases(tago *testutil.FakeTagoIO) map[string]any {
	out := map[string]any{}
	for _, r := range tago.Records("token-1", chargepoints.HistoryVariable) {
		out[r.ID] = r.Metadata["card_alias"]
	}
	return out
}

func TestCorrectingTheCardAliasFixesTheAuditAndTheTagoIOHistory(t *testing.T) {
	app, tago, sessionID := auditAliasApp(t)
	id := strconv.FormatInt(sessionID, 10)

	rec := correctAlias(app, id, `{"card_alias": " TPV Virtual "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var result dto.AuditCardAliasResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.CardAlias != "TPV Virtual" || result.TransactionID != 11297 || result.TagoIORecords != 1 {
		t.Errorf("result = %+v", result)
	}

	session, err := app.Store.Session(context.Background(), sessionID)
	if err != nil || session.CardAlias != "TPV Virtual" {
		t.Errorf("audit row = %+v, %v", session, err)
	}
	// Only the record of this session changes, with the rest of its metadata kept
	if got := aliases(tago); got["r1"] != "RFID3A" || got["r2"] != "TPV Virtual" {
		t.Errorf("TagoIO history aliases = %v", got)
	}
	for _, r := range tago.Records("token-1", chargepoints.HistoryVariable) {
		if r.ID == "r2" && r.Metadata["cost"] != 2.43 {
			t.Errorf("metadata after the edit = %v", r.Metadata)
		}
	}
}

func TestAFailedTagoIOEditCanBeRetried(t *testing.T) {
	app, tago, sessionID := auditAliasApp(t)
	id := strconv.FormatInt(sessionID, 10)
	tago.Fail("PUT /data", true)

	if rec := correctAlias(app, id, `{"card_alias": "TPV Virtual"}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("status with TagoIO failing = %d; want 502", rec.Code)
	}
	if got := aliases(tago)["r2"]; got != "Carga libre" {
		t.Fatalf("TagoIO alias after a failed edit = %v", got)
	}

	tago.Fail("PUT /data", false)
	if rec := correctAlias(app, id, `{"card_alias": "TPV Virtual"}`); rec.Code != http.StatusOK {
		t.Fatalf("status of the retry = %d: %s", rec.Code, rec.Body)
	}
	if got := aliases(tago)["r2"]; got != "TPV Virtual" {
		t.Errorf("TagoIO alias after the retry = %v", got)
	}
}

func TestCardAliasCorrectionRejectsBadRequests(t *testing.T) {
	app, tago, sessionID := auditAliasApp(t)
	id := strconv.FormatInt(sessionID, 10)

	cases := []struct {
		name, sessionID, body string
		want                  int
	}{
		{"unknown session", "999", `{"card_alias": "TPV Virtual"}`, http.StatusNotFound},
		{"non-integer id", "abc", `{"card_alias": "TPV Virtual"}`, http.StatusBadRequest},
		{"blank alias", id, `{"card_alias": "  "}`, http.StatusUnprocessableEntity},
		{"alias too long", id, `{"card_alias": "` + strings.Repeat("x", maxCardAliasLength+1) + `"}`, http.StatusUnprocessableEntity},
		{"invalid JSON", id, `{"card_alias":`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := correctAlias(app, c.sessionID, c.body); rec.Code != c.want {
			t.Errorf("%s: status = %d; want %d", c.name, rec.Code, c.want)
		}
	}
	if tago.Calls("PUT /data") != 0 {
		t.Error("a rejected request edited the TagoIO history")
	}
}
