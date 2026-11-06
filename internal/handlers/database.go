// This file holds the download of a copy of the database, to inspect the production data locally.

package handlers

import (
	"archive/zip"
	"cmp"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

// snapshotPrefix starts the names of the downloaded copies; the backups sent to Telegram use their own.
const snapshotPrefix = "tagoio_data_handler_snapshot_"

// DatabaseSnapshotAPI downloads a consistent copy of the database, zipped (GET /api/admin/database/snapshot). The
// copy has every table: the device registry, the station profiles, the session history and its telemetry, the
// connector statuses and the Payter terminals. The TagoIO device tokens are blanked.
//
// The copy is for reading only. Corrections go through the admin APIs that change one thing and keep TagoIO in step,
// such as the card alias of a finished session, never through uploading a database to the running service. One copy
// is made at a time; another request meanwhile answers 409.
func (a *App) DatabaseSnapshotAPI(w http.ResponseWriter, r *http.Request) {
	if !a.snapshotMu.TryLock() {
		writeAPIError(w, http.StatusConflict, dto.ErrorCodeConflict,
			"A database snapshot is already being prepared: try again when it finishes")
		return
	}
	defer a.snapshotMu.Unlock()

	started := time.Now()
	name := snapshotName(started.In(cmp.Or(a.Config.Location, time.Local)), a.Config.GitCommit)
	dir, err := os.MkdirTemp("", "db-snapshot-")
	if err != nil {
		a.Log.Error("Database snapshot: creating the temporary folder failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Snapshot failed")
		return
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, name+".sqlite3")
	if err := a.Store.ExportCopy(r.Context(), path); err != nil {
		a.Log.Error("Database snapshot: copying the database failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Snapshot failed")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		a.Log.Error("Database snapshot: opening the copy failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Snapshot failed")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		a.Log.Error("Database snapshot: reading the size of the copy failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Snapshot failed")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	w.Header().Set("Cache-Control", "no-store")
	sent := &countingWriter{w: w}
	if err := zipSnapshot(sent, file, name+".sqlite3", started); err != nil {
		// The answer has started: the client is left with a truncated archive, which doesn't open
		a.Log.Error("Database snapshot: sending failed", "client", clientAddress(r), "sent_bytes", sent.n, "error", err)
		return
	}
	a.Log.Info("Database snapshot downloaded", "client", clientAddress(r), "file", name+".zip",
		"database_bytes", info.Size(), "sent_bytes", sent.n, "duration", time.Since(started).Round(time.Millisecond))
}

// snapshotName names a copy by when it was made and the commit of the service that made it, e.g.
// tagoio_data_handler_snapshot_2026-11-06_15.04.05_04d359c. The commit keeps only letters and digits, so the name
// is always safe in a file name and a header.
func snapshotName(at time.Time, commit string) string {
	commit = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return -1
	}, commit)
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return snapshotPrefix + at.Format("2006-01-02_15.04.05") + "_" + cmp.Or(commit, "unknown")
}

// zipSnapshot writes a zip archive with the copy as its only entry. The archive is streamed: zip writes the sizes
// after the data, so nothing needs to be held in memory or on disk.
func zipSnapshot(w io.Writer, src io.Reader, entryName string, modified time.Time) error {
	archive := zip.NewWriter(w)
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Deflate, Modified: modified})
	if err != nil {
		return err
	}
	if _, err := io.Copy(entry, src); err != nil {
		return err
	}
	return archive.Close()
}

// countingWriter counts the bytes written through it.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
