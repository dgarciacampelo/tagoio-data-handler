package bucket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

const poolCode = 261001

type noConnectors struct{}

func (noConnectors) PoolConnectors(int) []Connector { return nil }

// newService returns a bucket service of a fake account with pool 261001, registered with registeredToken.
func newService(t *testing.T, tago *testutil.FakeTagoIO, registeredToken string) *Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "bucket.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	registry := devices.NewRegistry(db, tago.Client(), "MASTER-", log)
	if err := registry.Register(context.Background(), devices.Device{PoolCode: poolCode, DeviceID: "dev-1", Token: registeredToken}); err != nil {
		t.Fatal(err)
	}
	return New(tago.Client(), registry, noConnectors{}, log)
}

func rateCosts(iva string) tagoio.DataInput {
	return tagoio.DataInput{Variable: "rate_costs", Value: 0, Group: "1", Metadata: map[string]any{"IVA": json.Number(iva)}}
}

func TestReplaceLeavesOnlyTheNewValue(t *testing.T) {
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")
	tago.AddRecord("token-1", tagoio.DataRecord{Variable: "rate_costs", Group: "1", Metadata: map[string]any{"IVA": 10}})
	tago.AddRecord("token-1", tagoio.DataRecord{Variable: "rate_costs", Group: "1", Metadata: map[string]any{"IVA": 10}})
	s := newService(t, tago, "token-1")

	if err := s.Replace(context.Background(), poolCode, "rate_costs", "1", rateCosts("21")); err != nil {
		t.Fatal(err)
	}
	records := tago.Records("token-1", "rate_costs")
	if len(records) != 1 || fmt.Sprint(records[0].Metadata["IVA"]) != "21" {
		t.Fatalf("rate_costs after Replace = %+v", records)
	}
}

func TestReplaceKeepsTheOldValueWhenTheInsertFails(t *testing.T) {
	// Replace deletes first and inserts a second later. If the insert fails, the pool loses its rates: after the
	// next restart the CSMS falls back to 0.40 €/kWh and a 40 € preauthorization.
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")
	tago.AddRecord("token-1", tagoio.DataRecord{Variable: "rate_costs", Group: "1", Metadata: map[string]any{"IVA": 10}})
	s := newService(t, tago, "token-1")
	tago.Fail("POST /data", true)

	if err := s.Replace(context.Background(), poolCode, "rate_costs", "1", rateCosts("21")); err == nil {
		t.Fatal("Replace reported success with every insert failing")
	}
	testutil.KnownBug(t, len(tago.Records("token-1", "rate_costs")) == 1,
		"Replace deletes the variable before inserting the new value, so a failed insert leaves the pool without it")
}

func TestInsertAndListThroughThePoolDevice(t *testing.T) {
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")
	s := newService(t, tago, "token-1")
	ctx := context.Background()

	if err := s.Insert(ctx, poolCode, tagoio.DataInput{Variable: "state", Value: "Cargando"}); err != nil {
		t.Fatal(err)
	}
	last, err := s.LastValue(ctx, poolCode, "state")
	if err != nil || last == nil || last.Value != "Cargando" {
		t.Fatalf("LastValue = %+v, %v", last, err)
	}
}

func TestARevokedDeviceTokenIsRefreshed(t *testing.T) {
	// The registry keeps device tokens forever. When a token is revoked or rotated in TagoIO, every read and write
	// of the pool fails with "Authorization denied" until someone fixes the stored token by hand.
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-new")
	s := newService(t, tago, "token-revoked")

	err := s.Insert(context.Background(), poolCode, tagoio.DataInput{Variable: "state", Value: "Disponible"})
	testutil.KnownBug(t, err == nil && len(tago.Records("token-new", "state")) == 1,
		"a revoked or rotated device token is never fetched again: the pool's bucket stays unreachable")
}
