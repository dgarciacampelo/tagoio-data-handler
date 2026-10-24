package devices

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

func newRegistry(t *testing.T, tago *testutil.FakeTagoIO) *Registry {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "devices.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewRegistry(db, tago.Client(), "MASTER-", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPoolCodeFromName(t *testing.T) {
	r := newRegistry(t, testutil.NewFakeTagoIO(t))
	tests := map[string]struct {
		code int
		ok   bool
	}{
		"MASTER-BUSINESS-261001":   {261001, true},
		"MASTER-BUSINESS-261001 ":  {261001, true},
		"MASTER-LITE-220001":       {220001, true},
		"MASTER-BUSINESS-abc":      {0, false},
		"MASTER-BUSINESS-0":        {0, false},
		"MASTER-BUSINESS":          {0, false},
		"PAYMENTS-GATEWAY-1":       {0, false}, // Not a pool device
		"Estación SN-A del 261001": {0, false},
	}
	for name, want := range tests {
		if code, ok := r.PoolCodeFromName(name); code != want.code || ok != want.ok {
			t.Errorf("PoolCodeFromName(%q) = %d, %v; want %d, %v", name, code, ok, want.code, want.ok)
		}
	}
}

func TestByPoolScansOnceForAnUnknownPool(t *testing.T) {
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")
	r := newRegistry(t, tago)
	ctx := context.Background()

	d, err := r.ByPool(ctx, 261001)
	if err != nil || d.DeviceID != "dev-1" || d.Token != "token-1" {
		t.Fatalf("ByPool(261001) = %+v, %v", d, err)
	}
	if _, err := r.ByPool(ctx, 261001); err != nil || tago.Calls("GET /device") != 1 {
		t.Fatalf("a registered pool was looked up again: %v, %d listings", err, tago.Calls("GET /device"))
	}

	for range 3 {
		if _, err := r.ByPool(ctx, 269999); !errors.Is(err, ErrUnknownPool) {
			t.Fatalf("unknown pool: %v", err)
		}
	}
	if got := tago.Calls("GET /device"); got != 2 {
		t.Errorf("an unknown pool rescanned the account %d times; want once while it is remembered", got-1)
	}
}

func TestRescanKeepsTheTokensOfKnownDevices(t *testing.T) {
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")
	r := newRegistry(t, tago)
	if err := r.Register(context.Background(), Device{PoolCode: 261001, DeviceID: "dev-1", Token: "token-1"}); err != nil {
		t.Fatal(err)
	}

	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := tago.Calls("GET /device/token"); got != 0 {
		t.Errorf("rescan fetched %d tokens of an already registered device", got)
	}
}

func TestLoadScansTheAccountWhenTheStoreIsEmpty(t *testing.T) {
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")
	tago.AddDevice("dev-2", "MASTER-BUSINESS-261002", "token-2")
	tago.AddDevice("dev-x", "OTHER-DEVICE", "token-x")
	r := newRegistry(t, tago)

	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(r.PoolCodes()); got != 2 {
		t.Errorf("registered pools = %v", r.PoolCodes())
	}
}

func TestUnknownDeviceIsLookedUpAgainAfterAFailedRescan(t *testing.T) {
	// A device created directly in TagoIO sends its first trigger while TagoIO is failing: the scan fails, but the
	// device is still remembered as "not in the account" for 10 minutes, so all its triggers are dropped.
	tago := testutil.NewFakeTagoIO(t)
	r := newRegistry(t, tago)
	ctx := context.Background()

	tago.Fail("GET /device", true)
	if _, found := r.PoolByDevice(ctx, "dev-new"); found {
		t.Fatal("found a device while the account listing fails")
	}

	tago.Fail("GET /device", false)
	tago.AddDevice("dev-new", "MASTER-BUSINESS-261005", "token-new")
	code, found := r.PoolByDevice(ctx, "dev-new")
	testutil.KnownBug(t, found && code == 261005,
		"a device is remembered as unknown for 10 minutes even when the rescan that missed it failed")
}
