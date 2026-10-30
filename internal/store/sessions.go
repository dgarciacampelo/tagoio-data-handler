// This file holds the charging session history (one row per finished session, with its frozen rates) and the
// session telemetry (one row per meter value), which the audit dashboard and its XLSX export read.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// SessionRecord is a finished charging session, as stored in the history.
type SessionRecord struct {
	PoolCode        int
	StationName     string
	ConnectorID     int
	TransactionID   int64
	CardAlias       string
	StartDate       string // DD/MM/YYYY, as sent by the CSMS
	TimeBand        string // "HH:MM - HH:MM"
	StartMeterValue int64  // Wh
	LastMeterValue  int64  // Wh
	Cost            float64
	RateOffPeak     float64 // €/kWh
	RateFlat        float64
	RatePeak        float64
	EnergyOffPeak   int64 // Wh
	EnergyFlat      int64
	EnergyPeak      int64
}

// TelemetryRecord is one meter value of a charging session. Transaction ids are only unique per charge point, so
// the charge point is part of the session's identity.
type TelemetryRecord struct {
	PoolCode          int
	StationName       string
	ConnectorID       int
	TransactionID     int64
	Timestamp         string // ISO 8601, as sent by the CSMS
	MeterValue        int64  // Wh
	Power             int64  // W
	Cost              float64
	CurrentTariffBand string // "Off-Peak", "Flat" or "Peak"
	EnergyOffPeak     int64  // Cumulative Wh per band
	EnergyFlat        int64
	EnergyPeak        int64
}

// SessionSummary is a history row as the audit dashboard shows it.
type SessionSummary struct {
	ID             int64 // rowid of the history row: neither the transaction id nor the charge point identify a row
	TransactionID  int64
	PoolCode       int
	StationName    string
	ConnectorID    int
	StartDate      string
	TimeBand       string
	Cost           float64
	CardAlias      string
	TotalEnergyKWh float64
	RateOffPeak    float64
	RateFlat       float64
	RatePeak       float64
	EnergyOffPeak  int64
	EnergyFlat     int64
	EnergyPeak     int64
}

// InsertSession adds a finished session to the history. It reports false, without error, when the session was
// already stored (the CSMS can repeat the final update).
func (s *Store) InsertSession(ctx context.Context, r SessionRecord) (bool, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO charging_session_history (
			pool_code, station_name, connector_id, transaction_id, card_alias, start_date, time_band,
			start_meter_value, last_meter_value, cost, rate_off_peak, rate_flat, rate_peak,
			energy_off_peak, energy_flat, energy_peak, is_modified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		r.PoolCode, r.StationName, r.ConnectorID, r.TransactionID, r.CardAlias, r.StartDate, r.TimeBand,
		r.StartMeterValue, r.LastMeterValue, r.Cost, r.RateOffPeak, r.RateFlat, r.RatePeak,
		r.EnergyOffPeak, r.EnergyFlat, r.EnergyPeak)
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("store: inserting session %d: %w", r.TransactionID, err)
	}
	return true, nil
}

// InsertTelemetry stores a meter value of a session; a repeated timestamp is ignored.
func (s *Store) InsertTelemetry(ctx context.Context, t TelemetryRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO charging_session_telemetry (
			pool_code, station_name, connector_id, transaction_id, timestamp, meter_value, power, cost,
			current_tariff_band, energy_off_peak, energy_flat, energy_peak)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.PoolCode, t.StationName, t.ConnectorID, t.TransactionID, t.Timestamp, t.MeterValue, t.Power, t.Cost,
		t.CurrentTariffBand, t.EnergyOffPeak, t.EnergyFlat, t.EnergyPeak)
	if err != nil {
		return fmt.Errorf("store: inserting telemetry of session %d: %w", t.TransactionID, err)
	}
	return nil
}

// SessionTelemetry returns the meter values of a session in time order. Meter values stored before the telemetry
// recorded its charge point (pool_code NULL) are matched by the transaction id alone.
func (s *Store) SessionTelemetry(ctx context.Context, poolCode int, stationName string, connectorID int, transactionID int64) ([]TelemetryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT transaction_id, timestamp, meter_value, power, cost, current_tariff_band,
			energy_off_peak, energy_flat, energy_peak
		FROM charging_session_telemetry
		WHERE transaction_id = ? AND (pool_code IS NULL OR (pool_code = ? AND station_name = ? AND connector_id = ?))
		ORDER BY timestamp`, transactionID, poolCode, stationName, connectorID)
	if err != nil {
		return nil, fmt.Errorf("store: reading telemetry of session %d: %w", transactionID, err)
	}
	defer rows.Close()

	var records []TelemetryRecord
	for rows.Next() {
		t := TelemetryRecord{PoolCode: poolCode, StationName: stationName, ConnectorID: connectorID}
		err := rows.Scan(&t.TransactionID, &t.Timestamp, &t.MeterValue, &t.Power, &t.Cost, &t.CurrentTariffBand,
			&t.EnergyOffPeak, &t.EnergyFlat, &t.EnergyPeak)
		if err != nil {
			return nil, fmt.Errorf("store: reading telemetry row: %w", err)
		}
		records = append(records, t)
	}
	return records, rows.Err()
}

// summaryColumns are the columns of a SessionSummary, in scan order.
const summaryColumns = `rowid, transaction_id, pool_code, station_name, connector_id, start_date, time_band, cost, card_alias,
	(last_meter_value - start_meter_value) / 1000.0, rate_off_peak, rate_flat, rate_peak,
	energy_off_peak, energy_flat, energy_peak`

func scanSummary(row interface{ Scan(...any) error }) (SessionSummary, error) {
	var s SessionSummary
	err := row.Scan(&s.ID, &s.TransactionID, &s.PoolCode, &s.StationName, &s.ConnectorID, &s.StartDate, &s.TimeBand,
		&s.Cost, &s.CardAlias, &s.TotalEnergyKWh, &s.RateOffPeak, &s.RateFlat, &s.RatePeak,
		&s.EnergyOffPeak, &s.EnergyFlat, &s.EnergyPeak)
	return s, err
}

// Session returns a history row by its ID (SessionSummary.ID), or ErrNotFound.
func (s *Store) Session(ctx context.Context, id int64) (SessionSummary, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+summaryColumns+" FROM charging_session_history WHERE rowid = ?", id)
	summary, err := scanSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionSummary{}, ErrNotFound
	}
	if err != nil {
		return SessionSummary{}, fmt.Errorf("store: reading history row %d: %w", id, err)
	}
	return summary, nil
}

// RecentSessions returns the latest finished sessions, newest first; poolCode 0 means every pool.
func (s *Store) RecentSessions(ctx context.Context, poolCode, limit int) ([]SessionSummary, error) {
	query := "SELECT " + summaryColumns + " FROM charging_session_history"
	args := []any{}
	if poolCode != 0 {
		query += " WHERE pool_code = ?"
		args = append(args, poolCode)
	}
	query += " ORDER BY created_at DESC, rowid DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: listing recent sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("store: reading session row: %w", err)
		}
		sessions = append(sessions, summary)
	}
	return sessions, rows.Err()
}

// PurgeTelemetry deletes the meter values older than the given number of days. It returns how many rows it
// deleted and how many remain. Timestamps are compared as dates (julianday), so both the CSMS ISO format
// ("2026-10-22T10:00:00Z") and SQLite's ("2026-10-22 10:00:00") work.
func (s *Store) PurgeTelemetry(ctx context.Context, days int) (deleted, remaining int64, err error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM charging_session_telemetry WHERE julianday(timestamp) < julianday('now', ?)",
		fmt.Sprintf("-%d days", days))
	if err != nil {
		return 0, 0, fmt.Errorf("store: purging telemetry: %w", err)
	}
	deleted, _ = result.RowsAffected()
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM charging_session_telemetry").Scan(&remaining); err != nil {
		return deleted, 0, fmt.Errorf("store: counting telemetry: %w", err)
	}
	return deleted, remaining, nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE (or primary key) constraint failure.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code()
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}
