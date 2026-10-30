// This file holds the broker that fans the events out to the connected /api/stream subscribers (the CSMS
// instances). It keeps no history: an event published while nobody listens is dropped, as before.

package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// subscriberBuffer is the number of events a slow subscriber can fall behind before it misses events.
const subscriberBuffer = 256

// summarizedTypes are the periodic events (the MQTT meter of each installation reports every 90 s) that are logged
// as an hourly count per pool instead of a line each. Every other event is logged on its own.
var summarizedTypes = map[Type]bool{TypePowerConsumptionUpdate: true}

// Broker fans events out to the subscribers.
type Broker struct {
	log    *slog.Logger
	dryRun bool

	mu          sync.Mutex
	subscribers map[chan []byte]struct{}
	counts      map[countKey]*eventCount // Summarized events since windowStart
	windowStart time.Time
}

type countKey struct {
	eventType Type
	poolCode  int
}

type eventCount struct {
	published   int       // Delivered to every subscriber
	dropped     int       // Not delivered: no subscriber connected
	slowDrops   int       // Missed by a slow subscriber (one per subscriber and event)
	lastPublish time.Time // Zero when none was published in the window
}

// NewBroker creates a broker. In dry-run mode the events are logged and not delivered.
func NewBroker(log *slog.Logger, dryRun bool) *Broker {
	return &Broker{log: log, dryRun: dryRun, subscribers: map[chan []byte]struct{}{}, counts: map[countKey]*eventCount{},
		windowStart: time.Now()}
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
	var count *eventCount
	if summarizedTypes[h.EventType] {
		key := countKey{h.EventType, h.PoolCode}
		if count = b.counts[key]; count == nil {
			count = &eventCount{}
			b.counts[key] = count
		}
	}

	if len(b.subscribers) == 0 {
		if count != nil {
			count.dropped++
			return
		}
		b.log.Warn("Events: no subscriber connected, event dropped", "event_type", h.EventType, "pool_code", h.PoolCode)
		return
	}
	for ch := range b.subscribers {
		select {
		case ch <- data:
		default:
			if count != nil {
				count.slowDrops++
				continue
			}
			b.log.Warn("Events: subscriber too slow, event dropped", "event_type", h.EventType, "pool_code", h.PoolCode)
		}
	}
	if count != nil {
		count.published++
		count.lastPublish = time.Now()
		return
	}
	b.log.Info("Events: published", "event_type", h.EventType, "pool_code", h.PoolCode, "subscribers", len(b.subscribers))
}

// RunSummaries logs the counts of the summarized events at the top of every hour, and once more when ctx ends.
func (b *Broker) RunSummaries(ctx context.Context) {
	for {
		now := time.Now()
		timer := time.NewTimer(now.Truncate(time.Hour).Add(time.Hour).Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			b.LogSummary()
			return
		case <-timer.C:
			b.LogSummary()
		}
	}
}

// LogSummary logs a line per summarized event type and pool with its counts since the previous summary, and starts
// a new window. A pool whose events stopped gets one line with no events published, then no more lines.
func (b *Broker) LogSummary() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, count := range b.counts {
		args := []any{"event_type", key.eventType, "pool_code", key.poolCode, "since", b.windowStart.Format(time.TimeOnly),
			"published", count.published, "dropped", count.dropped, "slow_drops", count.slowDrops,
			"subscribers", len(b.subscribers)}
		if !count.lastPublish.IsZero() {
			args = append(args, "last_published", count.lastPublish.Format(time.TimeOnly))
		}
		level := slog.LevelInfo
		if count.published == 0 || count.dropped > 0 || count.slowDrops > 0 {
			level = slog.LevelWarn
		}
		b.log.Log(context.Background(), level, "Events: summary", args...)

		if count.published+count.dropped == 0 {
			delete(b.counts, key) // Already reported as stopped
		} else {
			*count = eventCount{}
		}
	}
	b.windowStart = time.Now()
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
