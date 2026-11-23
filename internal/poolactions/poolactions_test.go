package poolactions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

type call struct {
	op, variable, group string
	record              *tagoio.DataInput
}

type fakeBucket struct {
	calls []call
	fail  error
}

func (b *fakeBucket) Delete(_ context.Context, _ int, variable, group string, _ int) (int, error) {
	b.calls = append(b.calls, call{op: "delete", variable: variable, group: group})
	return 1, b.fail
}

func (b *fakeBucket) Replace(_ context.Context, _ int, variable, group string, record tagoio.DataInput) error {
	b.calls = append(b.calls, call{op: "replace", variable: variable, group: group, record: &record})
	return b.fail
}

type fakeConfigs struct{ config pools.Config }

func (c *fakeConfigs) Update(_ int, change func(*pools.Config)) { change(&c.config) }

type fakePublisher struct{ events []events.Event }

func (p *fakePublisher) Publish(e events.Event) { p.events = append(p.events, e) }

const pool = 220001

func newService() (*Service, *fakeBucket, *fakeConfigs, *fakePublisher) {
	b, c, p := &fakeBucket{}, &fakeConfigs{}, &fakePublisher{}
	return New(b, c, p, slog.New(slog.NewTextHandler(io.Discard, nil))), b, c, p
}

func TestSaveCardPublishesAndReplacesTheRecord(t *testing.T) {
	s, b, _, p := newService()
	err := s.SaveCard(context.Background(), pool, Card{ID: " AbC123 ", Alias: "office", LinkedCPs: []string{"ST1", "ST2"}})
	if err != nil {
		t.Fatal(err)
	}
	alias := "OFFICE"
	want := events.RFIDManagement{Header: events.NewHeader(events.TypeManageRFID, pool), CardID: "abc123",
		Action: events.RFIDCreate, LinkedCPs: []string{"ST1", "ST2"}, Alias: &alias}
	if !reflect.DeepEqual(p.events, []events.Event{want}) {
		t.Errorf("events = %+v; want %+v", p.events, want)
	}
	record := tagoio.DataInput{Variable: "card_id", Value: "abc123", Group: "ABC123",
		Metadata: map[string]any{"alias": "OFFICE", "email": "", "cps": "ST1, ST2"}}
	if !reflect.DeepEqual(b.calls, []call{{op: "replace", variable: "card_id", group: "ABC123", record: &record}}) {
		t.Errorf("bucket calls = %+v", b.calls)
	}
}

func TestSaveCardWithoutIDDoesNothing(t *testing.T) {
	s, b, _, p := newService()
	if err := s.SaveCard(context.Background(), pool, Card{ID: "  "}); err == nil {
		t.Fatal("want an error for a card without ID")
	}
	if len(b.calls)+len(p.events) != 0 {
		t.Errorf("calls = %v, events = %v; want none", b.calls, p.events)
	}
}

func TestDeleteCard(t *testing.T) {
	s, b, _, p := newService()
	if err := s.DeleteCard(context.Background(), pool, "abc", nil); err != nil {
		t.Fatal(err)
	}
	want := events.RFIDManagement{Header: events.NewHeader(events.TypeManageRFID, pool), CardID: "abc",
		Action: events.RFIDDelete, LinkedCPs: []string{}}
	if !reflect.DeepEqual(p.events, []events.Event{want}) {
		t.Errorf("events = %+v", p.events)
	}
	if len(b.calls) != 1 || b.calls[0].op != "delete" || b.calls[0].group != "ABC" {
		t.Errorf("bucket calls = %+v; want one delete of group ABC", b.calls)
	}
}

func TestSetRatesTakesTheVATAsFractionOrPercentage(t *testing.T) {
	for _, vat := range []float64{0.21, 21} {
		s, b, c, p := newService()
		if err := s.SetRates(context.Background(), pool, Rates{OffPeak: 0.2, Flat: 0.3, Peak: 0.4, VAT: vat, Withholding: 30}); err != nil {
			t.Fatal(err)
		}
		event := p.events[0].(events.RateList)
		if event.VAT != 21 || event.WithholdingAmount != 30 {
			t.Errorf("VAT %v: event = %+v; want VAT 21 and withholding 30", vat, event)
		}
		if c.config.VAT != 0.21 || c.config.RatePeak != 0.4 || c.config.PreauthAmount != 30 {
			t.Errorf("VAT %v: config = %+v", vat, c.config)
		}
		if len(b.calls) != 2 || b.calls[0].record.Metadata["IVA"] != 0.21 || b.calls[1].variable != "withholding_amount" {
			t.Errorf("VAT %v: bucket calls = %+v", vat, b.calls)
		}
	}
}

func TestSetRatesUpdatesTheCacheEvenWhenTheBucketFails(t *testing.T) {
	s, b, c, _ := newService()
	b.fail = errors.New("TagoIO down")
	if err := s.SetRates(context.Background(), pool, Rates{OffPeak: 0.5, VAT: 10}); err == nil {
		t.Fatal("want the bucket error")
	}
	if c.config.RateOffPeak != 0.5 {
		t.Errorf("config = %+v; want the new rates (the CSMS already has them)", c.config)
	}
}

func TestSetMaxPowerStoresOnlyWhenAsked(t *testing.T) {
	s, b, c, p := newService()
	_ = s.SetMaxPower(context.Background(), pool, 30000, false)
	if len(b.calls) != 0 || c.config.MaxPower != 30000 || len(p.events) != 1 {
		t.Fatalf("without store: calls %v, config %+v, events %v", b.calls, c.config, p.events)
	}
	_ = s.SetMaxPower(context.Background(), pool, 25000, true)
	if len(b.calls) != 1 || b.calls[0].variable != MaxPowerVariable || b.calls[0].record.Value != 25000.0 {
		t.Errorf("with store: calls = %+v", b.calls)
	}
}

func TestSetLoadBalancingMode(t *testing.T) {
	s, b, c, p := newService()
	label, err := s.SetLoadBalancingMode(context.Background(), pool, "dynamicboost")
	if err != nil || label != "Dinámico boost" || c.config.LoadBalancingMode != "Dinámico boost" {
		t.Fatalf("label %q, err %v, config %+v", label, err, c.config)
	}
	if b.calls[0].record.Value != "Dinámico boost" {
		t.Errorf("stored %v", b.calls[0].record.Value)
	}

	_, err = s.SetLoadBalancingMode(context.Background(), pool, "turbo")
	if !errors.Is(err, ErrUnknownMode) {
		t.Errorf("err = %v; want ErrUnknownMode", err)
	}
	if len(p.events) != 2 || len(b.calls) != 1 {
		t.Errorf("an unknown mode is sent to the CSMS but not stored: events %d, calls %d", len(p.events), len(b.calls))
	}
}

func TestSetCPOInfoCleansAndStores(t *testing.T) {
	s, b, c, p := newService()
	info, err := s.SetCPOInfo(context.Background(), pool, CPOInfo{Name: " Velo ", FiscalID: "B-74.454 166"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Velo" || info.FiscalID != "B-74454166" || c.config.CPOFiscalID != "B-74454166" {
		t.Errorf("info %+v, config %+v", info, c.config)
	}
	if p.events[0].(events.CPOInfo).FiscalID != "B-74454166" {
		t.Errorf("event = %+v", p.events[0])
	}
	if len(b.calls) != 1 || b.calls[0].variable != "operator_info" || b.calls[0].record.Metadata["CIF"] != "B-74454166" {
		t.Errorf("bucket calls = %+v", b.calls)
	}
}

func TestResetStationSendsASoftReset(t *testing.T) {
	s, _, _, p := newService()
	s.ResetStation(pool, "ST1")
	want := events.OCPPRequest{Header: events.NewHeader(events.TypeOCPPRequest, pool),
		Payload: events.ResetPayload{Request: events.RequestReset, StationNames: []string{"ST1"}, ResetType: "Soft"}}
	if !reflect.DeepEqual(p.events, []events.Event{want}) {
		t.Errorf("events = %+v", p.events)
	}
}
