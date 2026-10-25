// Package realtime receives the triggers of TagoIO analyses that run outside TagoIO ("External" analyses), over
// TagoIO's Server-Sent Events connection (the Socket.IO connection it replaces is turned off on 2026-11-19).
//
// Each analysis has its own token and its own long-lived request:
//
//	GET {sse}/events?channel=analysis_trigger&token=<analysis token>   (Accept: text/event-stream)
//
// Every SSE event's data is {"channel", "payload"}, the payload being a JSON trigger, {"analysis_id", "environment",
// "data": [records], "session_id", "token"} (captured on 2026-10-25, testdata/sse_*.json). TagoIO sends a
// keep-alive about every 5 seconds, so a minute without any line means the connection is dead.
package realtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

const (
	idleTimeout     = 60 * time.Second // No line at all for this long: reconnect
	baseBackoff     = time.Second      // First reconnect delay, doubled after each failure
	maxBackoff      = 60 * time.Second
	rejectedBackoff = 5 * time.Minute // Delay after TagoIO rejected the token (401/403): retrying fast is pointless
	maxEventSize    = 4 << 20         // Largest accepted SSE line
)

// Trigger is one analysis run request.
type Trigger struct {
	AnalysisID  string              `json:"analysis_id"`
	Environment []EnvironmentEntry  `json:"environment"`
	Data        []tagoio.DataRecord `json:"data"`
}

// EnvironmentEntry is an analysis environment variable.
type EnvironmentEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Handler processes a trigger. The triggers of one analysis run one at a time, in arrival order, on a goroutine of
// their own, so a slow handler never stalls the connection.
type Handler func(ctx context.Context, t Trigger)

// queueSize is the number of triggers of one analysis that can wait for the handler.
const queueSize = 64

// Worker keeps the realtime connection of one analysis open.
type Worker struct {
	Name     string                       // Analysis name, for the logs
	URL      string                       // SSE endpoint, e.g. https://sse.tago.io/events
	Token    string                       // Analysis token
	Handle   Handler                      // Trigger handler
	Log      *slog.Logger                 // Logger
	Rejected func(name string, err error) // Called once when TagoIO rejects the token, until a connection succeeds again

	client *http.Client
	queue  chan Trigger
}

// Run connects and reconnects until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.client = &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
	log := w.Log.With("analysis", w.Name)

	w.queue = make(chan Trigger, queueSize)
	go w.consume(ctx, log)

	backoff := baseBackoff
	rejectedReported := false
	for ctx.Err() == nil {
		connected, err := w.stream(ctx, log)
		if ctx.Err() != nil {
			return
		}

		delay := backoff
		var rejected *rejectedError
		switch {
		case errors.As(err, &rejected):
			log.Error("TagoIO realtime: analysis token rejected", "status", rejected.status)
			if !rejectedReported && w.Rejected != nil {
				w.Rejected(w.Name, err)
			}
			rejectedReported = true
			delay = rejectedBackoff
		case connected:
			// The stream was up and then dropped: start the backoff again.
			log.Warn("TagoIO realtime: connection closed, reconnecting", "error", err)
			rejectedReported = false
			backoff = baseBackoff
			delay = baseBackoff
		default:
			log.Warn("TagoIO realtime: connection failed", "error", err, "retry_in", delay)
			backoff = min(backoff*2, maxBackoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// rejectedError is a 401 or 403 answer: the analysis token is wrong or was revoked.
type rejectedError struct {
	status int
}

func (e *rejectedError) Error() string {
	return fmt.Sprintf("analysis token rejected (HTTP %d)", e.status)
}

// stream opens the connection and dispatches its events until it ends. connected reports whether TagoIO
// accepted the connection, so the caller can tell a dropped stream from a failed attempt.
func (w *Worker) stream(ctx context.Context, log *slog.Logger) (connected bool, err error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	endpoint := w.URL + "?" + url.Values{"channel": {"analysis_trigger"}, "token": {w.Token}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := w.client.Do(req)
	if err != nil {
		return false, redactToken(err, w.Token)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return false, &rejectedError{status: resp.StatusCode}
	case resp.StatusCode != http.StatusOK:
		return false, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	log.Info("TagoIO realtime: analysis connected")

	// The watchdog cancels the request when no line (event or keep-alive) arrives for idleTimeout.
	errIdle := errors.New("no data or keep-alive received for " + idleTimeout.String())
	watchdog := time.AfterFunc(idleTimeout, func() { cancel(errIdle) })
	defer watchdog.Stop()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), maxEventSize)

	var data bytes.Buffer
	for scanner.Scan() {
		watchdog.Reset(idleTimeout)
		line := scanner.Bytes()

		switch {
		case len(line) == 0: // A blank line ends the event
			if data.Len() > 0 {
				w.dispatch(log, data.Bytes())
				data.Reset()
			}
		case line[0] == ':': // Comment (keep-alive)
		default:
			field, value, _ := bytes.Cut(line, []byte(":"))
			value = bytes.TrimPrefix(value, []byte(" "))
			if string(field) == "data" {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.Write(value)
			}
		}
	}

	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return true, cause
	}
	if err := scanner.Err(); err != nil {
		return true, redactToken(err, w.Token)
	}
	return true, errors.New("stream ended by the server")
}

// event is the data of a TagoIO realtime SSE event: the trigger travels in "payload", next to the channel
// (captured from production on 2026-10-25, testdata/sse_*.json). The payload also carries the analysis token.
type event struct {
	Channel string          `json:"channel"`
	Payload json.RawMessage `json:"payload"`
}

// DecodeEvent decodes the data of an SSE event into a trigger. It unwraps TagoIO's {"channel", "payload"} envelope
// and also accepts a bare trigger. Numbers stay json.Number, as written by TagoIO.
func DecodeEvent(raw []byte) (Trigger, error) {
	var e event
	if err := json.Unmarshal(raw, &e); err == nil && len(e.Payload) > 0 && !bytes.Equal(e.Payload, []byte("null")) {
		raw = e.Payload
	}
	var trigger Trigger
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&trigger)
	return trigger, err
}

// dispatch decodes one event and queues it for the handler.
func (w *Worker) dispatch(log *slog.Logger, raw []byte) {
	trigger, err := DecodeEvent(raw)
	if err != nil {
		log.Warn("TagoIO realtime: ignoring event that is not a trigger", "error", err,
			"data", w.redacted(raw))
		return
	}
	if len(trigger.Data) == 0 {
		// A trigger always carries records: an empty one means the event format is not the expected one.
		log.Warn("TagoIO realtime: event without trigger data ignored", "analysis_id", trigger.AnalysisID,
			"data", w.redacted(raw))
		return
	}

	select {
	case w.queue <- trigger:
	default:
		log.Error("TagoIO realtime: handler queue full, trigger dropped", "queued", queueSize)
	}
}

// consume runs the queued triggers until ctx is cancelled.
func (w *Worker) consume(ctx context.Context, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case trigger := <-w.queue:
			w.run(ctx, log, trigger)
		}
	}
}

// run calls the handler, isolating the worker from a handler panic.
func (w *Worker) run(ctx context.Context, log *slog.Logger, trigger Trigger) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("TagoIO realtime: analysis handler panicked", "panic", r)
		}
	}()
	w.Handle(ctx, trigger)
}

// redacted returns the start of an event's data for the logs, without the analysis token it carries.
func (w *Worker) redacted(raw []byte) string {
	text := string(raw)
	if w.Token != "" {
		text = strings.ReplaceAll(text, w.Token, "REDACTED")
	}
	return truncate([]byte(text), 300)
}

// redactToken removes the analysis token from an error (url.Error includes the request URL).
func redactToken(err error, token string) error {
	if err == nil || token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "REDACTED"))
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
