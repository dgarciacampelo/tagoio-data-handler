// This file holds the Payter terminals edited on the eMSP dashboard. The Payter CSMS stores each change it receives
// as a manage_payter_terminal event, and reads the whole list at startup (GET /api/payter-terminals).

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrSharedTerminalExists rejects a second active pool-shared terminal in a pool.
var ErrSharedTerminalExists = errors.New("store: the pool already has an active pool-shared Payter terminal")

// PayterTerminal is a Payter terminal and the stations it serves.
type PayterTerminal struct {
	SerialNumber   string
	PoolCode       int
	IsPoolShared   bool     // Serves every station of the pool: it has no LinkedStations
	LinkedStations []string // Sorted
	IsActive       bool     // Never deleted: the CSMS keeps the payments made on it
	UpdatedAt      string   // SQLite CURRENT_TIMESTAMP, UTC
}

// migratePayterTerminals adds the Payter terminals, seeded with the production terminals they replace in neos-ocpp
// (seed_data.POS_TERMINALS until November 2026).
func migratePayterTerminals(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE payter_terminal(
			serial_number TEXT NOT NULL PRIMARY KEY,
			pool_code INTEGER NOT NULL,
			is_pool_shared INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		// One row per station a terminal serves: the primary key allows a single terminal per station
		`CREATE TABLE payter_terminal_station(
			pool_code INTEGER NOT NULL,
			station_name TEXT NOT NULL,
			serial_number TEXT NOT NULL REFERENCES payter_terminal(serial_number) ON DELETE CASCADE,
			PRIMARY KEY (pool_code, station_name))`,
		`INSERT INTO payter_terminal (serial_number, pool_code, is_pool_shared) VALUES
			('P6X20222101349', 221006, 0),
			('P6X20222100772', 221006, 0),
			('P6X20231300653', 241004, 1)`,
		`INSERT INTO payter_terminal_station (pool_code, station_name, serial_number) VALUES
			(221006, '22V20537CCPOS', 'P6X20222101349'),
			(221006, '22V20538CCPOS', 'P6X20222100772')`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// PayterTerminals returns every terminal, active or not, by pool code and serial number.
func (s *Store) PayterTerminals(ctx context.Context) ([]PayterTerminal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.serial_number, t.pool_code, t.is_pool_shared, t.is_active, t.updated_at,
			COALESCE((SELECT group_concat(station_name, char(31)) FROM payter_terminal_station
				WHERE serial_number = t.serial_number), '')
		FROM payter_terminal t ORDER BY t.pool_code, t.serial_number`)
	if err != nil {
		return nil, fmt.Errorf("store: reading Payter terminals: %w", err)
	}
	defer rows.Close()

	terminals := []PayterTerminal{}
	for rows.Next() {
		var t PayterTerminal
		var stations string
		if err := rows.Scan(&t.SerialNumber, &t.PoolCode, &t.IsPoolShared, &t.IsActive, &t.UpdatedAt, &stations); err != nil {
			return nil, fmt.Errorf("store: reading Payter terminals: %w", err)
		}
		t.LinkedStations = []string{}
		if stations != "" {
			t.LinkedStations = strings.Split(stations, "\x1f") // Unit separator: never part of a station name
			slices.Sort(t.LinkedStations)
		}
		terminals = append(terminals, t)
	}
	return terminals, rows.Err()
}

// SavePayterTerminal creates or updates an active terminal with the stations it serves. A station linked to another
// terminal moves to this one; a pool-shared terminal keeps no stations. It returns the moved stations, as
// {station: previous terminal}, and ErrSharedTerminalExists when the pool already has another pool-shared terminal.
func (s *Store) SavePayterTerminal(ctx context.Context, t PayterTerminal) (map[string]string, error) {
	stations := t.LinkedStations
	if t.IsPoolShared {
		stations = nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: saving Payter terminal %s: %w", t.SerialNumber, err)
	}
	defer func() { _ = tx.Rollback() }()

	if t.IsPoolShared {
		var other string
		err := tx.QueryRowContext(ctx, `SELECT serial_number FROM payter_terminal
			WHERE pool_code = ? AND is_pool_shared = 1 AND is_active = 1 AND serial_number <> ?`,
			t.PoolCode, t.SerialNumber).Scan(&other)
		if err == nil {
			return nil, fmt.Errorf("%w (%s)", ErrSharedTerminalExists, other)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: saving Payter terminal %s: %w", t.SerialNumber, err)
		}
	}

	moved := map[string]string{}
	for _, station := range stations {
		var previous string
		err := tx.QueryRowContext(ctx, `SELECT serial_number FROM payter_terminal_station
			WHERE pool_code = ? AND station_name = ? AND serial_number <> ?`, t.PoolCode, station, t.SerialNumber).Scan(&previous)
		switch {
		case err == nil:
			moved[station] = previous
		case !errors.Is(err, sql.ErrNoRows):
			return nil, fmt.Errorf("store: saving Payter terminal %s: %w", t.SerialNumber, err)
		}
	}

	type statement struct {
		query string
		args  []any
	}
	statements := []statement{
		{`INSERT INTO payter_terminal (serial_number, pool_code, is_pool_shared, is_active) VALUES (?, ?, ?, 1)
			ON CONFLICT(serial_number) DO UPDATE SET pool_code = excluded.pool_code,
				is_pool_shared = excluded.is_pool_shared, is_active = 1, updated_at = CURRENT_TIMESTAMP`,
			[]any{t.SerialNumber, t.PoolCode, t.IsPoolShared}},
		{`DELETE FROM payter_terminal_station WHERE serial_number = ?`, []any{t.SerialNumber}},
	}
	for _, station := range stations {
		statements = append(statements,
			statement{`DELETE FROM payter_terminal_station WHERE pool_code = ? AND station_name = ?`,
				[]any{t.PoolCode, station}}, // Moves it from its previous terminal
			statement{`INSERT INTO payter_terminal_station (pool_code, station_name, serial_number) VALUES (?, ?, ?)`,
				[]any{t.PoolCode, station, t.SerialNumber}},
		)
	}
	for _, s := range statements {
		if _, err := tx.ExecContext(ctx, s.query, s.args...); err != nil {
			return nil, fmt.Errorf("store: saving Payter terminal %s: %w", t.SerialNumber, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: saving Payter terminal %s: %w", t.SerialNumber, err)
	}
	return moved, nil
}

// DeactivatePayterTerminal deactivates a terminal and frees its stations; the row stays. It returns the pool of the
// terminal, and false when there is no such terminal.
func (s *Store) DeactivatePayterTerminal(ctx context.Context, serialNumber string) (poolCode int, found bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, fmt.Errorf("store: deactivating Payter terminal %s: %w", serialNumber, err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `SELECT pool_code FROM payter_terminal WHERE serial_number = ?`, serialNumber).Scan(&poolCode)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: deactivating Payter terminal %s: %w", serialNumber, err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE payter_terminal SET is_active = 0, updated_at = CURRENT_TIMESTAMP
		WHERE serial_number = ? AND is_active = 1`, serialNumber); err != nil {
		return 0, false, fmt.Errorf("store: deactivating Payter terminal %s: %w", serialNumber, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM payter_terminal_station WHERE serial_number = ?`, serialNumber); err != nil {
		return 0, false, fmt.Errorf("store: deactivating Payter terminal %s: %w", serialNumber, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("store: deactivating Payter terminal %s: %w", serialNumber, err)
	}
	return poolCode, true, nil
}
