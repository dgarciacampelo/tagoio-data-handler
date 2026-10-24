package provisioning

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

// recordingBucket records the default variables written for a new pool.
type recordingBucket struct {
	mu      sync.Mutex
	records []tagoio.DataInput
}

func (b *recordingBucket) Insert(_ context.Context, _ int, records ...tagoio.DataInput) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, records...)
	return nil
}

func (b *recordingBucket) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records)
}

type fixture struct {
	service  *Service
	registry *devices.Registry
	tago     *testutil.FakeTagoIO
	bucket   *recordingBucket
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "provisioning.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tago := testutil.NewFakeTagoIO(t)
	registry := devices.NewRegistry(db, tago.Client(), "MASTER-", log)
	madrid, _ := time.LoadLocation("Europe/Madrid")
	bucket := &recordingBucket{}
	return fixture{service: New(tago.Client(), registry, db, bucket, log, madrid), registry: registry, tago: tago, bucket: bucket}
}

// code returns a pool code of the current year, e.g. code(1003) = 261003 in 2026.
func code(sequence int) int {
	madrid, _ := time.LoadLocation("Europe/Madrid")
	c, _ := strconv.Atoi(time.Now().In(madrid).Format("06") + strconv.Itoa(sequence))
	return c
}

// region Pool codes

func TestNextPoolCodeStartsEachYearAt1001(t *testing.T) {
	f := newFixture(t)
	next, err := f.service.nextPoolCode(context.Background())
	if err != nil || next != code(1001) {
		t.Fatalf("nextPoolCode = %d, %v; want %d", next, err, code(1001))
	}
}

func TestNextPoolCodeFollowsTheHighestCodeOfTheYear(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.registry.Register(ctx, devices.Device{PoolCode: code(1003), DeviceID: "stored", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	f.tago.AddDevice("d7", "MASTER-BUSINESS-"+strconv.Itoa(code(1007)), "t7") // Only in the account
	f.tago.AddDevice("d4", "MASTER-BUSINESS-"+strconv.Itoa(code(1004)), "t4")
	f.tago.AddDevice("old", "MASTER-BUSINESS-201050", "t-old") // Another year

	next, err := f.service.nextPoolCode(ctx)
	if err != nil || next != code(1008) {
		t.Fatalf("nextPoolCode = %d, %v; want %d", next, err, code(1008))
	}
}

// endregion
// region Pool devices

func TestEnsureDeviceRegistersAnExistingDevice(t *testing.T) {
	f := newFixture(t)
	f.tago.AddDevice("dev-1", "MASTER-BUSINESS-261001", "token-1")

	d, created, err := f.service.EnsureDevice(context.Background(), 261001)
	if err != nil || created || d.DeviceID != "dev-1" || d.Token != "token-1" {
		t.Fatalf("EnsureDevice = %+v, created %v, %v", d, created, err)
	}
	if registered, err := f.registry.ByPool(context.Background(), 261001); err != nil || registered.DeviceID != "dev-1" {
		t.Errorf("registry = %+v, %v", registered, err)
	}
	if f.tago.DeviceCount() != 1 {
		t.Errorf("devices in the account = %d", f.tago.DeviceCount())
	}
}

func TestEnsureDeviceCreatesAMissingDevice(t *testing.T) {
	f := newFixture(t)

	d, created, err := f.service.EnsureDevice(context.Background(), 261001)
	if err != nil || !created || d.DeviceID == "" || d.Token == "" {
		t.Fatalf("EnsureDevice = %+v, created %v, %v", d, created, err)
	}
	devicesInAccount, _ := f.tago.Client().ListDevices(context.Background(), "MASTER-BUSINESS-261001")
	if len(devicesInAccount) != 1 {
		t.Fatalf("devices named MASTER-BUSINESS-261001: %d", len(devicesInAccount))
	}
	if device := devicesInAccount[0]; device.TagValue("plan") != "ADVANCED" || device.TagValue("group_id") != "261001" ||
		device.TagValue("type") != "MASTER" {
		t.Errorf("tags = %+v", device.Tags)
	}
}

func TestConcurrentFirstSetupsCreateOneDevice(t *testing.T) {
	// Two CSMS instances (or two pool builds) ask for the setup of a pool without a device at the same time.
	f := newFixture(t)
	var arrived atomic.Int32
	bothListed := make(chan struct{})
	f.tago.BeforeDeviceList = func() {
		if arrived.Add(1) == 2 {
			close(bothListed)
		}
		select { // Both lookups see "no device" before either creates it
		case <-bothListed:
		case <-time.After(2 * time.Second):
		}
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, _, err := f.service.EnsureDevice(context.Background(), 261001); err != nil {
				t.Errorf("EnsureDevice: %v", err)
			}
		})
	}
	wg.Wait()
	testutil.KnownBug(t, f.tago.DeviceCount() == 1,
		"EnsureDevice has no per-pool lock: two first-time setups of a pool create two MASTER devices")
}

// endregion
// region CPO provisioning

// notificationForm is the body neos-notification-mgmt posts (GoogleFormPayload.model_dump(by_alias=False)).
func notificationForm(description string) string {
	return `{"user_email": "ana@acme.es", "company_name": "ACME", "cpo_fiscal_id": "B12345678",
		"company_address": "C/ Mayor 1", "contact_phone": "985000000", "contact_email": "info@acme.es",
		"manager_name": "Ana", "manager_dni": "12345678Z", "iban": "ES0000000000000000000000",
		"payment_system": "No", "description": ` + description + `, "timestamp": ""}`
}

func TestProvisionCPOCreatesTheUserTheDeviceAndTheDefaults(t *testing.T) {
	f := newFixture(t)
	var form Form
	if err := json.Unmarshal([]byte(notificationForm(`"Garaje comunitario"`)), &form); err != nil {
		t.Fatal(err)
	}

	result, err := f.service.ProvisionCPO(context.Background(), form)
	if err != nil || !result.UserCreated || !result.DeviceCreated || result.PoolCode != code(1001) {
		t.Fatalf("ProvisionCPO = %+v, %v", result, err)
	}
	if len(result.Password) != 16 || f.tago.Calls("POST /run/users") != 1 {
		t.Errorf("password length %d, users created %d", len(result.Password), f.tago.Calls("POST /run/users"))
	}
	deadline := time.Now().Add(3 * time.Second) // The defaults are written in the background
	for f.bucket.count() < 6 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := f.bucket.count(); got != 6 {
		t.Errorf("default variables written = %d; want 6", got)
	}
}

func TestFormAcceptsTheNotificationServiceDefaults(t *testing.T) {
	// When the form leaves the questions empty, neos-notification-mgmt fills payment_system with "No" and the
	// description with the question's title.
	var form Form
	if err := json.Unmarshal([]byte(notificationForm(`"Descripción del sitio para la instalación."`)), &form); err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if form.PaymentSystem != "No" || form.UserEmail != "ana@acme.es" {
		t.Errorf("form = %+v", form)
	}
}

func TestFormAcceptsANullDescription(t *testing.T) {
	// neos-notification-mgmt declares the description Optional (schemas/google_forms.py) and forwards a null as is;
	// it then answers the Apps Script with a 502. The legacy service rejected it too (422): the contract mismatch
	// can be fixed here or by defaulting the description in neos-notification-mgmt.
	var form Form
	err := json.Unmarshal([]byte(notificationForm("null")), &form)
	testutil.KnownBug(t, err == nil,
		"a null description, allowed by neos-notification-mgmt's model, makes the provisioning fail (also in the legacy service)")
}

// endregion
