package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/events"
)

// sseMessage is a message of the stream: the fields before a blank line (comments left out).
type sseMessage struct {
	id, data string
}

// openStream connects to the event stream like a CSMS and returns its messages; the stream closes with the test or
// with the returned function.
func openStream(t *testing.T, url, lastEventID string) (<-chan sseMessage, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if lastEventID != "" {
		r.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}

	messages := make(chan sseMessage, 16)
	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var m sseMessage
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if m != (sseMessage{}) {
					messages <- m
				}
				m = sseMessage{}
			case strings.HasPrefix(line, "id: "):
				m.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				m.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return messages, cancel
}

func next(t *testing.T, messages <-chan sseMessage) sseMessage {
	t.Helper()
	select {
	case m := <-messages:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no message on the stream")
	}
	return sseMessage{}
}

func TestStreamReplaysTheEventsAReconnectingCSMSMissed(t *testing.T) {
	app := newTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(app.Stream))
	t.Cleanup(server.Close) // After the streams close (their cleanups run first): Close waits for open requests

	// A CSMS that just started: no replay, only the ID of the newest event (none yet).
	first, disconnect := openStream(t, server.URL, "")
	start := next(t, first)
	if start.data != "" || !strings.HasSuffix(start.id, "-0") {
		t.Fatalf("first message = %+v; want only the ID of no event yet", start)
	}
	disconnect()

	// Published while the CSMS reconnects (this service restarting, or its stream dropped).
	app.Broker.Publish(events.VPOSStop{Header: events.NewHeader(events.TypeVPOSStopTransaction, 261001), StationName: "ST1", ConnectorID: 1})

	again, _ := openStream(t, server.URL, start.id)
	missed := next(t, again)
	if !strings.HasSuffix(missed.id, "-1") || !strings.Contains(missed.data, `"event":"virtual_POS_stop_transaction"`) {
		t.Fatalf("first message after reconnecting = %+v; want the missed event", missed)
	}
	app.Broker.Publish(events.VPOSStop{Header: events.NewHeader(events.TypeVPOSStopTransaction, 261002), StationName: "ST2", ConnectorID: 1})
	live := next(t, again)
	if !strings.HasSuffix(live.id, "-2") || !strings.Contains(live.data, `"pool_code":261002`) {
		t.Fatalf("live message = %+v; want the new event", live)
	}
}
