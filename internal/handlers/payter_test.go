package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
)

// payterMux routes the Payter terminal APIs, without their authentication (covered by cmd/server's routes test).
func payterMux(app *App) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/payter-terminals", app.PayterTerminalsAPI)
	mux.HandleFunc("PUT /api/admin/payter-terminals/{serial_number}", app.SavePayterTerminalAPI)
	mux.HandleFunc("DELETE /api/admin/payter-terminals/{serial_number}", app.DeactivatePayterTerminalAPI)
	return mux
}

func callPayter(mux *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

// nextEvent returns the next event published on the stream, or fails after a second.
func nextEvent(t *testing.T, stream <-chan []byte) events.PayterTerminalManagement {
	t.Helper()
	select {
	case line := <-stream:
		var envelope struct {
			Data events.PayterTerminalManagement `json:"data"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			t.Fatalf("event %s: %v", line, err)
		}
		return envelope.Data
	case <-time.After(time.Second):
		t.Fatal("no event published")
	}
	return events.PayterTerminalManagement{}
}

func noEvent(t *testing.T, stream <-chan []byte) {
	t.Helper()
	select {
	case line := <-stream:
		t.Fatalf("unexpected event %s", line)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTheCSMSReadsEveryPayterTerminal(t *testing.T) {
	rec := callPayter(payterMux(newTestApp(t)), http.MethodGet, "/api/payter-terminals", "")

	var terminals []dto.PayterTerminal
	if err := json.Unmarshal(rec.Body.Bytes(), &terminals); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("status = %d, %v: %s", rec.Code, err, rec.Body)
	}
	if len(terminals) != 3 || terminals[0].SerialNumber != "P6X20222100772" || terminals[0].LinkedStations[0] != "22V20538CCPOS" {
		t.Fatalf("terminals = %+v; want the 3 seeded ones", terminals)
	}
}

func TestSavingAPayterTerminalTellsTheCSMS(t *testing.T) {
	app := newTestApp(t)
	stream, unsubscribe := app.Broker.Subscribe()
	defer unsubscribe()

	body := `{"pool_code": 221006, "is_pool_shared": false, "linked_stations": [" 22V20539CCPOS ", "22V20539CCPOS", ""]}`
	rec := callPayter(payterMux(app), http.MethodPut, "/api/admin/payter-terminals/p6x20250000001", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	event := nextEvent(t, stream)
	want := events.PayterTerminalManagement{Header: events.NewHeader(events.TypeManagePayterTerminal, 221006),
		SerialNumber: "P6X20250000001", Action: events.PayterTerminalCreate, LinkedStations: []string{"22V20539CCPOS"}}
	if event.SerialNumber != want.SerialNumber || event.Header != want.Header || event.Action != want.Action ||
		strings.Join(event.LinkedStations, ",") != "22V20539CCPOS" {
		t.Errorf("event = %+v; want %+v", event, want)
	}
	if !strings.Contains(rec.Body.String(), `"serial_number":"P6X20250000001"`) {
		t.Errorf("answer = %s; want every terminal, with the new one", rec.Body)
	}
}

func TestAnInvalidPayterTerminalIsRejectedWithoutTellingTheCSMS(t *testing.T) {
	tests := map[string]struct {
		serial, body string
		want         int
	}{
		"bad serial":        {"ABC123", `{"pool_code": 221006, "linked_stations": ["ST"]}`, http.StatusUnprocessableEntity},
		"bad pool":          {"P6X20250000001", `{"pool_code": 12, "linked_stations": ["ST"]}`, http.StatusUnprocessableEntity},
		"no stations":       {"P6X20250000001", `{"pool_code": 221006, "linked_stations": [" "]}`, http.StatusUnprocessableEntity},
		"slash in station":  {"P6X20250000001", `{"pool_code": 221006, "linked_stations": ["A/B"]}`, http.StatusUnprocessableEntity},
		"not JSON":          {"P6X20250000001", `pool_code=221006`, http.StatusBadRequest},
		"second shared one": {"P6X20250000001", `{"pool_code": 241004, "is_pool_shared": true}`, http.StatusConflict},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			app := newTestApp(t)
			stream, unsubscribe := app.Broker.Subscribe()
			defer unsubscribe()

			rec := callPayter(payterMux(app), http.MethodPut, "/api/admin/payter-terminals/"+tt.serial, tt.body)

			if rec.Code != tt.want {
				t.Errorf("status = %d; want %d: %s", rec.Code, tt.want, rec.Body)
			}
			noEvent(t, stream)
		})
	}
}

func TestDeactivatingAPayterTerminalTellsTheCSMSOfItsPool(t *testing.T) {
	app := newTestApp(t)
	stream, unsubscribe := app.Broker.Subscribe()
	defer unsubscribe()
	mux := payterMux(app)

	rec := callPayter(mux, http.MethodDelete, "/api/admin/payter-terminals/P6X20231300653", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	event := nextEvent(t, stream)
	if event.PoolCode != 241004 || event.SerialNumber != "P6X20231300653" || event.Action != events.PayterTerminalDelete {
		t.Errorf("event = %+v; want the deactivation of P6X20231300653 at pool 241004", event)
	}

	if rec := callPayter(mux, http.MethodDelete, "/api/admin/payter-terminals/P6X99999999999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown terminal: status = %d; want 404", rec.Code)
	}
	noEvent(t, stream)
}
