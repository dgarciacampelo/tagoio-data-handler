// This file holds the copy of the database an admin downloads to inspect the production data locally.

package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// ExportCopy writes a consistent copy of the database to dest, which must not exist, for an admin to inspect. It has
// every table and row of the live database, except the TagoIO device tokens, which are blanked. Unlike Snapshot, it
// leaves the backup flags alone, so a download never delays or skips a backup.
//
// VACUUM INTO reads the database in one transaction, so the copy holds the writes still in the WAL file too, which
// copying the database file would miss.
func (s *Store) ExportCopy(ctx context.Context, dest string) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("store: writing the export copy: %w", err)
	}
	if err := redactCopy(ctx, dest); err != nil {
		_ = os.Remove(dest)
		return err
	}
	return nil
}

// redactCopy blanks the secrets of an export copy. A device token gives write access to the pool's TagoIO device.
// secure_delete overwrites the old values instead of leaving them in free space, and the final VACUUM rebuilds the
// file. The copy is switched to a rollback journal, so it is a single, self-contained file.
func redactCopy(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(DELETE)&_pragma=secure_delete(1)")
	if err != nil {
		return fmt.Errorf("store: opening the export copy: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "UPDATE tagoio_device SET device_token = ''"); err != nil {
		return fmt.Errorf("store: blanking the device tokens of the export copy: %w", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("store: compacting the export copy: %w", err)
	}
	return db.Close()
}
