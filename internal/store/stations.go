// This file holds the station profiles (number of connectors) and the last known status of every connector.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ConnectorStatus is the last known OCPP status of a connector.
type ConnectorStatus struct {
	PoolCode    int
	StationName string
	ConnectorID int
	Status      string
}

// StationConnectorCount returns the number of connectors of a station (ok is false when the station is unknown).
func (s *Store) StationConnectorCount(ctx context.Context, stationName string) (noc int, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT noc FROM station_config WHERE station_name = ?", stationName).Scan(&noc)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: reading connectors of station %s: %w", stationName, err)
	}
	return noc, true, nil
}

// RegisterConnector records a station the first time one of its connectors reports, and raises its number of
// connectors when a higher connector ID shows up (connector 0, the station itself, never counts).
func (s *Store) RegisterConnector(ctx context.Context, poolCode int, stationName string, connectorID int) error {
	noc := max(connectorID, 1)
	_, err := s.db.ExecContext(ctx, `INSERT INTO station_config (station_name, pool_code, noc, is_modified)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(station_name) DO UPDATE SET noc = excluded.noc, is_modified = 1
		WHERE excluded.noc > station_config.noc`,
		stationName, poolCode, noc)
	if err != nil {
		return fmt.Errorf("store: registering station %d/%s: %w", poolCode, stationName, err)
	}
	return nil
}

// DeleteStation removes a station profile and its connector statuses; it reports whether the profile existed.
func (s *Store) DeleteStation(ctx context.Context, poolCode int, stationName string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: deleting station %d/%s: %w", poolCode, stationName, err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, "DELETE FROM station_config WHERE pool_code = ? AND station_name = ?", poolCode, stationName)
	if err != nil {
		return false, fmt.Errorf("store: deleting station %d/%s: %w", poolCode, stationName, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM connector_status WHERE pool_code = ? AND station_name = ?", poolCode, stationName); err != nil {
		return false, fmt.Errorf("store: deleting connector statuses of %d/%s: %w", poolCode, stationName, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: deleting station %d/%s: %w", poolCode, stationName, err)
	}
	deleted, _ := result.RowsAffected()
	return deleted > 0, nil
}

// UpsertConnectorStatus stores the last known status of a connector.
func (s *Store) UpsertConnectorStatus(ctx context.Context, c ConnectorStatus) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO connector_status (pool_code, station_name, connector_id, charge_point_status, is_modified)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT(pool_code, station_name, connector_id)
		DO UPDATE SET charge_point_status = excluded.charge_point_status, is_modified = 1`,
		c.PoolCode, c.StationName, c.ConnectorID, c.Status)
	if err != nil {
		return fmt.Errorf("store: upserting status of %d/%s/%d: %w", c.PoolCode, c.StationName, c.ConnectorID, err)
	}
	return nil
}

// ConnectorStatuses returns every stored connector status, to rebuild the in-memory state at startup.
func (s *Store) ConnectorStatuses(ctx context.Context) ([]ConnectorStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pool_code, station_name, connector_id, charge_point_status
		FROM connector_status ORDER BY pool_code, station_name, connector_id`)
	if err != nil {
		return nil, fmt.Errorf("store: listing connector statuses: %w", err)
	}
	defer rows.Close()

	var statuses []ConnectorStatus
	for rows.Next() {
		var c ConnectorStatus
		if err := rows.Scan(&c.PoolCode, &c.StationName, &c.ConnectorID, &c.Status); err != nil {
			return nil, fmt.Errorf("store: reading connector status: %w", err)
		}
		statuses = append(statuses, c)
	}
	return statuses, rows.Err()
}
