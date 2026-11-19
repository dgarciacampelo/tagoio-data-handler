package store

import (
	"context"
	"testing"
	"time"
)

func TestJobRuns(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.LastJobRun(ctx, "daily-backup"); err != nil || ok {
		t.Fatalf("LastJobRun of a job that never ran = %v, %v; want none", ok, err)
	}

	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 11, 18, 21, 15, 0, 0, madrid)
	second := first.AddDate(0, 0, 1)
	for _, slot := range []time.Time{first, second} {
		if err := s.RecordJobRun(ctx, "daily-backup", slot); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordJobRun(ctx, "telemetry-purge", first); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.LastJobRun(ctx, "daily-backup")
	if err != nil || !ok || !got.Equal(second) {
		t.Fatalf("LastJobRun = %v, %v, %v; want %v", got, ok, err, second)
	}
	if got, _, _ := s.LastJobRun(ctx, "telemetry-purge"); !got.Equal(first) {
		t.Fatalf("LastJobRun of another job = %v; want %v", got, first)
	}
}
