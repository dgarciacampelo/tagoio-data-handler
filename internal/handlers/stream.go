// This file holds the event stream of the CSMS instances (GET /api/stream).

package handlers

import (
	"fmt"
	"net/http"
	"time"
)

// streamKeepAlive is the interval of the keep-alive comments. ! neos-ocpp reconnects after 30 s without a line.
const streamKeepAlive = 15 * time.Second

// Stream sends the published events to a CSMS instance as Server-Sent Events: one "data:" line per event with its
// JSON envelope, and a comment line as keep-alive. There is no replay: events published while the CSMS is
// disconnected are lost, as before.
func (a *App) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	events, unsubscribe := a.Broker.Subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Ask the reverse proxy not to buffer the stream
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepAlive := time.NewTicker(streamKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-events:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case now := <-keepAlive.C:
			if _, err := fmt.Fprintf(w, ": ping - %s\n\n", now.UTC().Format(time.RFC3339)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
