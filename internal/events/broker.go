// This file holds the broker that fans the events out to the connected /api/stream subscribers (the CSMS
// instances). It keeps the events of the last replayWindow, so a CSMS that reconnects (its stream dropped, or this
// service restarted) gets the ones it missed: it sends the ID of the last event it received (Last-Event-ID).

package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// subscriberBuffer is the number of events a slow subscriber can fall behind before it misses events.
const subscriberBuffer = 256

// replayWindow is how long an event is kept for the subscribers that reconnect. A CSMS reconnects 1 to 8 s after
// this service restarts (neos-ocpp waits 1, 2, 5 and 10 s between attempts). An event older than this is dropped,
// and logged when it never reached a subscriber.
const replayWindow = 30 * time.Second

// replayCapacity bounds the kept events: about one per pool every 90 s (the MQTT meters), plus the triggers.
const replayCapacity = 512

// summarizedTypes are the periodic events (the MQTT meter of each installation reports every 90 s) that are logged
// as an hourly count per pool instead of a line each. Every other event is logged on its own.
var summarizedTypes = map[Type]bool{TypePowerConsumptionUpdate: true}

// Message is an event as sent on the stream: its ID and its JSON envelope.
type Message struct {
	ID   string // "<epoch>-<sequence>": the epoch tells the events of this process from those of earlier ones
	Data []byte
}

// Subscription is what a subscriber receives: the events it missed, then the ones published from now on.
type Subscription struct {
	Replay []Message      // The kept events published after its Last-Event-ID, oldest first
	Events <-chan Message // The events published from now on
	LastID string         // The ID of the newest event published so far (where the subscriber is, after Replay)
}

// Broker fans events out to the subscribers.
type Broker struct {
	log    *slog.Logger
	dryRun bool
	epoch  string           // Unique to this process
	now    func() time.Time // The clock, replaced in tests

	mu          sync.Mutex
	seq         uint64      // Sequence of the newest event published
	kept        []keptEvent // The events of the last replayWindow, oldest first
	subscribers map[chan Message]struct{}
	counts      map[countKey]*eventCount // Summarized events since windowStart
	windowStart time.Time
}

type keptEvent struct {
	Message
	seq       uint64
	header    Header
	at        time.Time
	delivered bool // Sent to a subscriber, live or replayed
}

type countKey struct {
	eventType Type
	poolCode  int
}

type eventCount struct {
	published   int       // Delivered live to every subscriber
	replayed    int       // Delivered late, to a subscriber that reconnected
	dropped     int       // Never delivered: no subscriber connected within the replay window
	slowDrops   int       // Missed by a slow subscriber (one per subscriber and event)
	lastPublish time.Time // Zero when none was published in the window
}

// NewBroker creates a broker. In dry-run mode the events are logged and not delivered.
func NewBroker(log *slog.Logger, dryRun bool) *Broker {
	return &Broker{log: log, dryRun: dryRun, epoch: newEpoch(), now: time.Now,
		subscribers: map[chan Message]struct{}{}, counts: map[countKey]*eventCount{}, windowStart: time.Now()}
}

// newEpoch returns a random identifier for this process, so the event IDs of a restarted service never match.
func newEpoch() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b) // Never fails (crypto/rand crashes the program instead)
	return hex.EncodeToString(b)
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

// Publish sends an event to every subscriber, without blocking: a subscriber whose buffer is full misses it. The
// event is kept for the replay window either way.
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
	now := b.now()
	b.seq++
	msg := Message{ID: b.eventID(b.seq), Data: data}
	b.kept = append(b.kept, keptEvent{Message: msg, seq: b.seq, header: h, at: now, delivered: len(b.subscribers) > 0})
	b.expire(now)

	var count *eventCount
	if summarizedTypes[h.EventType] {
		count = b.countOf(h)
	}
	if len(b.subscribers) == 0 {
		if count == nil {
			b.log.Info("Events: no subscriber connected, event kept for replay", "event_type", h.EventType, "pool_code", h.PoolCode)
		}
		return
	}
	for ch := range b.subscribers {
		select {
		case ch <- msg:
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
		count.lastPublish = now
		return
	}
	b.log.Info("Events: published", "event_type", h.EventType, "pool_code", h.PoolCode, "subscribers", len(b.subscribers))
}

// Subscribe registers a subscriber; call the returned function to unregister it. lastEventID is the ID of the last
// event the subscriber received (its Last-Event-ID header): it gets the kept events published after it, or all of
// them when that ID is from an earlier process. A subscriber without one (a CSMS that just started) gets no replay.
func (b *Broker) Subscribe(lastEventID string) (Subscription, func()) {
	ch := make(chan Message, subscriberBuffer)
	b.mu.Lock()
	b.expire(b.now())
	replay, missed := b.replay(lastEventID)
	b.subscribers[ch] = struct{}{}
	count := len(b.subscribers)
	lastID := b.eventID(b.seq)
	b.mu.Unlock()

	b.log.Info("Events: subscriber connected", "subscribers", count, "last_event_id", lastEventID)
	if len(replay) > 0 || missed > 0 {
		level := slog.LevelInfo
		if missed > 0 {
			level = slog.LevelWarn
		}
		b.log.Log(context.Background(), level, "Events: missed events replayed to a reconnected subscriber",
			"replayed", len(replay), "missed", missed, "last_event_id", lastEventID)
	}

	var once sync.Once
	return Subscription{Replay: replay, Events: ch, LastID: lastID}, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, ch)
			count := len(b.subscribers)
			b.mu.Unlock()
			b.log.Info("Events: subscriber disconnected", "subscribers", count)
		})
	}
}

// replay returns the kept events published after lastEventID, and how many were published after it but are no
// longer kept. The caller holds the lock.
func (b *Broker) replay(lastEventID string) ([]Message, uint64) {
	epoch, seq, ok := parseEventID(lastEventID)
	if !ok {
		return nil, 0
	}
	if epoch != b.epoch {
		seq = 0 // Received from an earlier process: every event of this one is new to it
	}

	oldest := b.seq + 1 // The sequence of the oldest kept event
	if len(b.kept) > 0 {
		oldest = b.kept[0].seq
	}
	var missed uint64
	if seq < b.seq && oldest > seq+1 {
		missed = oldest - seq - 1
	}

	var replay []Message
	for i := range b.kept {
		k := &b.kept[i]
		if k.seq <= seq {
			continue
		}
		replay = append(replay, k.Message)
		if !k.delivered {
			k.delivered = true
			if summarizedTypes[k.header.EventType] {
				b.countOf(k.header).replayed++
			} else {
				b.log.Info("Events: published late", "event_type", k.header.EventType, "pool_code", k.header.PoolCode,
					"delay", b.now().Sub(k.at).Round(time.Millisecond))
			}
		}
	}
	return replay, missed
}

// expire drops the events older than the replay window, and the oldest beyond its capacity, logging those that
// never reached a subscriber. The caller holds the lock.
func (b *Broker) expire(now time.Time) {
	n := 0
	for n < len(b.kept) && (len(b.kept)-n > replayCapacity || now.Sub(b.kept[n].at) > replayWindow) {
		if k := b.kept[n]; !k.delivered {
			if summarizedTypes[k.header.EventType] {
				b.countOf(k.header).dropped++
			} else {
				b.log.Warn("Events: no subscriber connected in time, event dropped", "event_type", k.header.EventType,
					"pool_code", k.header.PoolCode, "published_at", k.at.Format(time.TimeOnly))
			}
		}
		n++
	}
	b.kept = slices.Delete(b.kept, 0, n)
}

// countOf returns the summary count of a summarized event's type and pool. The caller holds the lock.
func (b *Broker) countOf(h Header) *eventCount {
	key := countKey{h.EventType, h.PoolCode}
	count := b.counts[key]
	if count == nil {
		count = &eventCount{}
		b.counts[key] = count
	}
	return count
}

func (b *Broker) eventID(seq uint64) string {
	return b.epoch + "-" + strconv.FormatUint(seq, 10)
}

// parseEventID splits an event ID into its epoch and sequence; ok is false for an empty or foreign ID.
func parseEventID(id string) (epoch string, seq uint64, ok bool) {
	epoch, digits, found := strings.Cut(id, "-")
	if !found || epoch == "" {
		return "", 0, false
	}
	seq, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return epoch, seq, true
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
	b.expire(b.now())
	for key, count := range b.counts {
		args := []any{"event_type", key.eventType, "pool_code", key.poolCode, "since", b.windowStart.Format(time.TimeOnly),
			"published", count.published, "replayed", count.replayed, "dropped", count.dropped,
			"slow_drops", count.slowDrops, "subscribers", len(b.subscribers)}
		if !count.lastPublish.IsZero() {
			args = append(args, "last_published", count.lastPublish.Format(time.TimeOnly))
		}
		level := slog.LevelInfo
		if count.published == 0 || count.dropped > 0 || count.slowDrops > 0 {
			level = slog.LevelWarn
		}
		b.log.Log(context.Background(), level, "Events: summary", args...)

		if count.published+count.replayed+count.dropped == 0 {
			delete(b.counts, key) // Already reported as stopped
		} else {
			*count = eventCount{}
		}
	}
	b.windowStart = time.Now()
}
