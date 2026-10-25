package realtime

// Tests of the trigger format and the reconnect behaviour of the realtime connection. The SSE trigger format was
// captured from production on 2026-10-25: testdata/sse_*.json.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

const triggerJSON = `{"analysis_id":"a1","environment":[{"key":"k","value":"v"}],"data":[{"device":"d1","variable":"v","value":7}]}`

// syncBuffer is a bytes.Buffer safe for the worker's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// streamOnce serves body on one SSE connection and keeps it open until the test ends, so the worker doesn't
// reconnect (and log about it) during the test.
func streamOnce(t *testing.T, body string) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
		w.(http.Flusher).Flush()
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(done); server.Close() })
	return server
}

// runWorker starts a worker against url and returns the triggers it hands to its handler.
func runWorker(t *testing.T, url string, log *slog.Logger) <-chan Trigger {
	t.Helper()
	received := make(chan Trigger, 8)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := &Worker{Name: "test", URL: url, Token: "tok", Log: log, Handle: func(_ context.Context, tr Trigger) { received <- tr }}
	go w.Run(ctx)
	return received
}

func waitTrigger(received <-chan Trigger, timeout time.Duration) (Trigger, bool) {
	select {
	case tr := <-received:
		return tr, true
	case <-time.After(timeout):
		return Trigger{}, false
	}
}

func TestWorkerAcceptsCRLFLineEndings(t *testing.T) {
	// The SSE format allows CRLF line endings; bufio.ScanLines drops the CR.
	server := streamOnce(t, "event: analysis_trigger\r\ndata: "+triggerJSON+"\r\n\r\n")

	tr, ok := waitTrigger(runWorker(t, server.URL, quietLogger()), 3*time.Second)
	if !ok || tr.AnalysisID != "a1" || len(tr.Data) != 1 || tr.Data[0].Device != "d1" {
		t.Fatalf("trigger = %+v (received: %v)", tr, ok)
	}
}

func TestWorkerDecodesTheEnvironment(t *testing.T) {
	server := streamOnce(t, "data: "+triggerJSON+"\n\n")

	tr, ok := waitTrigger(runWorker(t, server.URL, quietLogger()), 3*time.Second)
	if !ok || len(tr.Environment) != 1 || tr.Environment[0] != (EnvironmentEntry{Key: "k", Value: "v"}) {
		t.Fatalf("environment = %+v (received: %v)", tr.Environment, ok)
	}
}

func TestWorkerAcceptsTriggerWrappedInPayload(t *testing.T) {
	// TagoIO sends {"channel", "payload": trigger} (testdata/sse_*.json). Before the fix of 2026-10-25 the worker
	// decoded the top level and dropped every trigger in production.
	server := streamOnce(t, `data: {"channel":"analysis_trigger.a1","payload":`+triggerJSON+"}\n\n")

	tr, ok := waitTrigger(runWorker(t, server.URL, quietLogger()), 3*time.Second)
	if !ok || tr.AnalysisID != "a1" || len(tr.Data) != 1 || tr.Data[0].Device != "d1" {
		t.Fatalf("trigger = %+v (received: %v)", tr, ok)
	}
}

func TestWorkerWarnsAboutEventsWithoutTriggerData(t *testing.T) {
	// An event that decodes without data means the format changed: it must be visible at the default log level,
	// without the analysis token the payload carries.
	var logs syncBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := streamOnce(t, `data: {"channel":"analysis_trigger.a1","body":{"data":[{"value":1}]},"token":"tok"}`+"\n\n")

	runWorker(t, server.URL, log)
	time.Sleep(500 * time.Millisecond)
	got := logs.String()
	if !strings.Contains(got, "level=WARN") || !strings.Contains(got, "without trigger data") {
		t.Fatalf("no warning for an event without trigger data: %q", got)
	}
	if strings.Contains(got, `"token":"tok"`) || !strings.Contains(got, "REDACTED") {
		t.Errorf("the warning leaks the analysis token: %q", got)
	}
}

// capturedEvent reads an SSE event captured from production (tokens and IDs redacted).
func capturedEvent(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSpace(raw)
}

func TestDecodeCapturedEvents(t *testing.T) {
	t.Run("change_availability", func(t *testing.T) {
		tr, err := DecodeEvent(capturedEvent(t, "sse_change_availability.json"))
		if err != nil || tr.AnalysisID == "" || len(tr.Data) != 4 {
			t.Fatalf("trigger = %+v, %v", tr, err)
		}
		first := tr.Data[0]
		if first.Variable != "state" || first.Value != "22HBS0452HBS22" || first.Group != "22HBS0452HBS22" || first.Device == "" {
			t.Errorf("first record = %+v", first)
		}
		if len(tr.Environment) != 2 || tr.Environment[0].Key != "_dashboard_id" {
			t.Errorf("environment = %+v", tr.Environment)
		}
	})

	t.Run("power_consumption_update", func(t *testing.T) {
		tr, err := DecodeEvent(capturedEvent(t, "sse_power_consumption_update.json"))
		if err != nil || len(tr.Data) != 1 {
			t.Fatalf("trigger = %+v, %v", tr, err)
		}
		r := tr.Data[0]
		if r.Params["P"] != json.Number("29.88") || r.Metadata["mqtt_topic"] != "Bivocom/MASTER-BUSINESS-221006" || r.Device != "" {
			t.Errorf("record = %+v", r)
		}
	})

	t.Run("bare trigger", func(t *testing.T) {
		tr, err := DecodeEvent([]byte(triggerJSON))
		if err != nil || tr.AnalysisID != "a1" || len(tr.Data) != 1 {
			t.Fatalf("trigger = %+v, %v", tr, err)
		}
	})
}

func TestWorkerDeliversACapturedEvent(t *testing.T) {
	server := streamOnce(t, "data: "+string(capturedEvent(t, "sse_change_availability.json"))+"\n\n")

	tr, ok := waitTrigger(runWorker(t, server.URL, quietLogger()), 3*time.Second)
	if !ok || len(tr.Data) != 4 || tr.Data[0].Variable != "state" {
		t.Fatalf("trigger = %+v (received: %v)", tr, ok)
	}
}

func TestWorkerBacksOffWhenTheStreamClosesAtOnce(t *testing.T) {
	// TagoIO accepts the request (200) and closes it at once, e.g. an in-band token error or maintenance.
	t.Parallel()
	var connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connections.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{Name: "test", URL: server.URL, Token: "tok", Log: quietLogger(), Handle: func(context.Context, Trigger) {}}
	go w.Run(ctx)
	time.Sleep(4500 * time.Millisecond)
	cancel()

	// With an exponential backoff (1 s, 2 s, 4 s...) there are at most 3 connections in 4.5 s; a backoff that
	// resets on every accepted connection retries every second: 5 connections.
	got := connections.Load()
	if got == 0 {
		t.Fatal("the worker never connected")
	}
	testutil.KnownBug(t, got <= 3,
		fmt.Sprintf("the backoff resets whenever TagoIO answers 200, so a stream that closes at once is retried every second (%d connections in 4.5 s)", got))
}
