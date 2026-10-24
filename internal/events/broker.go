// This file holds the broker that fans the events out to the connected /api/stream subscribers (the CSMS
// instances). It keeps no history: an event published while nobody listens is dropped, as before.

package events

import (
	"encoding/json"
	"log/slog"
	"sync"
)

// subscriberBuffer is the number of events a slow subscriber can fall behind before it misses events.
const subscriberBuffer = 256

// Broker fans events out to the subscribers.
type Broker struct {
	log    *slog.Logger
	dryRun bool

	mu          sync.Mutex
	subscribers map[chan []byte]struct{}
}

// NewBroker creates a broker. In dry-run mode the events are logged and not delivered.
func NewBroker(log *slog.Logger, dryRun bool) *Broker {
	return &Broker{log: log, dryRun: dryRun, subscribers: map[chan []byte]struct{}{}}
}

// Envelope is the JSON object sent in each SSE data line: {"event": <type>, "data": <event>}.
type Envelope struct {
	Event Type  `json:"event"`
	Data  Event `json:"data"`
}

// Marshal encodes an event in its envelope, on a single line (neos-ocpp parses each data line on its own).
func Marshal(e Event) ([]byte, error) {
	return json.Marshal(Envelope{Event: e.header().EventType, Data: e})
}

// Publish sends an event to every subscriber, without blocking: a subscriber whose buffer is full misses it.
func (b *Broker) Publish(e Event) {
	h := e.header()
	data, err := Marshal(e)
	if err != nil {
		b.log.Error("Events: encoding failed", "event_type", h.EventType, "pool_code", h.PoolCode, "error", err)
		return
	}
	if b.dryRun {
		b.log.Info("Events: dry run, event not delivered", "event", string(data))
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subscribers) == 0 {
		b.log.Warn("Events: no subscriber connected, event dropped", "event_type", h.EventType, "pool_code", h.PoolCode)
		return
	}
	for ch := range b.subscribers {
		select {
		case ch <- data:
		default:
			b.log.Warn("Events: subscriber too slow, event dropped", "event_type", h.EventType, "pool_code", h.PoolCode)
		}
	}
	b.log.Info("Events: published", "event_type", h.EventType, "pool_code", h.PoolCode, "subscribers", len(b.subscribers))
}

// Subscribe registers a subscriber; call the returned function to unregister it.
func (b *Broker) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, subscriberBuffer)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	count := len(b.subscribers)
	b.mu.Unlock()
	b.log.Info("Events: subscriber connected", "subscribers", count)

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, ch)
			count := len(b.subscribers)
			b.mu.Unlock()
			b.log.Info("Events: subscriber disconnected", "subscribers", count)
		})
	}
}
