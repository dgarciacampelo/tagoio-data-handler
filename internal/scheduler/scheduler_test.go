package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestNextRun(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		now  time.Time
		want time.Time
	}{
		"later today": {
			now:  time.Date(2026, 10, 22, 10, 0, 0, 0, madrid),
			want: time.Date(2026, 10, 22, 21, 15, 0, 0, madrid),
		},
		"exactly now runs tomorrow": {
			now:  time.Date(2026, 10, 22, 21, 15, 0, 0, madrid),
			want: time.Date(2026, 10, 23, 21, 15, 0, 0, madrid),
		},
		"from UTC across midnight in Madrid": {
			now:  time.Date(2026, 10, 22, 22, 30, 0, 0, time.UTC), // 00:30 on the 23rd in Madrid
			want: time.Date(2026, 10, 23, 21, 15, 0, 0, madrid),
		},
		"over the DST change": {
			now:  time.Date(2026, 10, 24, 22, 0, 0, 0, madrid), // Clocks go back on the 25th
			want: time.Date(2026, 10, 25, 21, 15, 0, 0, madrid),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := NextRun(tt.now, 21, 15, madrid)
			if !got.Equal(tt.want) {
				t.Fatalf("NextRun = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPreviousRun(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		now  time.Time
		want time.Time
	}{
		"earlier today":          {now: time.Date(2026, 11, 19, 21, 20, 0, 0, madrid), want: time.Date(2026, 11, 19, 21, 15, 0, 0, madrid)},
		"exactly now is today":   {now: time.Date(2026, 11, 19, 21, 15, 0, 0, madrid), want: time.Date(2026, 11, 19, 21, 15, 0, 0, madrid)},
		"not yet today":          {now: time.Date(2026, 11, 19, 21, 14, 0, 0, madrid), want: time.Date(2026, 11, 18, 21, 15, 0, 0, madrid)},
		"from UTC, across a day": {now: time.Date(2026, 11, 19, 23, 30, 0, 0, time.UTC), want: time.Date(2026, 11, 19, 21, 15, 0, 0, madrid)},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := PreviousRun(tt.now, 21, 15, madrid); !got.Equal(tt.want) {
				t.Fatalf("PreviousRun = %v; want %v", got, tt.want)
			}
		})
	}
}

// fakeRuns is the run log, in memory.
type fakeRuns struct {
	mu   sync.Mutex
	last map[string]time.Time
	err  error
}

func (f *fakeRuns) LastJobRun(_ context.Context, job string) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	slot, ok := f.last[job]
	return slot, ok, f.err
}

func (f *fakeRuns) RecordJobRun(_ context.Context, job string, slot time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[job] = slot
	return nil
}

func (f *fakeRuns) get(job string) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[job]
}

func TestCatchUpAfterARestart(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 11, 19, 21, 15, 0, 0, madrid)
	yesterday := slot.AddDate(0, 0, -1)
	tests := map[string]struct {
		started   time.Time
		lastRun   *time.Time // nil: no run recorded
		wantRun   bool
		wantSlot  time.Time // Recorded after the test
		readError error
	}{
		"restarted at the job's minute": {started: slot.Add(20 * time.Second), lastRun: &yesterday, wantRun: true, wantSlot: slot},
		"restarted within the window":   {started: slot.Add(CatchUpWindow), lastRun: &yesterday, wantRun: true, wantSlot: slot},
		"restarted after the window":    {started: slot.Add(CatchUpWindow + time.Second), lastRun: &yesterday, wantSlot: yesterday},
		"restarted just after it ran":   {started: slot.Add(2 * time.Minute), lastRun: &slot, wantSlot: slot},
		"restarted before its time":     {started: slot.Add(-time.Minute), lastRun: &yesterday, wantSlot: yesterday},
		"never recorded (first start)":  {started: slot.Add(time.Minute)},
		"run log unreadable":            {started: slot.Add(time.Minute), lastRun: &yesterday, wantSlot: yesterday, readError: errors.New("disk")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runs := &fakeRuns{last: map[string]time.Time{}, err: tt.readError}
			if tt.lastRun != nil {
				runs.last["daily-backup"] = *tt.lastRun
			}
			s := New(madrid, slog.New(slog.NewTextHandler(io.Discard, nil)), runs)
			s.now = func() time.Time { return tt.started }
			ran := 0
			s.catchUp(context.Background(), Job{Name: "daily-backup", Hour: 21, Minute: 15, Run: func(context.Context) { ran++ }})

			if (ran == 1) != tt.wantRun || ran > 1 {
				t.Fatalf("job ran %d times; want run=%v", ran, tt.wantRun)
			}
			if got := runs.get("daily-backup"); !got.Equal(tt.wantSlot) {
				t.Fatalf("recorded run = %v; want %v", got, tt.wantSlot)
			}
		})
	}
}

func TestARunThatDidntCompleteIsNotRecorded(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 11, 19, 21, 15, 0, 0, madrid)
	yesterday := slot.AddDate(0, 0, -1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	runs := &fakeRuns{last: map[string]time.Time{"daily-backup": yesterday}}
	s := New(madrid, log, runs)
	s.run(context.Background(), Job{Name: "daily-backup", Run: func(context.Context) { panic("boom") }}, slot)
	if got := runs.get("daily-backup"); !got.Equal(yesterday) {
		t.Fatalf("a panicked run was recorded: %v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.run(ctx, Job{Name: "daily-backup", Run: func(context.Context) { cancel() }}, slot) // The service stops meanwhile
	if got := runs.get("daily-backup"); !got.Equal(yesterday) {
		t.Fatalf("a run cut by the shutdown was recorded: %v", got)
	}
}

func TestRunCatchesUpThenWaitsForTheNextTime(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 11, 19, 21, 15, 0, 0, madrid)
	runs := &fakeRuns{last: map[string]time.Time{"daily-backup": slot.AddDate(0, 0, -1), "purge": slot.AddDate(0, 0, -1)}}
	s := New(madrid, slog.New(slog.NewTextHandler(io.Discard, nil)), runs)
	s.now = func() time.Time { return slot.Add(30 * time.Second) }

	ran := make(chan string, 4)
	s.Daily("daily-backup", 21, 15, func(context.Context) { ran <- "daily-backup" })
	s.Daily("purge", 4, 0, func(context.Context) { ran <- "purge" }) // Its time passed hours ago
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(stopped)
	}()

	select {
	case job := <-ran:
		if job != "daily-backup" {
			t.Fatalf("caught up %s; want daily-backup", job)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the missed run wasn't caught up")
	}
	select {
	case job := <-ran:
		t.Fatalf("%s ran again before its next time", job)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	<-stopped
	if got := runs.get("daily-backup"); !got.Equal(slot) {
		t.Fatalf("recorded run = %v; want %v", got, slot)
	}
}
