package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportCopyHasTheDataWithoutTheDeviceTokens(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	const token = "device-token-that-must-not-leak"
	if err := s.UpsertDevice(ctx, Device{PoolCode: 990001, DeviceID: "dev-1", Token: token}); err != nil {
		t.Fatal(err)
	}
	_, err := s.InsertSession(ctx, SessionRecord{PoolCode: 990001, StationName: "ST1", ConnectorID: 1,
		TransactionID: 4321, CardAlias: "Carga libre", StartDate: "04/11/2026", TimeBand: "19:00 - 19:30",
		LastMeterValue: 6000, StopMotive: "DISCONNECT"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.InsertTelemetry(ctx, TelemetryRecord{PoolCode: 990001, StationName: "ST1", ConnectorID: 1,
		TransactionID: 4321, Timestamp: "2026-11-04T19:01:00+01:00", MeterValue: 100, Power: 6000, CurrentTariffBand: "Flat"})
	if err != nil {
		t.Fatal(err)
	}
	pending, _ := s.PendingBackupRows(ctx)

	dest := filepath.Join(t.TempDir(), "copy.sqlite3")
	if err := s.ExportCopy(ctx, dest); err != nil {
		t.Fatalf("ExportCopy: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(token)) {
		t.Fatal("the copy holds the device token")
	}
	if _, err := os.Stat(dest + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("the copy left a WAL file (%v): it must be a single file", err)
	}
	if n, _ := s.PendingBackupRows(ctx); n != pending {
		t.Fatalf("pending backup rows = %d after the copy; want %d (the copy doesn't touch the backup flags)", n, pending)
	}
	if devices, _ := s.Devices(ctx); len(devices) != 1 || devices[0].Token != token {
		t.Fatalf("live devices = %+v; want the token kept in the live database", devices)
	}

	exported, err := Open(ctx, dest)
	if err != nil {
		t.Fatalf("opening the copy: %v", err)
	}
	defer exported.Close()
	devices, _ := exported.Devices(ctx)
	if len(devices) != 1 || devices[0].DeviceID != "dev-1" || devices[0].Token != "" {
		t.Fatalf("copy devices = %+v; want the device with a blank token", devices)
	}
	session, err := exported.SessionByKey(ctx, 990001, "ST1", 1, 4321)
	if err != nil || session.CardAlias != "Carga libre" || session.StopMotive != "DISCONNECT" {
		t.Fatalf("copy session = %+v, %v", session, err)
	}
	if telemetry, _ := exported.SessionTelemetry(ctx, 990001, "ST1", 1, 4321); len(telemetry) != 1 {
		t.Fatalf("copy telemetry = %+v; want the one meter value", telemetry)
	}
}

func TestExportCopyRefusesAnExistingFile(t *testing.T) {
	s := openTestStore(t)
	dest := filepath.Join(t.TempDir(), "taken.sqlite3")
	if err := os.WriteFile(dest, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ExportCopy(context.Background(), dest); err == nil {
		t.Fatal("ExportCopy wrote over an existing file")
	}
	if raw, _ := os.ReadFile(dest); string(raw) != "keep me" {
		t.Fatalf("the existing file now holds %q", raw)
	}
}
