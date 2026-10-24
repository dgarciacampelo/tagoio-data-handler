// Package maintenance holds the housekeeping jobs: keeping the TagoIO buckets below their record limit, purging
// the old session telemetry, and backing up the database to Telegram.
package maintenance

import (
	"archive/zip"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// Bucket data thresholds (records per TagoIO device; TagoIO refuses inserts past 50 000).
const (
	cleanupThreshold  = 25_000 // Above it, the routine variables with many records are trimmed
	warningThreshold  = 40_000 // Above it, Telegram is warned and the routine variables are emptied
	variableThreshold = 250    // Records of one variable that make it worth trimming
	variablePageSize  = 10_000 // Records per page when counting the variables of a bucket
	routineRetention  = 2      // Weeks kept when trimming below the warning threshold
	telemetryDays     = 120    // Days of session telemetry kept in the database
)

// routinePrefixes are the variables the service writes all the time and can trim safely.
var routinePrefixes = []string{"active_cs_data", "state", "cost_", "energy_", "time_"}

// Notifier sends notices and backups.
type Notifier interface {
	Notify(ctx context.Context, message string) error
	SendBackup(ctx context.Context, path, caption string) error
}

// Service runs the maintenance jobs.
type Service struct {
	store         *store.Store
	tago          *tagoio.Client
	devices       *devices.Registry
	bucket        *bucket.Service
	notifier      Notifier
	log           *slog.Logger
	location      *time.Location
	backupDir     string
	backupsToKeep int

	backupMu sync.Mutex // One backup at a time (schedule and manual trigger)
	pause    func(ctx context.Context, d time.Duration) bool
}

// New creates the maintenance service. Backups are written next to the database file.
func New(s *store.Store, tago *tagoio.Client, registry *devices.Registry, b *bucket.Service, notifier Notifier,
	log *slog.Logger, location *time.Location, databasePath string, backupsToKeep int) *Service {
	return &Service{
		store: s, tago: tago, devices: registry, bucket: b, notifier: notifier, log: log, location: location,
		backupDir: filepath.Dir(databasePath), backupsToKeep: backupsToKeep, pause: sleep,
	}
}

// sleep waits d, or less when ctx ends; it reports whether the full wait elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// region Bucket data

// CheckDataAmount measures the records of every pool's bucket, warns on Telegram when one is close to the limit,
// and trims the routine variables of the buckets above the cleanup threshold.
func (s *Service) CheckDataAmount(ctx context.Context) {
	registered := s.devices.Devices()
	slices.SortFunc(registered, func(a, b devices.Device) int { return a.PoolCode - b.PoolCode })

	amounts := map[int]int{}
	nearLimit := false
	for _, d := range registered {
		if !s.pause(ctx, time.Second) { // TagoIO rate limits
			return
		}
		amount, err := s.tago.DataAmount(ctx, d.DeviceID)
		if err != nil {
			s.log.Warn("Maintenance: reading data amount failed", "pool_code", d.PoolCode, "error", err)
			continue
		}
		s.log.Info("Maintenance: bucket data amount", "pool_code", d.PoolCode, "records", amount)
		amounts[d.PoolCode] = amount
		nearLimit = nearLimit || amount > warningThreshold
	}
	if nearLimit {
		if err := s.notifier.Notify(ctx, "Some TagoIO devices are reaching the data limit."); err != nil {
			s.log.Error("Maintenance: Telegram notice failed", "error", err)
		}
	}

	for _, d := range registered {
		if amount := amounts[d.PoolCode]; amount > cleanupThreshold {
			s.trimBucket(ctx, d, amount)
		}
	}
}

// trimBucket counts the records of each variable of a bucket and trims the routine ones: those with many
// records, keeping two weeks, or all of them, emptied, when the bucket is close to the limit.
func (s *Service) trimBucket(ctx context.Context, d devices.Device, amount int) {
	counts := map[string]int{}
	for skip := 0; skip < amount; skip += variablePageSize {
		names, err := s.tago.VariableNames(ctx, d.Token, variablePageSize, skip)
		if err != nil {
			s.log.Warn("Maintenance: reading bucket page failed", "pool_code", d.PoolCode, "skip", skip, "error", err)
			continue
		}
		for _, name := range names {
			counts[name]++
		}
		if !s.pause(ctx, time.Second) {
			return
		}
	}
	s.log.Info("Maintenance: bucket variables counted", "pool_code", d.PoolCode, "variables", len(counts))

	nearLimit := amount >= warningThreshold
	retention := routineRetention
	if nearLimit {
		retention = 0
	}
	for name, count := range counts {
		if !isRoutine(name) || (count < variableThreshold && !nearLimit) {
			continue
		}
		removed, err := s.bucket.Delete(ctx, d.PoolCode, name, "", retention)
		if err != nil {
			s.log.Warn("Maintenance: trimming variable failed", "pool_code", d.PoolCode, "variable", name, "error", err)
			continue
		}
		s.log.Warn("Maintenance: variable trimmed", "pool_code", d.PoolCode, "variable", name, "records", count, "removed", removed)
		if !s.pause(ctx, time.Second) {
			return
		}
	}
}

func isRoutine(variable string) bool {
	for _, prefix := range routinePrefixes {
		if strings.HasPrefix(variable, prefix) {
			return true
		}
	}
	return false
}

// CleanupAllPools trims the routine variables of every pool, a pool every 30 seconds (TagoIO rate limits).
func (s *Service) CleanupAllPools(ctx context.Context) {
	codes := s.devices.PoolCodes()
	slices.Sort(codes)
	s.log.Warn("Maintenance: cleaning the variables of every pool", "pools", len(codes))
	for i, code := range codes {
		if i > 0 && !s.pause(ctx, 30*time.Second) {
			return
		}
		s.bucket.CleanupPool(ctx, code)
	}
}

// endregion

// PurgeTelemetry deletes the session telemetry older than 120 days.
func (s *Service) PurgeTelemetry(ctx context.Context) {
	deleted, remaining, err := s.store.PurgeTelemetry(ctx, telemetryDays)
	if err != nil {
		s.log.Error("Maintenance: telemetry purge failed", "error", err)
		return
	}
	s.log.Info("Maintenance: telemetry purged", "deleted", deleted, "remaining", remaining)
}

// region Backups

const backupPrefix = "tagoio_data_handler_backup_"

// MonthlyBackup backs up the database on the first day of the month, changed or not.
func (s *Service) MonthlyBackup(ctx context.Context) {
	if time.Now().In(s.location).Day() == 1 {
		s.Backup(ctx, true)
	}
}

// Backup sends a zipped snapshot of the database to the Telegram backups chat, when something changed since the
// last backup (or always, when forced). The changed rows stay marked until the upload succeeds.
func (s *Service) Backup(ctx context.Context, force bool) {
	s.backupMu.Lock()
	defer s.backupMu.Unlock()

	if !force {
		pending, err := s.store.PendingBackupRows(ctx)
		if err != nil {
			s.log.Error("Maintenance: checking pending backup rows failed", "error", err)
			return
		}
		if pending == 0 {
			s.log.Info("Maintenance: nothing changed since the last backup")
			return
		}
	}

	name := backupPrefix + time.Now().In(s.location).Format("2006-01-02_15.04.05")
	snapshot := filepath.Join(s.backupDir, name+".sqlite3")
	archive := filepath.Join(s.backupDir, name+".zip")

	if err := s.store.Snapshot(ctx, snapshot); err != nil {
		s.log.Error("Maintenance: database snapshot failed", "error", err)
		return
	}
	err := zipFile(snapshot, archive, name+".sqlite3")
	_ = os.Remove(snapshot)
	if err != nil {
		s.log.Error("Maintenance: zipping the backup failed", "error", err)
		_ = s.store.FinishBackup(ctx, false)
		return
	}

	err = s.notifier.SendBackup(ctx, archive, "uploading: "+filepath.Base(archive))
	if err != nil {
		s.log.Error("Maintenance: backup upload failed", "file", archive, "error", err)
	} else {
		s.log.Info("Maintenance: backup uploaded", "file", archive)
	}
	if err := s.store.FinishBackup(ctx, err == nil); err != nil {
		s.log.Error("Maintenance: updating backup flags failed", "error", err)
	}
	s.pruneBackups()
}

// zipFile writes src into a new zip archive dest, as an entry named entryName.
func zipFile(src, dest, entryName string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(dest)
		}
	}()

	writer := zip.NewWriter(out)
	writer.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, 8)
	})
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	if _, err := io.Copy(entry, in); err != nil {
		return err
	}
	return writer.Close()
}

// pruneBackups deletes the oldest backup archives beyond the number to keep (their names sort by date).
func (s *Service) pruneBackups() {
	if s.backupsToKeep <= 0 {
		return
	}
	archives, err := filepath.Glob(filepath.Join(s.backupDir, backupPrefix+"*.zip"))
	if err != nil || len(archives) <= s.backupsToKeep {
		return
	}
	slices.Sort(archives)
	for _, old := range archives[:len(archives)-s.backupsToKeep] {
		if err := os.Remove(old); err != nil {
			s.log.Warn("Maintenance: removing old backup failed", "file", old, "error", err)
		}
	}
}

// endregion

// Describe returns a short summary of the bucket thresholds, for the logs at startup.
func Describe() string {
	return fmt.Sprintf("bucket cleanup above %d records, warning above %d, telemetry kept %d days",
		cleanupThreshold, warningThreshold, telemetryDays)
}
