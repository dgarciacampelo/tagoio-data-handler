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
	fast, unsubscribeFast := broker.Subscribe()
	slow, unsubscribeSlow := broker.Subscribe()
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
	ch, unsubscribe := broker.Subscribe()
	defer unsubscribe()
	broker.Publish(VPOSStop{Header: NewHeader(TypeVPOSStopTransaction, 1)})
	if len(ch) != 0 {
		t.Fatalf("dry-run broker delivered an event")
	}
}

func TestBrokerSummarizesMeterEvents(t *testing.T) {
	var logs strings.Builder
	broker := NewBroker(slog.New(slog.NewTextHandler(&logs, nil)), false)
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
	ch, unsubscribe := broker.Subscribe()
	defer unsubscribe()
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

	broker.LogSummary()
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "level=WARN") || !strings.Contains(got[0], "pool_code=221006") ||
		!strings.Contains(got[0], "published=3 dropped=1 slow_drops=0") {
		t.Fatalf("first summary = %q; want one WARN line for 221006 with 3 published and 1 dropped", got)
	}

	broker.Publish(PowerUpdate{Header: NewHeader(TypePowerConsumptionUpdate, 221006), MeterWatts: 10})
	<-ch
	broker.LogSummary()
	if got := lines(); len(got) != 1 || !strings.Contains(got[0], "level=INFO") || !strings.Contains(got[0], "published=1 dropped=0") {
		t.Fatalf("second summary = %q; want one INFO line with 1 published", got)
	}

	// The meter stops: one WARN line without events, then nothing.
	broker.LogSummary()
	if got := lines(); len(got) != 1 || !strings.Contains(got[0], "level=WARN") || !strings.Contains(got[0], "published=0 dropped=0") {
		t.Fatalf("summary after the meter stopped = %q; want one WARN line with nothing published", got)
	}
	broker.LogSummary()
	if got := lines(); len(got) != 0 {
		t.Fatalf("later summary = %q; want no line for a pool already reported as stopped", got)
	}
}
