package pools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// fakeReader serves a pool's bucket from memory: the latest record of each variable and lists of records.
type fakeReader struct {
	mu     sync.Mutex
	last   map[string]*tagoio.DataRecord
	lists  map[string][]tagoio.DataRecord
	failed bool // Every read fails, as with TagoIO down or an unknown pool
	reads  atomic.Int32
}

func (f *fakeReader) LastValue(_ context.Context, _ int, variable string) (*tagoio.DataRecord, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed {
		return nil, errors.New("TagoIO unavailable")
	}
	return f.last[variable], nil
}

func (f *fakeReader) List(_ context.Context, _ int, variable string, _ int) ([]tagoio.DataRecord, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed {
		return nil, errors.New("TagoIO unavailable")
	}
	return f.lists[variable], nil
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// configuredPool is a bucket as the TagoIO dashboards leave it after the CPO filled in every form. Numbers come
// as json.Number, as the TagoIO client decodes them; a rate typed with a decimal comma stays a string.
func configuredPool() *fakeReader {
	return &fakeReader{
		last: map[string]*tagoio.DataRecord{
			"operator_info": {Metadata: map[string]any{
				"nombre": "ACME Recarga", "CIF": "B12345678", "direccion": "C/ Mayor 1", "telefono": "985000000",
				"correo": "cpo@acme.es", "web": "https://acme.es",
			}},
			"max_installation_power": {Value: json.Number("7400")},
			"load_balancing_mode":    {Value: "Estático"},
			"rate_costs": {Value: json.Number("0"), Metadata: map[string]any{
				"valle": json.Number("0.25"), "llanas": "0,35", "punta": json.Number("0.45"), "IVA": json.Number("21"),
			}},
			"withholding_amount": {Value: json.Number("50.0")},
		},
		lists: map[string][]tagoio.DataRecord{
			"card_id": {
				{Value: "abc123", Metadata: map[string]any{"alias": "Juan", "cps": "SN-A, SN-B", "email": "juan@acme.es"}},
				{Value: "def456"},
				{Value: ""}, // Ignored
			},
		},
	}
}

func TestSetupMatchesTheNeosOcppModel(t *testing.T) {
	// neos-ocpp parses the answer of GET /api/pools/{code} as PoolDeviceSetupResponse (schemas/ocpp_csms.py) and
	// reads: rates and VAT as a fraction, preauth_amount in euros (x100 for its cents), cards with linked_to lists.
	cache := NewCache(configuredPool(), quietLog())
	raw, err := json.Marshal(cache.FetchSetup(context.Background(), 261001, false))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"pool_code": float64(261001), "is_newly_created": false, "load_balancing_mode": "Estático",
		"max_installation_power": float64(7400), "rate_off_peak": 0.25, "rate_flat": 0.35, "rate_peak": 0.45,
		"vat": 0.21, "preauth_amount": float64(50), "cpo_name": "ACME Recarga", "cpo_fiscal_id": "B12345678",
		"cpo_address": "C/ Mayor 1", "cpo_phone": "985000000", "cpo_email": "cpo@acme.es", "cpo_web": "https://acme.es",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %#v; want %#v", key, got[key], value)
		}
	}

	cards, _ := got["cards"].([]any)
	if len(cards) != 2 {
		t.Fatalf("cards = %#v", got["cards"])
	}
	first, second := cards[0].(map[string]any), cards[1].(map[string]any)
	if first["card_id"] != "abc123" || first["card_alias"] != "Juan" || first["email"] != "juan@acme.es" {
		t.Errorf("first card = %#v", first)
	}
	if linked, _ := first["linked_to"].([]any); len(linked) != 2 || linked[0] != "SN-A" || linked[1] != "SN-B" {
		t.Errorf("linked_to = %#v", first["linked_to"])
	}
	// A card without metadata: alias = id, an empty list (pydantic set[str] rejects null) and no email
	if second["card_alias"] != "def456" || second["email"] != nil {
		t.Errorf("second card = %#v", second)
	}
	if linked, ok := second["linked_to"].([]any); !ok || len(linked) != 0 {
		t.Errorf("linked_to of a card without stations = %#v; want []", second["linked_to"])
	}
}

func TestSetupOfANewlyCreatedDeviceHasTheDefaults(t *testing.T) {
	reader := configuredPool()
	setup := NewCache(reader, quietLog()).FetchSetup(context.Background(), 261002, true)

	if reader.reads.Load() != 0 {
		t.Errorf("a new device has no data, but the bucket was read %d times", reader.reads.Load())
	}
	want := Setup{PoolCode: 261002, IsNewlyCreated: true, LoadBalancingMode: "Ninguno", MaxInstallationPower: 5000,
		RateOffPeak: 0.4, RateFlat: 0.4, RatePeak: 0.4, VAT: 0.1, PreauthAmount: 40, Cards: []RFIDCard{}}
	gotJSON, _ := json.Marshal(setup)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("setup = %s\nwant    %s", gotJSON, wantJSON)
	}
}

func TestGetOrLoadRetriesAFailedPoolAtMostOncePerInterval(t *testing.T) {
	// The public station page calls GetOrLoad on every poll: an unreachable TagoIO must not be read each time.
	reader := &fakeReader{failed: true}
	cache := NewCache(reader, quietLog())

	if cache.GetOrLoad(context.Background(), 261001).Loaded {
		t.Fatal("loaded with every read failing")
	}
	readsAfterFirst := reader.reads.Load()
	for range 5 {
		cache.GetOrLoad(context.Background(), 261001)
	}
	if reader.reads.Load() != readsAfterFirst {
		t.Errorf("reads went from %d to %d within the retry interval", readsAfterFirst, reader.reads.Load())
	}
}

func TestVATConversions(t *testing.T) {
	fractions := map[float64]float64{21: 0.21, 10: 0.1, 0.21: 0.21, 0.1: 0.1, 0: 0}
	for in, want := range fractions {
		if got := VATFraction(in); got != want {
			t.Errorf("VATFraction(%v) = %v; want %v", in, got, want)
		}
	}
	percentages := map[float64]float64{0.21: 21, 0.1: 10, 21: 21, 4: 4}
	for in, want := range percentages {
		if got := VATPercentage(in); got != want {
			t.Errorf("VATPercentage(%v) = %v; want %v", in, got, want)
		}
	}
}

func TestSplitList(t *testing.T) {
	tests := map[string][]string{"": {}, "SN-A": {"SN-A"}, " SN-A , ,SN-B,": {"SN-A", "SN-B"}}
	for in, want := range tests {
		got := SplitList(in)
		if len(got) != len(want) {
			t.Errorf("SplitList(%q) = %#v; want %#v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("SplitList(%q) = %#v; want %#v", in, got, want)
			}
		}
	}
}
