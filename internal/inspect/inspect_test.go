package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

type fakeStations map[int][]bucket.Connector

func (f fakeStations) PoolConnectors(poolCode int) []bucket.Connector { return f[poolCode] }

type fixture struct {
	service  *Service
	tago     *testutil.FakeTagoIO
	registry *devices.Registry
	db       *store.Store
}

func newFixture(t *testing.T, stations Stations) fixture {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "inspect.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tago := testutil.NewFakeTagoIO(t)
	registry := devices.NewRegistry(db, tago.Client(), "MASTER-", log)
	return fixture{service: New(tago.Client(), registry, stations, log), tago: tago, registry: registry, db: db}
}

func poolTags(code, plan string) []tagoio.Tag {
	return []tagoio.Tag{{Key: "group_id", Value: code}, {Key: "type", Value: "MASTER"}, {Key: "plan", Value: plan}}
}

// twoSignUps is a CPO that signed up twice with the same email: pool 990005 has its user, its configuration and a
// registered device; pool 990006 got a device with an operator_info only, nobody sees it, and the service hasn't
// registered it.
func twoSignUps(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t, fakeStations{990005: {{StationName: "ST-B", ConnectorID: 1}, {StationName: "ST-A", ConnectorID: 1}, {StationName: "ST-A", ConnectorID: 2}}})
	f.tago.AddDevice("dev-5", "MASTER-BUSINESS-990005", "token-5", poolTags("990005", "ADVANCED")...)
	f.tago.AddDevice("dev-6", "MASTER-BUSINESS-990006", "token-6", poolTags("990006", "ADVANCED")...)
	f.tago.AddDevice("dev-x", "PAYMENTS-GATEWAY", "token-x")
	f.tago.SetDeviceDetails("dev-5", testutil.DeviceDetails{Active: true, CreatedAt: "2026-07-27T09:40:00.000Z", LastInput: "2026-11-18T10:00:00.000Z"})

	operator := map[string]any{"web": "", "nombre": "Example Inn", "correo": "OPS@example.test"}
	f.tago.AddRecord("token-5", tagoio.DataRecord{ID: "1", Variable: "operator_info", Value: 0, Group: "1", Metadata: operator})
	f.tago.AddRecord("token-5", tagoio.DataRecord{ID: "2", Variable: "rate_costs", Value: 0, Group: "1",
		Metadata: map[string]any{"valle": 0.4, "llanas": 0.4, "punta": 0.4, "IVA": 0.1}})
	for i, v := range []string{"max_installation_power", "max_grid_power_consumption", "load_balancing_mode", "withholding_amount"} {
		f.tago.AddRecord("token-5", tagoio.DataRecord{ID: string(rune('3' + i)), Variable: v, Value: 1, Group: "1"})
	}
	f.tago.AddRecord("token-6", tagoio.DataRecord{ID: "1", Variable: "operator_info", Value: 0, Group: "1", Metadata: operator})

	f.tago.AddUser(testutil.FakeUser{ID: "user-1", Name: "Manager", Email: "ops@example.test", Active: true,
		Tags: []tagoio.Tag{{Key: "group_id", Value: "990005"}, {Key: "user_type", Value: "MANAGER"}}})
	f.tago.AddUser(testutil.FakeUser{ID: "user-2", Name: "Other", Email: "other@example.test", Active: true,
		Tags: []tagoio.Tag{{Key: "group_id", Value: "990001"}}})

	if err := f.registry.Register(context.Background(), devices.Device{PoolCode: 990005, DeviceID: "dev-5", Token: "token-5"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPoolReportOfAHealthyPool(t *testing.T) {
	f := twoSignUps(t)
	report, err := f.service.Pool(context.Background(), 990005)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Devices) != 1 || report.Devices[0].ID != "dev-5" || !report.Devices[0].Registered || report.Devices[0].PoolCode != 990005 {
		t.Fatalf("devices = %+v", report.Devices)
	}
	if report.Devices[0].LastInput != "2026-11-18T10:00:00.000Z" {
		t.Errorf("last input = %q", report.Devices[0].LastInput)
	}
	if report.Registered == nil || report.Registered.DeviceID != "dev-5" {
		t.Errorf("registered = %+v", report.Registered)
	}
	if !slices.Equal(report.Stations, []string{"ST-A", "ST-B"}) {
		t.Errorf("stations = %v", report.Stations)
	}
	if report.DataAmount == nil || *report.DataAmount != 6 {
		t.Errorf("data amount = %v", report.DataAmount)
	}
	for _, v := range ConfigVariables {
		if report.Configuration[v] == nil {
			t.Errorf("configuration %s missing", v)
		}
	}
	if rates := report.Configuration["rate_costs"]; rates == nil || rates.Metadata["IVA"] == nil {
		t.Errorf("rate_costs = %+v", rates)
	}
	if len(report.Users) != 1 || report.Users[0].ID != "user-1" || !slices.Equal(report.Users[0].GroupIDs, []string{"990005"}) {
		t.Errorf("users = %+v", report.Users)
	}
	if len(report.OperatorEmailUsers) != 1 || report.OperatorEmailUsers[0].ID != "user-1" {
		t.Errorf("operator email users = %+v", report.OperatorEmailUsers)
	}
	if !slices.Equal(report.Findings, []string{"operator_info has no web."}) || len(report.Errors) != 0 {
		t.Errorf("findings = %q, errors = %q", report.Findings, report.Errors)
	}
}

func TestPoolReportOfASecondSignUp(t *testing.T) {
	f := twoSignUps(t)
	report, err := f.service.Pool(context.Background(), 990006)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Devices) != 1 || report.Devices[0].ID != "dev-6" || report.Devices[0].Registered {
		t.Fatalf("devices = %+v", report.Devices)
	}
	if report.Registered != nil {
		t.Errorf("registered = %+v", report.Registered)
	}
	// The bucket is read with the token TagoIO gives, but the device is not registered by the inspection.
	if report.Configuration["operator_info"] == nil || report.DataAmount == nil || *report.DataAmount != 1 {
		t.Errorf("configuration = %+v, data amount = %v", report.Configuration, report.DataAmount)
	}
	if stored, _ := f.db.Devices(context.Background()); len(stored) != 1 {
		t.Errorf("the inspection registered a device: %+v", stored)
	}
	if len(report.Users) != 0 {
		t.Errorf("users = %+v", report.Users)
	}
	if len(report.OperatorEmailUsers) != 1 || report.OperatorEmailUsers[0].ID != "user-1" {
		t.Errorf("operator email users = %+v", report.OperatorEmailUsers)
	}
	for _, want := range []string{
		"The service has not registered the device yet",
		"The bucket has no rate_costs.",
		"No run user carries group_id 990006",
		"The operator email's user ops@example.test carries group_id 990005, not 990006.",
		"The service knows no station of the pool.",
	} {
		if !slices.ContainsFunc(report.Findings, func(s string) bool { return strings.HasPrefix(s, want) }) {
			t.Errorf("finding %q missing from %q", want, report.Findings)
		}
	}
	if f.tago.Calls("POST /device") != 0 || f.tago.Calls("POST /data") != 0 || f.tago.Calls("POST /run/users") != 0 {
		t.Error("the inspection wrote to TagoIO")
	}
}

func TestPoolReportWithoutADevice(t *testing.T) {
	f := twoSignUps(t)
	report, err := f.service.Pool(context.Background(), 990007)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Devices) != 0 || report.Registered != nil || report.DataAmount != nil {
		t.Errorf("report = %+v", report)
	}
	for _, v := range ConfigVariables {
		if value, ok := report.Configuration[v]; !ok || value != nil {
			t.Errorf("configuration %s = %v, present %v; want null", v, value, ok)
		}
	}
	if len(report.Findings) == 0 || report.Findings[0] != "No TagoIO device named MASTER-BUSINESS-990007." {
		t.Errorf("findings = %q", report.Findings)
	}
	if f.tago.Calls("POST /device") != 0 {
		t.Error("the inspection created a device")
	}
}

func TestPoolReportListsTheFailedReads(t *testing.T) {
	f := twoSignUps(t)
	f.tago.Fail("GET /run/users", true)
	f.tago.Fail("GET /device/data_amount", true)
	report, err := f.service.Pool(context.Background(), 990005)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 3 { // data amount, users, operator email users
		t.Errorf("errors = %q", report.Errors)
	}
	if report.Configuration["rate_costs"] == nil {
		t.Error("the configuration was not read")
	}
	if slices.ContainsFunc(report.Findings, func(s string) bool { return strings.HasPrefix(s, "No run user") }) {
		t.Errorf("a failed user read is reported as no users: %q", report.Findings)
	}

	f.tago.Fail("GET /device", true)
	if _, err := f.service.Pool(context.Background(), 990005); err == nil {
		t.Error("a failed device listing gave a report")
	}
}

func TestReportsNeverCarryTokens(t *testing.T) {
	f := twoSignUps(t)
	ctx := context.Background()
	pool, err := f.service.Pool(ctx, 990006)
	if err != nil {
		t.Fatal(err)
	}
	all, err := f.service.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	users, err := f.service.Users(ctx, UserQuery{GroupID: "990005"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]any{pool, all, users})
	for _, token := range []string{"token-5", "token-6", "token-x", "account-token"} {
		if strings.Contains(string(raw), token) {
			t.Errorf("a report carries %s", token)
		}
	}
}

func TestDevicesReport(t *testing.T) {
	f := twoSignUps(t)
	if err := f.registry.Register(context.Background(), devices.Device{PoolCode: 990002, DeviceID: "gone", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	report, err := f.service.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Devices) != 3 {
		t.Fatalf("devices = %+v", report.Devices)
	}
	byID := map[string]Device{}
	for _, d := range report.Devices {
		byID[d.ID] = d
	}
	if !byID["dev-5"].Registered || byID["dev-6"].Registered || byID["dev-x"].PoolCode != 0 || byID["dev-6"].PoolCode != 990006 {
		t.Errorf("devices = %+v", byID)
	}
	if want := []RegisteredDevice{{PoolCode: 990002, DeviceID: "gone"}}; !slices.Equal(report.RegisteredWithoutDevice, want) {
		t.Errorf("registered without device = %+v", report.RegisteredWithoutDevice)
	}
}

func TestUsersQuery(t *testing.T) {
	f := twoSignUps(t)
	ctx := context.Background()
	if _, err := f.service.Users(ctx, UserQuery{Email: "  "}); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("empty query: %v", err)
	}
	// TagoIO filters the email as given; the service compares it case-insensitively.
	users, err := f.service.Users(ctx, UserQuery{Email: "ops@example.test", GroupID: "990005"})
	if err != nil || len(users) != 1 || users[0].ID != "user-1" {
		t.Errorf("users = %+v, %v", users, err)
	}
	users, err = f.service.Users(ctx, UserQuery{Email: "ops@example.test", GroupID: "990001"})
	if err != nil || len(users) != 0 {
		t.Errorf("users with another group = %+v, %v", users, err)
	}
}
