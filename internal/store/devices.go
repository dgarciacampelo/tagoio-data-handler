// This file holds the TagoIO device registry: one MASTER device per charging pool, with its device token.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Device is a charging pool's TagoIO device.
type Device struct {
	PoolCode int
	DeviceID string
	Token    string
}

// Devices returns every registered device, ordered by pool code.
func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT pool_code, device_id, device_token FROM tagoio_device ORDER BY pool_code")
	if err != nil {
		return nil, fmt.Errorf("store: listing devices: %w", err)
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.PoolCode, &d.DeviceID, &d.Token); err != nil {
			return nil, fmt.Errorf("store: reading device: %w", err)
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// UpsertDevice inserts a device, or replaces the device of its pool.
func (s *Store) UpsertDevice(ctx context.Context, d Device) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tagoio_device (pool_code, device_id, device_token, is_modified)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(pool_code) DO UPDATE SET
			device_id = excluded.device_id, device_token = excluded.device_token, is_modified = 1
		WHERE device_id <> excluded.device_id OR device_token <> excluded.device_token`,
		d.PoolCode, d.DeviceID, d.Token)
	if err != nil {
		return fmt.Errorf("store: upserting device of pool %d: %w", d.PoolCode, err)
	}
	return nil
}

// MaxPoolCode returns the highest registered pool code (ok is false when there are no devices).
func (s *Store) MaxPoolCode(ctx context.Context) (code int, ok bool, err error) {
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT MAX(pool_code) FROM tagoio_device").Scan(&max); err != nil {
		return 0, false, fmt.Errorf("store: reading max pool code: %w", err)
	}
	return int(max.Int64), max.Valid, nil
}

// PoolCodeByDeviceID returns the pool of a device, or ErrNotFound.
func (s *Store) PoolCodeByDeviceID(ctx context.Context, deviceID string) (int, error) {
	var code int
	err := s.db.QueryRowContext(ctx, "SELECT pool_code FROM tagoio_device WHERE device_id = ?", deviceID).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("store: looking up pool of device %s: %w", deviceID, err)
	}
	return code, nil
}
