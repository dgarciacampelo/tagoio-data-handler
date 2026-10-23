package store

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDevices(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, ok, err := s.MaxPoolCode(ctx); err != nil || ok {
		t.Fatalf("MaxPoolCode on empty store = ok %v, err %v; want no code", ok, err)
	}
	for _, d := range []Device{{261001, "dev-a", "tok-a"}, {261002, "dev-b", "tok-b"}, {261001, "dev-a", "tok-new"}} {
		if err := s.UpsertDevice(ctx, d); err != nil {
			t.Fatalf("UpsertDevice: %v", err)
		}
	}

	devices, err := s.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 2 || devices[0].Token != "tok-new" {
		t.Fatalf("Devices = %+v; want 2 devices, the first one with the replaced token", devices)
	}
	if code, ok, _ := s.MaxPoolCode(ctx); !ok || code != 261002 {
		t.Fatalf("MaxPoolCode = %d, %v; want 261002", code, ok)
	}
	if code, err := s.PoolCodeByDeviceID(ctx, "dev-b"); err != nil || code != 261002 {
		t.Fatalf("PoolCodeByDeviceID = %d, %v; want 261002", code, err)
	}
	if _, err := s.PoolCodeByDeviceID(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("PoolCodeByDeviceID(missing) error = %v; want ErrNotFound", err)
	}
}

func TestRegisterConnectorRaisesConnectorCount(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	steps := []struct {
		connectorID int
		wantNOC     int
	}{
		{0, 1}, // The station itself registers it with one connector
		{2, 2},
		{1, 2}, // A lower connector never lowers the count
	}
	for _, step := range steps {
		if err := s.RegisterConnector(ctx, 261001, "ST1", step.connectorID); err != nil {
			t.Fatalf("RegisterConnector(%d): %v", step.connectorID, err)
		}
		noc, ok, err := s.StationConnectorCount(ctx, "ST1")
		if err != nil || !ok || noc != step.wantNOC {
			t.Fatalf("after connector %d: noc = %d (ok %v, err %v); want %d", step.connectorID, noc, ok, err, step.wantNOC)
		}
	}

	if err := s.UpsertConnectorStatus(ctx, ConnectorStatus{261001, "ST1", 1, "Charging"}); err != nil {
		t.Fatalf("UpsertConnectorStatus: %v", err)
	}
	deleted, err := s.DeleteStation(ctx, 261001, "ST1")
	if err != nil || !deleted {
		t.Fatalf("DeleteStation = %v, %v; want true", deleted, err)
	}
	statuses, _ := s.ConnectorStatuses(ctx)
	if len(statuses) != 0 {
		t.Fatalf("connector statuses after delete = %+v; want none", statuses)
	}
}

func TestInsertSessionRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	record := SessionRecord{PoolCode: 261001, StationName: "ST1", ConnectorID: 1, TransactionID: 42, CardAlias: "CARD",
		StartDate: "22/10/2026", TimeBand: "10:00 - 11:00", StartMeterValue: 1000, LastMeterValue: 6000, Cost: 2.5}
	if inserted, err := s.InsertSession(ctx, record); err != nil || !inserted {
		t.Fatalf("first InsertSession = %v, %v; want inserted", inserted, err)
	}
	if inserted, err := s.InsertSession(ctx, record); err != nil || inserted {
		t.Fatalf("repeated InsertSession = %v, %v; want ignored without error", inserted, err)
	}

	summary, err := s.Session(ctx, 42)
	if err != nil || summary.TotalEnergyKWh != 5 {
		t.Fatalf("Session = %+v, %v; want 5 kWh", summary, err)
	}
	recent, err := s.RecentSessions(ctx, 261002, 10)
	if err != nil || len(recent) != 0 {
		t.Fatalf("RecentSessions(other pool) = %+v, %v; want none", recent, err)
	}
}

func TestPurgeTelemetryAcceptsISOTimestamps(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	for _, ts := range []string{"2020-01-01T10:00:00Z", "2099-01-01T10:00:00.123+00:00"} {
		if err := s.InsertTelemetry(ctx, TelemetryRecord{TransactionID: 1, Timestamp: ts, CurrentTariffBand: "Flat"}); err != nil {
			t.Fatalf("InsertTelemetry: %v", err)
		}
	}
	deleted, remaining, err := s.PurgeTelemetry(ctx, 120)
	if err != nil || deleted != 1 || remaining != 1 {
		t.Fatalf("PurgeTelemetry = %d deleted, %d remaining, %v; want 1 and 1", deleted, remaining, err)
	}
}

func TestBackupFlags(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if err := s.UpsertDevice(ctx, Device{261001, "dev-a", "tok-a"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.PendingBackupRows(ctx); n != 1 {
		t.Fatalf("pending rows = %d; want 1", n)
	}

	dest := filepath.Join(t.TempDir(), "snapshot.sqlite3")
	if err := s.Snapshot(ctx, dest); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// A change made while the backup uploads must stay pending after it succeeds.
	if err := s.UpsertDevice(ctx, Device{261002, "dev-b", "tok-b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishBackup(ctx, true); err != nil {
		t.Fatalf("FinishBackup: %v", err)
	}
	if n, _ := s.PendingBackupRows(ctx); n != 1 {
		t.Fatalf("pending rows after upload = %d; want 1 (the row changed during the upload)", n)
	}

	snapshot, err := Open(ctx, dest)
	if err != nil {
		t.Fatalf("opening snapshot: %v", err)
	}
	defer snapshot.Close()
	if devices, _ := snapshot.Devices(ctx); len(devices) != 1 {
		t.Fatalf("snapshot devices = %+v; want the one device stored before the snapshot", devices)
	}
}

func TestBaselineMigrationKeepsHistoryAndRemovesDuplicates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.sqlite3")

	// A legacy database: old history columns and no unique index, with one duplicated session.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE charging_session_history(
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, pool_code INTEGER NOT NULL, station_name TEXT NOT NULL,
			connector_id INTEGER NOT NULL, transaction_id INTEGER NOT NULL DEFAULT 0, card_alias TEXT NOT NULL,
			start_date TEXT NOT NULL, time_band TEXT NOT NULL, start_meter_value INTEGER NOT NULL,
			last_meter_value INTEGER NOT NULL, cost REAL NOT NULL, is_modified INTEGER NOT NULL DEFAULT 1);
		INSERT INTO charging_session_history (pool_code, station_name, connector_id, transaction_id, card_alias,
			start_date, time_band, start_meter_value, last_meter_value, cost)
		VALUES (1, 'ST', 1, 7, 'A', 'd', 't', 0, 10, 1), (1, 'ST', 1, 7, 'A', 'd', 't', 0, 10, 1),
			(1, 'ST', 1, 8, 'A', 'd', 't', 10, 20, 1);`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	sessions, err := s.RecentSessions(ctx, 0, 10)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions after migration = %d, %v; want 2 (the duplicate removed, the rest kept)", len(sessions), err)
	}
}

// TestOpenDatabaseCopy opens a copy of a real database (STORE_TEST_DB, never the original file) to check that the
// migrations accept it. It is skipped unless the variable is set.
func TestOpenDatabaseCopy(t *testing.T) {
	source := os.Getenv("STORE_TEST_DB")
	if source == "" {
		t.Skip("STORE_TEST_DB not set")
	}
	dest := filepath.Join(t.TempDir(), "copy.sqlite3")
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	_ = in.Close()
	_ = out.Close()

	ctx := context.Background()
	s, err := Open(ctx, dest)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	devices, err := s.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	statuses, err := s.ConnectorStatuses(ctx)
	if err != nil {
		t.Fatalf("ConnectorStatuses: %v", err)
	}
	sessions, err := s.RecentSessions(ctx, 0, 1000)
	if err != nil {
		t.Fatalf("RecentSessions: %v", err)
	}
	t.Logf("devices %d, connector statuses %d, sessions %d", len(devices), len(statuses), len(sessions))
}
