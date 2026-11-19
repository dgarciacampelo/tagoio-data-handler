// This file holds the event stream of the CSMS instances (GET /api/stream).

package handlers

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/events"
)

// streamKeepAlive is the interval of the keep-alive comments. ! neos-ocpp reconnects after 30 s without a line.
const streamKeepAlive = 15 * time.Second

// Stream sends the published events to a CSMS instance as Server-Sent Events: one "id:" and "data:" line per event
// (its ID and its JSON envelope), and a comment line as keep-alive. A CSMS that reconnects sends the ID of the last
// event it received (Last-Event-ID) and first gets the events it missed, if the broker still keeps them (also
// across a restart of this service). Right after connecting, the stream sends the ID of the newest event, so a CSMS
// that received nothing yet still has one to send.
func (a *App) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	sub, unsubscribe := a.Broker.Subscribe(r.Header.Get("Last-Event-ID"))
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Ask the reverse proxy not to buffer the stream
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	for _, m := range sub.Replay {
		if writeEvent(w, m) != nil {
			return
		}
	}
	if len(sub.Replay) == 0 {
		fmt.Fprintf(w, "id: %s\n\n", sub.LastID)
	}
	flusher.Flush()

	keepAlive := time.NewTicker(streamKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-sub.Events:
			if writeEvent(w, m) != nil {
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

// writeEvent writes one event with its ID. The CSMS takes it at its blank line, so an event cut by a dropped
// connection is replayed, not lost.
func writeEvent(w io.Writer, m events.Message) error {
	_, err := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", m.ID, m.Data)
	return err
}
