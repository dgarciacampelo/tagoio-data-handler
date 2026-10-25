package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio/realtime"
)

const (
	testDevice = "637f4de7e7223300208aa890"
	testPool   = 221006
)

// region Fakes

type fakeResolver struct{}

func (fakeResolver) PoolByDevice(_ context.Context, deviceID string) (int, bool) {
	return testPool, deviceID == testDevice
}

type bucketCall struct {
	Op       string
	Variable string
	Group    string
	Record   *tagoio.DataInput
}

type fakeBucket struct {
	mu       sync.Mutex
	calls    []bucketCall
	feedback []string
	fail     bool
}

func (b *fakeBucket) Insert(_ context.Context, _ int, records ...tagoio.DataInput) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range records {
		b.calls = append(b.calls, bucketCall{Op: "insert", Variable: records[i].Variable, Record: &records[i]})
	}
	return nil
}

func (b *fakeBucket) Delete(_ context.Context, _ int, variable, group string, _ int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, bucketCall{Op: "delete", Variable: variable, Group: group})
	return 1, nil
}

func (b *fakeBucket) Replace(_ context.Context, _ int, variable, group string, record tagoio.DataInput) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, bucketCall{Op: "replace", Variable: variable, Group: group, Record: &record})
	if b.fail {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (b *fakeBucket) Feedback(_ int, variable string, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := "OK"
	if !ok {
		result = "ERROR"
	}
	b.feedback = append(b.feedback, variable+"="+result)
}

type fakeConfigs struct {
	config pools.Config
}

func (c *fakeConfigs) Update(_ int, change func(*pools.Config)) { change(&c.config) }

type fakePublisher struct {
	events []events.Event
}

func (p *fakePublisher) Publish(e events.Event) { p.events = append(p.events, e) }

type harness struct {
	service   *Service
	bucket    *fakeBucket
	configs   *fakeConfigs
	publisher *fakePublisher
}

func newHarness() *harness {
	h := &harness{bucket: &fakeBucket{}, configs: &fakeConfigs{config: pools.Defaults()}, publisher: &fakePublisher{}}
	h.service = New(fakeResolver{}, h.bucket, h.configs, h.publisher, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	return h
}

func (h *harness) trigger(t *testing.T, analysis config.Analysis, records []tagoio.DataRecord) {
	t.Helper()
	handler := h.service.Handler(analysis)
	if handler == nil {
		t.Fatalf("no handler for %s", analysis)
	}
	handler(context.Background(), realtime.Trigger{Data: records})
}

// rec builds a scope record of the test device.
func rec(variable string, value any) tagoio.DataRecord {
	return tagoio.DataRecord{Device: testDevice, Variable: variable, Value: value}
}

// assertEvents compares the published events with the expected ones, as JSON.
func assertEvents(t *testing.T, got []events.Event, want ...events.Event) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("published %d events; want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		gotJSON, _ := events.Marshal(got[i])
		wantJSON, _ := events.Marshal(want[i])
		if !bytes.Equal(gotJSON, wantJSON) {
			t.Errorf("event %d:\n got  %s\n want %s", i, gotJSON, wantJSON)
		}
	}
}

// endregion

func TestOCPPRequestFromRealScope(t *testing.T) {
	raw, err := os.ReadFile("testdata/ocpp_status_notification.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []tagoio.DataRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber() // As the realtime client decodes
	if err := decoder.Decode(&records); err != nil {
		t.Fatal(err)
	}

	h := newHarness()
	h.trigger(t, config.AnalysisOCPPRequests, records)
	assertEvents(t, h.publisher.events, events.OCPPRequest{
		Header:  events.NewHeader(events.TypeOCPPRequest, testPool),
		Payload: events.StatusNotificationPayload{Request: "status_notification", StationNames: []string{"22HBS0442HBS23"}, ConnectorID: 1},
	})
}

func TestOCPPRequestParsesByVariableNameInAnyOrder(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisOCPPRequests, []tagoio.DataRecord{
		rec("params_connector", json.Number("2")),
		rec("ocpp_request", "unlock_connector"),
		rec("ocpp_cp", "ST1, ST2,"),
	})
	assertEvents(t, h.publisher.events, events.OCPPRequest{
		Header:  events.NewHeader(events.TypeOCPPRequest, testPool),
		Payload: events.UnlockConnectorPayload{Request: "unlock_connector", StationNames: []string{"ST1", "ST2"}, ConnectorID: 2},
	})
}

func TestOCPPRequestVariants(t *testing.T) {
	tests := map[string]struct {
		records []tagoio.DataRecord
		want    any // Payload, or nil when nothing must be published
	}{
		"change availability to available": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "change_availability"), rec("params_connector", json.Number("1")), rec("x", "Available")},
			want:    events.ChangeAvailabilityPayload{Request: "change_availability", StationNames: []string{"ST1"}, ConnectorID: 1, AvailabilityType: "Operative"},
		},
		"change availability to anything else": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "change_availability"), rec("params_connector", json.Number("0")), rec("x", "unavailable")},
			want:    events.ChangeAvailabilityPayload{Request: "change_availability", StationNames: []string{"ST1"}, ConnectorID: 0, AvailabilityType: "Inoperative"},
		},
		"reset is capitalized": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "reset"), rec("x", "HARD")},
			want:    events.ResetPayload{Request: "reset", StationNames: []string{"ST1"}, ResetType: "Hard"},
		},
		"invalid reset type": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "reset"), rec("x", "warm")},
		},
		"remote start": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "remote_start_transaction"), rec("params_connector", "1"), rec("x", "TAG01")},
			want:    events.RemoteStartPayload{Request: "remote_start_transaction", StationNames: []string{"ST1"}, ConnectorID: 1, IDTag: "TAG01"},
		},
		"remote stop": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "remote_stop_transaction"), rec("x", json.Number("4521"))},
			want:    events.RemoteStopPayload{Request: "remote_stop_transaction", TransactionID: 4521},
		},
		"unsupported request": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "data_transfer")},
		},
		"connector with decimals": {
			records: []tagoio.DataRecord{rec("ocpp_cp", "ST1"), rec("ocpp_request", "status_notification"), rec("params_connector", json.Number("1.5"))},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			h.trigger(t, config.AnalysisOCPPRequests, tt.records)
			if tt.want == nil {
				if len(h.publisher.events) != 0 {
					t.Fatalf("published %+v; want nothing", h.publisher.events)
				}
				return
			}
			assertEvents(t, h.publisher.events, events.OCPPRequest{Header: events.NewHeader(events.TypeOCPPRequest, testPool), Payload: tt.want})
		})
	}
}

func TestManageRFIDCreate(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisManageRFID, []tagoio.DataRecord{
		rec("a", "AbC123"), rec("b", "office"), {Device: testDevice, Variable: "c", Value: nil}, rec("d", "ST1, ST2"), rec("e", "create"),
	})

	alias := "OFFICE"
	assertEvents(t, h.publisher.events, events.RFIDManagement{
		Header: events.NewHeader(events.TypeManageRFID, testPool),
		CardID: "abc123", Action: "create", LinkedCPs: []string{"ST1", "ST2"}, Alias: &alias, Email: nil,
	})
	want := []bucketCall{{Op: "replace", Variable: "card_id", Group: "ABC123", Record: &tagoio.DataInput{
		Variable: "card_id", Value: "abc123", Group: "ABC123",
		Metadata: map[string]any{"alias": "OFFICE", "email": "", "cps": "ST1, ST2"},
	}}}
	if !reflect.DeepEqual(h.bucket.calls, want) {
		t.Errorf("bucket calls = %+v; want %+v", h.bucket.calls, want)
	}
	if !reflect.DeepEqual(h.bucket.feedback, []string{"validation_rfid=OK"}) {
		t.Errorf("feedback = %v", h.bucket.feedback)
	}
}

func TestManageRFIDCreateWithoutAliasUsesTheCard(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisManageRFID, []tagoio.DataRecord{
		rec("a", "abc"), rec("b", ""), rec("c", "x@y.es"), rec("d", ""), rec("e", "create"),
	})
	alias, email := "ABC", "x@y.es"
	assertEvents(t, h.publisher.events, events.RFIDManagement{
		Header: events.NewHeader(events.TypeManageRFID, testPool),
		CardID: "abc", Action: "create", LinkedCPs: []string{}, Alias: &alias, Email: &email,
	})
}

func TestManageRFIDDelete(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisManageRFID, []tagoio.DataRecord{
		{Device: testDevice, Variable: "card_id", Value: "abc123", Metadata: map[string]any{"cps": "ST1,ST3"}},
	})
	assertEvents(t, h.publisher.events, events.RFIDManagement{
		Header: events.NewHeader(events.TypeManageRFID, testPool),
		CardID: "abc123", Action: "delete", LinkedCPs: []string{"ST1", "ST3"},
	})
	if len(h.bucket.calls) != 1 || h.bucket.calls[0].Op != "delete" || h.bucket.calls[0].Group != "ABC123" {
		t.Errorf("bucket calls = %+v; want one delete of group ABC123", h.bucket.calls)
	}
}

func TestManageRFIDIncompleteScopeReportsError(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisManageRFID, []tagoio.DataRecord{rec("a", "abc"), rec("b", "alias")})
	if len(h.publisher.events) != 0 {
		t.Fatalf("published %+v; want nothing", h.publisher.events)
	}
	if !reflect.DeepEqual(h.bucket.feedback, []string{"validation_rfid=ERROR"}) {
		t.Errorf("feedback = %v; want an ERROR toast", h.bucket.feedback)
	}
}

func TestChangeRateList(t *testing.T) {
	tests := map[string]struct {
		vat, withholding any
		wantVATPercent   float64
		wantVATFraction  float64
		wantWithholding  float64
	}{
		"VAT as a percentage": {json.Number("21"), json.Number("50"), 21, 0.21, 50},
		"VAT as a fraction":   {json.Number("0.1"), "30,5", 10, 0.1, 30.5},
		"missing withholding": {json.Number("10"), nil, 10, 0.1, defaultWithholding},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			records := []tagoio.DataRecord{rec("a", "0,15"), rec("b", json.Number("0.25")), rec("c", "0.35"), rec("d", tt.vat)}
			if tt.withholding != nil {
				records = append(records, rec("e", tt.withholding))
			}
			h.trigger(t, config.AnalysisChangeRateList, records)

			assertEvents(t, h.publisher.events, events.RateList{
				Header:      events.NewHeader(events.TypeChangeRateList, testPool),
				RateOffPeak: 0.15, RateFlat: 0.25, RatePeak: 0.35, VAT: tt.wantVATPercent, WithholdingAmount: tt.wantWithholding,
			})
			if h.configs.config.VAT != tt.wantVATFraction || h.configs.config.PreauthAmount != tt.wantWithholding {
				t.Errorf("config VAT %v, preauth %v; want %v, %v", h.configs.config.VAT, h.configs.config.PreauthAmount, tt.wantVATFraction, tt.wantWithholding)
			}
			if len(h.bucket.calls) != 2 || h.bucket.calls[0].Record.Metadata["IVA"] != tt.wantVATFraction {
				t.Errorf("bucket calls = %+v; want rate_costs (IVA as a fraction) and withholding_amount replaced", h.bucket.calls)
			}
		})
	}
}

func TestChangeRateListInvalidRateReportsError(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisChangeRateList, []tagoio.DataRecord{rec("a", "cheap"), rec("b", "0.2"), rec("c", "0.3"), rec("d", "21")})
	if len(h.publisher.events) != 0 || len(h.bucket.calls) != 0 {
		t.Fatalf("an invalid rate must change nothing: events %+v, calls %+v", h.publisher.events, h.bucket.calls)
	}
	if !reflect.DeepEqual(h.bucket.feedback, []string{"validation_rate=ERROR"}) {
		t.Errorf("feedback = %v", h.bucket.feedback)
	}
}

func TestChangeRateListFailedWriteReportsError(t *testing.T) {
	h := newHarness()
	h.bucket.fail = true
	h.trigger(t, config.AnalysisChangeRateList, []tagoio.DataRecord{rec("a", "0.1"), rec("b", "0.2"), rec("c", "0.3"), rec("d", "21")})
	if !reflect.DeepEqual(h.bucket.feedback, []string{"validation_rate=ERROR"}) {
		t.Errorf("feedback = %v; want ERROR when the bucket write fails", h.bucket.feedback)
	}
}

func TestChangeCPOInfo(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisChangeCPOInfo, []tagoio.DataRecord{
		rec("a", "ACME S.L."), rec("b", "B 12.345.678"), rec("c", "C/ Mayor 1"), rec("d", json.Number("985000000")), rec("e", "info@acme.es"), {Device: testDevice, Value: nil},
	})
	assertEvents(t, h.publisher.events, events.CPOInfo{
		Header: events.NewHeader(events.TypeChangeCPOInfo, testPool),
		Name:   "ACME S.L.", FiscalID: "B12345678", Address: "C/ Mayor 1", Phone: "985000000", Email: "info@acme.es", Web: "",
	})
	if h.configs.config.CPOName != "ACME S.L." || h.configs.config.CPOPhone != "985000000" {
		t.Errorf("config not updated in place: %+v", h.configs.config)
	}
}

func TestSimpleAnalyses(t *testing.T) {
	h := newHarness()
	h.trigger(t, config.AnalysisChangeAvailability, []tagoio.DataRecord{rec("station", "22HBS0442HBS23")})
	h.trigger(t, config.AnalysisChangeMaxGridPower, []tagoio.DataRecord{rec("power", json.Number("7400"))})
	h.trigger(t, config.AnalysisChangeLoadBalancingMode, []tagoio.DataRecord{rec("mode", "Dinámico")})
	h.trigger(t, config.AnalysisChangeAvailability, []tagoio.DataRecord{{Device: "unknown", Value: "X"}}) // Unknown pool: ignored

	assertEvents(t, h.publisher.events,
		events.ChangeAvailability{Header: events.NewHeader(events.TypeChangeAvailability, testPool), StationName: "22HBS0442HBS23"},
		events.MaxGridPower{Header: events.NewHeader(events.TypeChangeMaxGridPower, testPool), MaxPowerWatts: 7400},
		events.LoadBalancing{Header: events.NewHeader(events.TypeChangeLoadBalancingMode, testPool), SelectedMode: "Dinámico"},
	)
	if h.configs.config.MaxPower != 7400 || h.configs.config.LoadBalancingMode != "Dinámico" {
		t.Errorf("config not updated in place: %+v", h.configs.config)
	}
}

func TestPowerConsumptionShapes(t *testing.T) {
	tests := map[string]struct {
		record    tagoio.DataRecord
		wantPool  int
		wantWatts float64
	}{
		"parsed MQTT params": {
			record:   tagoio.DataRecord{Device: testDevice, Params: map[string]any{"P": json.Number("1500.5")}},
			wantPool: testPool, wantWatts: 1500.5,
		},
		"raw MQTT payload string, pool from the topic": {
			record:   tagoio.DataRecord{Topic: "Bivocom/MASTER-BUSINESS-261003", Payload: json.RawMessage(`"{\"params\":{\"P\":2200}}"`)},
			wantPool: 261003, wantWatts: 2200,
		},
		"plain variable, pool from the metadata topic": {
			record:   tagoio.DataRecord{Value: json.Number("300"), Metadata: map[string]any{"mqtt_topic": "x/MASTER-BUSINESS-261004"}},
			wantPool: 261004, wantWatts: 300,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			h.trigger(t, config.AnalysisPowerConsumptionUpdate, []tagoio.DataRecord{tt.record})
			assertEvents(t, h.publisher.events, events.PowerUpdate{
				Header: events.NewHeader(events.TypePowerConsumptionUpdate, tt.wantPool), MeterWatts: tt.wantWatts,
			})
		})
	}
}

// capturedTrigger decodes an SSE event captured from production, as the realtime worker does.
func capturedTrigger(t *testing.T, name string) realtime.Trigger {
	t.Helper()
	raw, err := os.ReadFile("../tagoio/realtime/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	trigger, err := realtime.DecodeEvent(bytes.TrimSpace(raw))
	if err != nil {
		t.Fatal(err)
	}
	return trigger
}

func TestCapturedChangeAvailability(t *testing.T) {
	trigger := capturedTrigger(t, "sse_change_availability.json")
	for i := range trigger.Data {
		trigger.Data[i].Device = testDevice // The capture's device ID is redacted
	}

	h := newHarness()
	h.trigger(t, config.AnalysisChangeAvailability, trigger.Data)
	assertEvents(t, h.publisher.events, events.ChangeAvailability{
		Header: events.NewHeader(events.TypeChangeAvailability, testPool), StationName: "22HBS0452HBS22",
	})
}

func TestCapturedPowerConsumptionUpdate(t *testing.T) {
	// The MQTT integration forwards the meter's parsed message without a device: the pool comes from the topic.
	trigger := capturedTrigger(t, "sse_power_consumption_update.json")

	h := newHarness()
	h.trigger(t, config.AnalysisPowerConsumptionUpdate, trigger.Data)
	assertEvents(t, h.publisher.events, events.PowerUpdate{
		Header: events.NewHeader(events.TypePowerConsumptionUpdate, 221006), MeterWatts: 29.88,
	})
}
