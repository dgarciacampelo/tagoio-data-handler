package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

func TestDatabaseSnapshotDownloadsAZippedCopyWithoutDeviceTokens(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	app.Config.GitCommit = "abc1234"
	if err := app.Store.UpsertDevice(ctx, store.Device{PoolCode: 990001, DeviceID: "dev-1", Token: "secret-device-token"}); err != nil {
		t.Fatal(err)
	}
	_, err := app.Store.InsertSession(ctx, store.SessionRecord{PoolCode: 990001, StationName: "ST1", ConnectorID: 2,
		TransactionID: 4321, CardAlias: "Carga libre", StartDate: "04/11/2026", TimeBand: "19:00 - 20:40", LastMeterValue: 6000})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	app.DatabaseSnapshotAPI(rec, httptest.NewRequest(http.MethodGet, "/api/admin/database/snapshot", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	disposition := regexp.MustCompile(`^attachment; filename="tagoio_data_handler_snapshot_\d{4}-\d\d-\d\d_\d\d\.\d\d\.\d\d_abc1234\.zip"$`)
	if got := rec.Header().Get("Content-Disposition"); !disposition.MatchString(got) {
		t.Errorf("Content-Disposition = %q", got)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("secret-device-token")) {
		t.Fatal("the archive holds the device token") // Deflate could hide it, so the copy is checked below too
	}

	archive, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	if len(archive.File) != 1 || !regexp.MustCompile(`^tagoio_data_handler_snapshot_.*_abc1234\.sqlite3$`).MatchString(archive.File[0].Name) {
		t.Fatalf("archive entries = %v; want the one database copy", archive.File)
	}
	copied := extract(t, archive.File[0])
	if bytes.Contains(copied, []byte("secret-device-token")) {
		t.Fatal("the database copy holds the device token")
	}

	path := filepath.Join(t.TempDir(), "copy.sqlite3")
	if err := os.WriteFile(path, copied, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("opening the copy: %v", err)
	}
	defer snapshot.Close()
	if devices, _ := snapshot.Devices(ctx); len(devices) != 1 || devices[0].Token != "" {
		t.Errorf("copy devices = %+v; want the device without its token", devices)
	}
	if session, err := snapshot.SessionByKey(ctx, 990001, "ST1", 2, 4321); err != nil || session.CardAlias != "Carga libre" {
		t.Errorf("copy session = %+v, %v", session, err)
	}
}

func TestDatabaseSnapshotMakesOneCopyAtATime(t *testing.T) {
	app := newTestApp(t)
	app.snapshotMu.Lock() // A snapshot in progress
	defer app.snapshotMu.Unlock()

	rec := httptest.NewRecorder()
	app.DatabaseSnapshotAPI(rec, httptest.NewRequest(http.MethodGet, "/api/admin/database/snapshot", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d; want 409 while another snapshot is made", rec.Code)
	}
}

func TestSnapshotNameKeepsOnlySafeCharactersOfTheCommit(t *testing.T) {
	at := time.Date(2026, 11, 6, 15, 4, 5, 0, time.UTC)
	cases := map[string]string{
		"04d359c":                   "tagoio_data_handler_snapshot_2026-11-06_15.04.05_04d359c",
		"":                          "tagoio_data_handler_snapshot_2026-11-06_15.04.05_unknown",
		`a"b/../c d`:                "tagoio_data_handler_snapshot_2026-11-06_15.04.05_abcd",
		"04d359caefb8c649c8d3431bd": "tagoio_data_handler_snapshot_2026-11-06_15.04.05_04d359caefb8",
	}
	for commit, want := range cases {
		if got := snapshotName(at, commit); got != want {
			t.Errorf("snapshotName(%q) = %q; want %q", commit, got, want)
		}
	}
}

// extract reads an archive entry.
func extract(t *testing.T, f *zip.File) []byte {
	t.Helper()
	r, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
