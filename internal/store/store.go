// Package store is the SQLite persistence of the service: the TagoIO device registry, the station profiles, the
// last known connector statuses and the charging session history and telemetry (used by the audit dashboard).
//
// The schema is the one the Python service created, so the same database file keeps working. Schema changes are
// versioned with PRAGMA user_version (see migrate); the Python service never set it, so an existing file starts at
// version 0 and the baseline migration only adds what is missing.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO), registered as "sqlite"
)

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database file and applies the pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: creating database directory: %w", err)
	}

	// WAL lets the dashboards read while a write is in progress; busy_timeout waits for a lock instead of failing.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: opening %s: %w", path, err)
	}
	// SQLite allows one writer at a time; a single connection serializes the (small) workload without SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// region Migrations

// migrations are applied in order; migrations[i] moves the schema from version i to version i+1.
var migrations = []func(ctx context.Context, tx *sql.Tx) error{
	migrateBaseline,
	migrateTelemetryChargePoint,
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: reading schema version: %w", err)
	}

	for ; version < len(migrations); version++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: starting migration %d: %w", version+1, err)
		}
		if err := migrations[version](ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", version+1, err)
		}
		// PRAGMA does not take bound parameters; version is an integer we control.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: setting schema version %d: %w", version+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: committing migration %d: %w", version+1, err)
		}
	}
	return nil
}

// migrateBaseline creates the schema of the Python service, or completes an existing one: it adds the rate and
// energy columns of the session history, and the unique index that rejects duplicated sessions. Unlike the Python
// service, which emptied the whole history table when the index was missing, it only removes the duplicates.
func migrateBaseline(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS tagoio_device(
			pool_code INTEGER NOT NULL PRIMARY KEY,
			device_id TEXT NOT NULL,
			device_token TEXT NOT NULL,
			is_modified INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE IF NOT EXISTS station_config(
			station_name TEXT NOT NULL PRIMARY KEY,
			pool_code INTEGER NOT NULL,
			noc INTEGER NOT NULL DEFAULT 1,
			is_modified INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE IF NOT EXISTS charging_session_history(
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			pool_code INTEGER NOT NULL,
			station_name TEXT NOT NULL,
			connector_id INTEGER NOT NULL,
			transaction_id INTEGER NOT NULL DEFAULT 0,
			card_alias TEXT NOT NULL,
			start_date TEXT NOT NULL,
			time_band TEXT NOT NULL,
			start_meter_value INTEGER NOT NULL,
			last_meter_value INTEGER NOT NULL,
			cost REAL NOT NULL,
			rate_off_peak REAL NOT NULL DEFAULT 0.0,
			rate_flat REAL NOT NULL DEFAULT 0.0,
			rate_peak REAL NOT NULL DEFAULT 0.0,
			energy_off_peak INTEGER NOT NULL DEFAULT 0,
			energy_flat INTEGER NOT NULL DEFAULT 0,
			energy_peak INTEGER NOT NULL DEFAULT 0,
			is_modified INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE IF NOT EXISTS charging_session_telemetry(
			transaction_id INTEGER NOT NULL,
			timestamp TEXT NOT NULL,
			meter_value INTEGER NOT NULL,
			power INTEGER NOT NULL,
			cost REAL NOT NULL,
			current_tariff_band TEXT NOT NULL,
			energy_off_peak INTEGER NOT NULL DEFAULT 0,
			energy_flat INTEGER NOT NULL DEFAULT 0,
			energy_peak INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (transaction_id, timestamp))`,
		`CREATE TABLE IF NOT EXISTS connector_status(
			pool_code INTEGER NOT NULL,
			station_name TEXT NOT NULL,
			connector_id INTEGER NOT NULL,
			charge_point_status TEXT NOT NULL,
			is_modified INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (pool_code, station_name, connector_id))`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	// Columns added to the session history after its first release.
	historyColumns := []struct{ name, definition string }{
		{"rate_off_peak", "REAL NOT NULL DEFAULT 0.0"},
		{"rate_flat", "REAL NOT NULL DEFAULT 0.0"},
		{"rate_peak", "REAL NOT NULL DEFAULT 0.0"},
		{"energy_off_peak", "INTEGER NOT NULL DEFAULT 0"},
		{"energy_flat", "INTEGER NOT NULL DEFAULT 0"},
		{"energy_peak", "INTEGER NOT NULL DEFAULT 0"},
	}
	existing, err := tableColumns(ctx, tx, "charging_session_history")
	if err != nil {
		return err
	}
	for _, column := range historyColumns {
		if existing[column.name] {
			continue
		}
		statement := fmt.Sprintf("ALTER TABLE charging_session_history ADD COLUMN %s %s", column.name, column.definition)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	var indexCount int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_charging_session_history'`).Scan(&indexCount)
	if err != nil {
		return err
	}
	if indexCount == 0 {
		dedupe := `DELETE FROM charging_session_history WHERE rowid NOT IN (
			SELECT MIN(rowid) FROM charging_session_history
			GROUP BY pool_code, station_name, connector_id, start_meter_value, last_meter_value)`
		if _, err := tx.ExecContext(ctx, dedupe); err != nil {
			return err
		}
		index := `CREATE UNIQUE INDEX idx_charging_session_history ON charging_session_history
			(pool_code, station_name, connector_id, start_meter_value, last_meter_value)`
		if _, err := tx.ExecContext(ctx, index); err != nil {
			return err
		}
	}
	return nil
}

// migrateTelemetryChargePoint adds the charge point to the session telemetry: transaction ids are only unique per
// charge point, so the meter values of two sessions with the same id were mixed in their audit exports. The existing
// meter values take the charge point of the history when exactly one charge point has that transaction id; the rest
// (sessions still in progress, or ids shared by several charge points) keep it NULL and are matched by the id alone.
func migrateTelemetryChargePoint(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE charging_session_telemetry_new(
			pool_code INTEGER,
			station_name TEXT,
			connector_id INTEGER,
			transaction_id INTEGER NOT NULL,
			timestamp TEXT NOT NULL,
			meter_value INTEGER NOT NULL,
			power INTEGER NOT NULL,
			cost REAL NOT NULL,
			current_tariff_band TEXT NOT NULL,
			energy_off_peak INTEGER NOT NULL DEFAULT 0,
			energy_flat INTEGER NOT NULL DEFAULT 0,
			energy_peak INTEGER NOT NULL DEFAULT 0,
			UNIQUE (pool_code, station_name, connector_id, transaction_id, timestamp))`,
		`INSERT INTO charging_session_telemetry_new
		SELECT h.pool_code, h.station_name, h.connector_id, t.transaction_id, t.timestamp, t.meter_value, t.power, t.cost,
			t.current_tariff_band, t.energy_off_peak, t.energy_flat, t.energy_peak
		FROM charging_session_telemetry t LEFT JOIN (
			SELECT transaction_id, MIN(pool_code) AS pool_code, MIN(station_name) AS station_name,
				MIN(connector_id) AS connector_id
			FROM charging_session_history GROUP BY transaction_id
			HAVING COUNT(DISTINCT pool_code || '/' || station_name || '/' || connector_id) = 1
		) h ON h.transaction_id = t.transaction_id`,
		`DROP TABLE charging_session_telemetry`,
		`ALTER TABLE charging_session_telemetry_new RENAME TO charging_session_telemetry`,
		`CREATE INDEX idx_charging_session_telemetry_transaction ON charging_session_telemetry (transaction_id)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// tableColumns returns the column names of a table.
func tableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

// endregion

// region Backups

// backupTables are the tables whose changes call for a new backup. The connector statuses change all the time and
// are rebuilt by the stations themselves, and the telemetry is only kept for the audits, so neither triggers one.
var backupTables = []string{"tagoio_device", "station_config", "charging_session_history"}

// Modification flag values of the backup tables.
const (
	notModified    = 0 // Included in the last uploaded backup
	modified       = 1 // Changed since the last backup (every write sets it)
	inBackupUpload = 2 // Captured by the backup being uploaded
)

// PendingBackupRows counts the rows changed since the last uploaded backup.
func (s *Store) PendingBackupRows(ctx context.Context) (int, error) {
	total := 0
	for _, table := range backupTables {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE is_modified <> %d", table, notModified)
		if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return 0, fmt.Errorf("store: counting modified rows of %s: %w", table, err)
		}
		total += count
	}
	return total, nil
}

// Snapshot marks the changed rows as captured and writes a consistent copy of the database to dest (which must
// not exist). After the upload, call FinishBackup with whether it succeeded: rows changed in the meantime keep
// their modified flag either way, so they go into the next backup.
func (s *Store) Snapshot(ctx context.Context, dest string) error {
	if err := s.setModifiedFlags(ctx, modified, inBackupUpload); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		_ = s.setModifiedFlags(ctx, inBackupUpload, modified)
		return fmt.Errorf("store: writing snapshot: %w", err)
	}
	return nil
}

// FinishBackup clears the captured rows after a successful upload, or marks them modified again after a failure.
func (s *Store) FinishBackup(ctx context.Context, uploaded bool) error {
	if uploaded {
		return s.setModifiedFlags(ctx, inBackupUpload, notModified)
	}
	return s.setModifiedFlags(ctx, inBackupUpload, modified)
}

func (s *Store) setModifiedFlags(ctx context.Context, from, to int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: starting backup flags update: %w", err)
	}
	for _, table := range backupTables {
		query := fmt.Sprintf("UPDATE %s SET is_modified = ? WHERE is_modified = ?", table)
		if _, err := tx.ExecContext(ctx, query, to, from); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: updating backup flags of %s: %w", table, err)
		}
	}
	return tx.Commit()
}

// endregion

// ErrNotFound is returned when a looked up row does not exist.
var ErrNotFound = errors.New("store: not found")
