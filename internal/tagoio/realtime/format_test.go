package realtime

// Tests of the trigger format and the reconnect behaviour, the cutover risks of the realtime connection: the SSE
// trigger format has not been checked against a real TagoIO capture yet (README, cutover step 1).

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	// TagoIO's own SDKs read the trigger from a "payload" field of the SSE event (JSON.parse(event.data).payload).
	// If the real events are wrapped like that, the worker decodes an empty trigger and drops every one of them.
	server := streamOnce(t, `data: {"payload":`+triggerJSON+"}\n\n")

	tr, ok := waitTrigger(runWorker(t, server.URL, quietLogger()), 2*time.Second)
	testutil.KnownBug(t, ok && len(tr.Data) == 1,
		`triggers wrapped in {"payload": ...} are decoded as empty and dropped (format unverified until the dry-run capture)`)
}

func TestWorkerWarnsAboutEventsWithoutTriggerData(t *testing.T) {
	// An event that decodes without data means the format is not the expected one: it must be visible at the
	// default log level, or the dry-run capture just looks like "no triggers arrived".
	var logs syncBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := streamOnce(t, `data: {"payload":`+triggerJSON+"}\n\n")

	runWorker(t, server.URL, log)
	time.Sleep(500 * time.Millisecond)
	testutil.KnownBug(t, strings.Contains(logs.String(), "level=WARN"),
		"an event without trigger data is logged at DEBUG, invisible with LOG_LEVEL=info")
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
