package realtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWorkerDeliversTriggersAndReconnects(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("channel") != "analysis_trigger" || r.URL.Query().Get("token") != "tok" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			http.Error(w, "bad accept", http.StatusBadRequest)
			return
		}
		n := connections.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, ": keep-alive\n\n")
		// A trigger split over two data lines, as the SSE format allows.
		fmt.Fprintf(w, "event: analysis_trigger\ndata: {\"analysis_id\":\"a1\",\"environment\":[],\n")
		fmt.Fprintf(w, "data: \"data\":[{\"device\":\"d1\",\"variable\":\"v\",\"value\":%d}]}\n\n", n)
		fmt.Fprint(w, "data: not json\n\n")                             // Ignored
		fmt.Fprint(w, "data: {\"analysis_id\":\"a1\",\"data\":[]}\n\n") // Empty: ignored
		flusher.Flush()
		// The first connection ends; the worker must reconnect.
	}))
	defer server.Close()

	received := make(chan Trigger, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &Worker{Name: "test", URL: server.URL, Token: "tok", Log: quietLogger(),
		Handle: func(_ context.Context, t Trigger) { received <- t }}
	go w.Run(ctx)

	for want := 1; want <= 2; want++ {
		select {
		case trigger := <-received:
			if trigger.AnalysisID != "a1" || len(trigger.Data) != 1 || trigger.Data[0].Device != "d1" {
				t.Fatalf("trigger = %+v", trigger)
			}
			if got := fmt.Sprint(trigger.Data[0].Value); got != fmt.Sprint(want) {
				t.Fatalf("trigger from connection %s; want %d", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no trigger %d received", want)
		}
	}
}

func TestWorkerReportsRejectedTokenOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
	}))
	defer server.Close()

	rejected := make(chan string, 2)
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{Name: "test", URL: server.URL, Token: "tok", Log: quietLogger(),
		Handle:   func(context.Context, Trigger) {},
		Rejected: func(name string, _ error) { rejected <- name }}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	select {
	case <-rejected:
	case <-time.After(5 * time.Second):
		t.Fatal("rejection not reported")
	}
	cancel() // The worker waits rejectedBackoff before retrying; cancelling must stop it at once
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop on cancel")
	}
}

func TestWorkerSurvivesHandlerPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"data\":[{\"value\":\"boom\"}]}\n\n")
		fmt.Fprint(w, "data: {\"data\":[{\"value\":\"ok\"}]}\n\n")
		w.(http.Flusher).Flush()
		<-time.After(time.Second)
	}))
	defer server.Close()

	received := make(chan string, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &Worker{Name: "test", URL: server.URL, Token: "tok", Log: quietLogger(),
		Handle: func(_ context.Context, t Trigger) {
			if t.Data[0].Value == "boom" {
				panic("handler bug")
			}
			received <- fmt.Sprint(t.Data[0].Value)
		}}
	go w.Run(ctx)

	select {
	case v := <-received:
		if v != "ok" {
			t.Fatalf("received %q", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the trigger after a panic was not handled")
	}
}

func TestRedactToken(t *testing.T) {
	err := redactToken(fmt.Errorf(`Get "https://sse.tago.io/events?token=secret-123": EOF`), "secret-123")
	if got := err.Error(); got != `Get "https://sse.tago.io/events?token=REDACTED": EOF` {
		t.Fatalf("redacted error = %q", got)
	}
}
