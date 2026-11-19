package events

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestEventsMatchPythonContract compares each event with the JSON the Python service's Pydantic models produced
// for the same values (testdata/python_events.jsonl, generated from src/schemas/analysis.py). The comparison is
// on the decoded JSON: key order, spacing and 40 vs 40.0 do not matter to the consumer.
func TestEventsMatchPythonContract(t *testing.T) {
	alias, email := "ABC123", "a@b.es"
	header := func(t Type) Header { return NewHeader(t, 261001) }
	goEvents := []Event{
		ChangeAvailability{Header: header(TypeChangeAvailability), StationName: "22HBS0442HBS23"},
		MaxGridPower{Header: header(TypeChangeMaxGridPower), MaxPowerWatts: 7400},
		RFIDManagement{Header: header(TypeManageRFID), CardID: "abc123", Action: RFIDCreate, LinkedCPs: []string{"ST1", "ST2"}, Alias: &alias, Email: &email},
		RFIDManagement{Header: header(TypeManageRFID), CardID: "abc123", Action: RFIDDelete, LinkedCPs: []string{}},
		CPOInfo{Header: header(TypeChangeCPOInfo), Name: "ACME", FiscalID: "B12345678", Address: "C/ Mayor 1", Phone: "985000000", Email: "info@acme.es"},
		RateList{Header: header(TypeChangeRateList), RateOffPeak: 0.15, RateFlat: 0.25, RatePeak: 0.35, VAT: 21, WithholdingAmount: 40},
		LoadBalancing{Header: header(TypeChangeLoadBalancingMode), SelectedMode: "Ninguno"},
		PowerUpdate{Header: header(TypePowerConsumptionUpdate), MeterWatts: 1234.5},
		OCPPRequest{Header: header(TypeOCPPRequest), Payload: ChangeAvailabilityPayload{Request: RequestChangeAvailability, StationNames: []string{"ST1"}, ConnectorID: 1, AvailabilityType: "Operative"}},
		OCPPRequest{Header: header(TypeOCPPRequest), Payload: RemoteStopPayload{Request: RequestRemoteStopTransaction, TransactionID: 99}},
		VPOSStart{Header: header(TypeVPOSStartTransaction), StationName: "ST1", ConnectorID: 1, Email: "d@x.es", Amount: 40, Currency: "EUR"},
		VPOSStop{Header: header(TypeVPOSStopTransaction), StationName: "ST1", ConnectorID: 1},
		PayterTerminalManagement{Header: header(TypeManagePayterTerminal), SerialNumber: "P6X20222101349", Action: PayterTerminalCreate, LinkedStations: []string{"ST1", "ST2"}},
		PayterTerminalManagement{Header: header(TypeManagePayterTerminal), SerialNumber: "P6X20231300653", Action: PayterTerminalDelete, IsPoolShared: true, LinkedStations: []string{}},
	}

	file, err := os.Open("testdata/python_events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for i := 0; scanner.Scan(); i++ {
		if i >= len(goEvents) {
			t.Fatalf("more reference lines than Go events")
		}
		got, err := Marshal(goEvents[i])
		if err != nil {
			t.Fatalf("event %d: Marshal: %v", i, err)
		}
		if strings.Contains(string(got), "\n") {
			t.Errorf("event %d: encoded on several lines", i)
		}
		var gotValue, wantValue any
		_ = json.Unmarshal(got, &gotValue)
		_ = json.Unmarshal(scanner.Bytes(), &wantValue)
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("event %d differs from the Python contract:\n got  %s\n want %s", i, got, scanner.Bytes())
		}
	}
}

func TestBrokerFanOutAndSlowSubscriber(t *testing.T) {
	broker := NewBroker(slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	fastSub, unsubscribeFast := broker.Subscribe("")
	slowSub, unsubscribeSlow := broker.Subscribe("")
	fast, slow := fastSub.Events, slowSub.Events
	defer unsubscribeFast()
	defer unsubscribeSlow()

	event := PowerUpdate{Header: NewHeader(TypePowerConsumptionUpdate, 1), MeterWatts: 10}
	for i := 0; i < subscriberBuffer+5; i++ {
		broker.Publish(event)
		if i < subscriberBuffer {
			<-fast // The fast subscriber keeps up
		}
	}
	if len(slow) != subscriberBuffer {
		t.Fatalf("slow subscriber buffered %d events; want %d (the rest dropped, without blocking)", len(slow), subscriberBuffer)
	}

	unsubscribeSlow()
	unsubscribeSlow() // Idempotent
	broker.Publish(event)
	if len(slow) != subscriberBuffer {
		t.Fatalf("unsubscribed channel still receives events")
	}
}

func TestBrokerDryRunDeliversNothing(t *testing.T) {
	broker := NewBroker(slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	sub, unsubscribe := broker.Subscribe("")
	defer unsubscribe()
	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 1)})
	if len(sub.Events) != 0 {
		t.Fatalf("dry-run broker delivered an event")
	}
}

func TestBrokerSummarizesMeterEvents(t *testing.T) {
	var logs strings.Builder
	broker := NewBroker(slog.New(slog.NewTextHandler(&logs, nil)), false)
	clock := time.Now()
	broker.now = func() time.Time { return clock }
	lines := func() []string {
		defer logs.Reset()
		var out []string
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			if line != "" {
				out = append(out, line)
			}
		}
		return out
	}

	broker.Publish(PowerUpdate{Header: NewHeader(TypePowerConsumptionUpdate, 221006), MeterWatts: 10}) // No subscriber yet
	// A new subscriber: no replay.
	sub, unsubscribe := broker.Subscribe("")
	defer unsubscribe()
	ch := sub.Events
	lines()
	for range 3 {
		broker.Publish(PowerUpdate{Header: NewHeader(TypePowerConsumptionUpdate, 221006), MeterWatts: 10})
		<-ch
	}
	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 221013)})
	<-ch
	if got := lines(); len(got) != 1 || !strings.Contains(got[0], "event_type=virtual_POS_stop_transaction") {
		t.Fatalf("logs while publishing = %q; want only the line of the virtual POS event", got)
	}

	clock = clock.Add(replayWindow + time.Second) // The event published before the subscriber expires undelivered
	broker.LogSummary()
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "level=WARN") || !strings.Contains(got[0], "pool_code=221006") ||
		!strings.Contains(got[0], "published=3 replayed=0 dropped=1 slow_drops=0") {
		t.Fatalf("first summary = %q; want one WARN line for 221006 with 3 published and 1 dropped", got)
	}

	broker.Publish(PowerUpdate{Header: NewHeader(TypePowerConsumptionUpdate, 221006), MeterWatts: 10})
	<-ch
	broker.LogSummary()
	if got := lines(); len(got) != 1 || !strings.Contains(got[0], "level=INFO") || !strings.Contains(got[0], "published=1 replayed=0 dropped=0") {
		t.Fatalf("second summary = %q; want one INFO line with 1 published", got)
	}

	// The meter stops: one WARN line without events, then nothing.
	broker.LogSummary()
	if got := lines(); len(got) != 1 || !strings.Contains(got[0], "level=WARN") || !strings.Contains(got[0], "published=0 replayed=0 dropped=0") {
		t.Fatalf("summary after the meter stopped = %q; want one WARN line with nothing published", got)
	}
	broker.LogSummary()
	if got := lines(); len(got) != 0 {
		t.Fatalf("later summary = %q; want no line for a pool already reported as stopped", got)
	}
}

// region Replay

func TestBrokerReplaysTheEventsAReconnectedSubscriberMissed(t *testing.T) {
	broker := NewBroker(slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	first, unsubscribe := broker.Subscribe("")
	if len(first.Replay) != 0 || first.LastID != broker.epoch+"-0" {
		t.Fatalf("first subscription = %+v; want no replay and the ID of no event yet", first)
	}
	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 1)})
	received := <-first.Events
	unsubscribe() // The stream drops

	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 2)})
	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 3)})
	again, unsubscribeAgain := broker.Subscribe(received.ID)
	defer unsubscribeAgain()
	if got := poolsOf(t, again.Replay); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("replayed pools = %v; want the 2 events published while disconnected", got)
	}
	if again.LastID != again.Replay[1].ID {
		t.Fatalf("LastID = %q; want the newest replayed event %q", again.LastID, again.Replay[1].ID)
	}

	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 4)})
	if got := poolsOf(t, []Message{<-again.Events}); !reflect.DeepEqual(got, []int{4}) || len(again.Events) != 0 {
		t.Fatalf("live events after the replay = %v; want only the new one", got)
	}

	upToDate, unsubscribeUpToDate := broker.Subscribe(broker.eventID(4))
	defer unsubscribeUpToDate()
	if len(upToDate.Replay) != 0 {
		t.Fatalf("a subscriber with the newest ID got a replay of %d events", len(upToDate.Replay))
	}
}

func TestBrokerReplaysAllItsEventsToASubscriberOfAnEarlierProcess(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	before := NewBroker(log, false)
	sub, _ := before.Subscribe("")
	before.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 1)})
	idBeforeRestart := (<-sub.Events).ID

	broker := NewBroker(log, false) // The service restarted
	broker.Publish(VPOSStart{Header: NewHeader(TypeVPOSStartTransaction, 2), StationName: "ST1", ConnectorID: 1})
	other, unsubscribeOther := broker.Subscribe(idBeforeRestart) // The other CSMS reconnects first
	defer unsubscribeOther()
	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 3)})
	<-other.Events

	late, unsubscribeLate := broker.Subscribe(idBeforeRestart)
	defer unsubscribeLate()
	if got := poolsOf(t, late.Replay); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("replayed pools = %v; want every event of the restarted service", got)
	}
	if got := poolsOf(t, other.Replay); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("replayed pools to the first CSMS = %v; want the event published before it reconnected", got)
	}

	started, unsubscribeStarted := broker.Subscribe("") // A CSMS that just started
	defer unsubscribeStarted()
	if len(started.Replay) != 0 {
		t.Fatalf("a subscriber without Last-Event-ID got a replay of %d events", len(started.Replay))
	}
	for _, foreign := range []string{"garbage", "-3", "abc-x"} {
		if replay, _ := broker.replay(foreign); len(replay) != 0 {
			t.Errorf("Last-Event-ID %q replayed %d events; want none", foreign, len(replay))
		}
	}
}

func TestBrokerDropsTheEventsNoSubscriberGotInTime(t *testing.T) {
	var logs strings.Builder
	broker := NewBroker(slog.New(slog.NewTextHandler(&logs, nil)), false)
	clock := time.Now()
	broker.now = func() time.Time { return clock }

	broker.Publish(VPOSStart{Header: NewHeader(TypeVPOSStartTransaction, 261001), StationName: "ST1", ConnectorID: 1})
	clock = clock.Add(replayWindow + time.Second)
	sub, unsubscribe := broker.Subscribe("0badc0de-9") // Reconnects from an earlier process, too late
	defer unsubscribe()

	if len(sub.Replay) != 0 {
		t.Fatalf("replayed %d events older than the replay window", len(sub.Replay))
	}
	got := logs.String()
	if !strings.Contains(got, `level=WARN msg="Events: no subscriber connected in time, event dropped" event_type=virtual_POS_start_transaction pool_code=261001`) {
		t.Errorf("logs = %s; want the dropped event", got)
	}
	if !strings.Contains(got, "replayed=0 missed=1") {
		t.Errorf("logs = %s; want the missed event counted", got)
	}
}

func TestBrokerLogsAnEventPublishedLate(t *testing.T) {
	var logs strings.Builder
	broker := NewBroker(slog.New(slog.NewTextHandler(&logs, nil)), false)
	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 261001)})
	sub, unsubscribe := broker.Subscribe("0badc0de-9")
	defer unsubscribe()

	if len(sub.Replay) != 1 || !strings.Contains(logs.String(), `msg="Events: published late" event_type=virtual_POS_stop_transaction`) {
		t.Fatalf("replay = %d events, logs = %s; want the event published late", len(sub.Replay), logs.String())
	}
}

func TestBrokerKeepsAtMostItsCapacity(t *testing.T) {
	var logs strings.Builder
	broker := NewBroker(slog.New(slog.NewTextHandler(&logs, nil)), false)
	for i := range replayCapacity + 10 {
		broker.Publish(PowerUpdate{Header: NewHeader(TypePowerConsumptionUpdate, i), MeterWatts: 10})
	}
	sub, unsubscribe := broker.Subscribe("0badc0de-1")
	defer unsubscribe()

	if len(sub.Replay) != replayCapacity || sub.Replay[0].ID != broker.eventID(11) {
		t.Fatalf("replayed %d events from %s; want the newest %d", len(sub.Replay), sub.Replay[0].ID, replayCapacity)
	}
	if !strings.Contains(logs.String(), "replayed=512 missed=10") {
		t.Errorf("logs = %s; want 10 missed events", logs.String())
	}
}

// poolsOf returns the pool code of each message, in order.
func poolsOf(t *testing.T, messages []Message) []int {
	t.Helper()
	var pools []int
	for _, m := range messages {
		var envelope struct {
			Data struct {
				PoolCode int `json:"pool_code"`
			} `json:"data"`
		}
		if err := json.Unmarshal(m.Data, &envelope); err != nil {
			t.Fatalf("message %s: %v", m.Data, err)
		}
		pools = append(pools, envelope.Data.PoolCode)
	}
	return pools
}

// endregion
