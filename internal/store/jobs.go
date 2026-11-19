// This file holds the last completed run of each scheduled job, so the scheduler can catch up a run that a restart
// of the service made it miss, and only that one.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// migrateJobRuns adds the last completed run of each scheduled job.
func migrateJobRuns(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE scheduled_job_run(
		job TEXT NOT NULL PRIMARY KEY,
		slot TEXT NOT NULL,
		finished_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	return err
}

// LastJobRun returns the scheduled time of the last completed run of a job; ok is false when it never completed
// one (or not since this table exists).
func (s *Store) LastJobRun(ctx context.Context, job string) (slot time.Time, ok bool, err error) {
	var text string
	err = s.db.QueryRowContext(ctx, "SELECT slot FROM scheduled_job_run WHERE job = ?", job).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: reading the last run of %s: %w", job, err)
	}
	slot, err = time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: last run of %s: %w", job, err)
	}
	return slot, true, nil
}

// RecordJobRun records that the run of a job scheduled at slot completed.
func (s *Store) RecordJobRun(ctx context.Context, job string, slot time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO scheduled_job_run (job, slot) VALUES (?, ?)
		ON CONFLICT (job) DO UPDATE SET slot = excluded.slot, finished_at = CURRENT_TIMESTAMP`,
		job, slot.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store: recording the run of %s: %w", job, err)
	}
	return nil
}
