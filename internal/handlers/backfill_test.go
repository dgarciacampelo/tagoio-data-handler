package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// backfillBody is a back-fill request as neos-ocpp's outbox sends it: the stored ChargingSessionUpdate bodies.
func backfillBody(updates ...string) string {
	return `{"updates": [` + strings.Join(updates, ",") + `]}`
}

func postBackfill(t *testing.T, app *App, body string) (int, backfillResponse) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/charging-session-backfill", app.ChargingSessionBackfill)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, serviceRequest(http.MethodPost, "/v1/charging-session-backfill", body))
	var answer backfillResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &answer)
	return rec.Code, answer
}

func TestChargingSessionBackfillContract(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	running := neosSessionUpdate("INPROGRESS", "", "")
	earlier := strings.Replace(running, "2026-10-21T10:15:30.123456+02:00", "2026-10-21T10:14:30.123456+02:00", 1)
	ended := strings.Replace(neosSessionUpdate("COMPLETED", "Cost", "09:00 - 10:20"),
		"2026-10-21T10:15:30.123456+02:00", "2026-10-21T10:20:00.5+02:00", 1)
	invalid := strings.Replace(running, `"pool_code": 261001`, `"pool_code": 0`, 1)
	malformed := `{"pool_code": "not a number"}`

	code, answer := postBackfill(t, app, backfillBody(running, earlier, invalid, malformed, ended))
	if code != http.StatusOK {
		t.Fatalf("status = %d; want 200", code)
	}
	if answer.Stored != 3 || answer.Duplicates != 0 || len(answer.Rejected) != 2 {
		t.Fatalf("answer = %+v; want 3 stored and the 2 bad updates rejected", answer)
	}
	if answer.Rejected[0].Index != 2 || answer.Rejected[1].Index != 3 {
		t.Fatalf("rejected = %+v; want the updates at 2 and 3", answer.Rejected)
	}

	telemetry, _ := app.Store.SessionTelemetry(ctx, 261001, "22HBS0452HBS22", 1, 4321)
	sessions, _ := app.Store.RecentSessions(ctx, 261001, 10)
	if len(telemetry) != 3 || len(sessions) != 1 {
		t.Fatalf("telemetry %d rows, history %d rows; want 3 meter values and the finished session", len(telemetry), len(sessions))
	}

	// The CSMS didn't get the answer and sends the same batch again
	if code, answer := postBackfill(t, app, backfillBody(running, earlier, ended)); code != http.StatusOK || answer.Stored != 0 || answer.Duplicates != 3 {
		t.Fatalf("repeated back-fill = %d %+v; want 3 duplicates", code, answer)
	}
}

func TestChargingSessionBackfillRejectsBadRequests(t *testing.T) {
	app := newTestApp(t)
	if code, _ := postBackfill(t, app, `{"updates": {}}`); code != http.StatusUnprocessableEntity {
		t.Errorf("updates that aren't a list: status %d; want 422", code)
	}
	tooMany := make([]string, MaxBackfillUpdates+1)
	for i := range tooMany {
		tooMany[i] = "{}"
	}
	if code, _ := postBackfill(t, app, backfillBody(tooMany...)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("%d updates: status %d; want 413", len(tooMany), code)
	}
	if code, answer := postBackfill(t, app, `{"updates": []}`); code != http.StatusOK || answer.Rejected == nil {
		t.Errorf("an empty back-fill = %d %+v; want 200 with an empty rejected list", code, answer)
	}
}
