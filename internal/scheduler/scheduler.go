// Package scheduler runs jobs every day at fixed times of a time zone (Europe/Madrid for this service, whatever
// the container's own zone is). A job never overlaps itself: a run still going when its next time comes is
// followed by the next run only once it ends.
//
// A restart of the service at a job's time used to skip that day's run. Now each completed run is recorded (RunLog),
// and a job whose time passed less than CatchUpWindow before the service started runs once at startup, unless its
// last completed run is that one.
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// CatchUpWindow is how late a missed run is still made up at startup. A deploy restarts the service in seconds.
const CatchUpWindow = 10 * time.Minute

// Job is a daily job.
type Job struct {
	Name   string
	Hour   int
	Minute int
	Run    func(ctx context.Context)
}

// RunLog keeps the scheduled time of the last completed run of each job.
type RunLog interface {
	LastJobRun(ctx context.Context, job string) (slot time.Time, ok bool, err error)
	RecordJobRun(ctx context.Context, job string, slot time.Time) error
}

// Scheduler runs daily jobs.
type Scheduler struct {
	location *time.Location
	log      *slog.Logger
	runs     RunLog           // nil: no catch-up
	now      func() time.Time // The clock, replaced in tests
	jobs     []Job
}

// New creates a scheduler for the given time zone. runs records the completed runs, for the catch-up after a
// restart; nil disables it.
func New(location *time.Location, log *slog.Logger, runs RunLog) *Scheduler {
	return &Scheduler{location: location, log: log, runs: runs, now: time.Now}
}

// Daily adds a job run every day at hour:minute.
func (s *Scheduler) Daily(name string, hour, minute int, run func(ctx context.Context)) {
	s.jobs = append(s.jobs, Job{Name: name, Hour: hour, Minute: minute, Run: run})
}

// Run starts the jobs and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	done := make(chan struct{}, len(s.jobs))
	for _, job := range s.jobs {
		go func() {
			s.loop(ctx, job)
			done <- struct{}{}
		}()
	}
	for range s.jobs {
		<-done
	}
}

func (s *Scheduler) loop(ctx context.Context, job Job) {
	s.catchUp(ctx, job)
	for {
		next := NextRun(s.now(), job.Hour, job.Minute, s.location)
		s.log.Debug("Scheduler: next run", "job", job.Name, "at", next)
		timer := time.NewTimer(next.Sub(s.now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.run(ctx, job, next)
	}
}

// catchUp runs a job once when the service started less than CatchUpWindow after its time and its last completed
// run is an earlier one. A job without a recorded run is left alone: there is no telling whether it ran (the
// first start with this catch-up, or a job that never completed).
func (s *Scheduler) catchUp(ctx context.Context, job Job) {
	if s.runs == nil {
		return
	}
	now := s.now()
	slot := PreviousRun(now, job.Hour, job.Minute, s.location)
	if now.Sub(slot) > CatchUpWindow {
		return
	}
	last, ok, err := s.runs.LastJobRun(ctx, job.Name)
	if err != nil {
		s.log.Error("Scheduler: reading the last run failed, no catch-up", "job", job.Name, "error", err)
		return
	}
	if !ok || !last.Before(slot) {
		return
	}
	s.log.Warn("Scheduler: catching up a run missed by a restart", "job", job.Name, "scheduled", slot, "last_run", last)
	s.run(ctx, job, slot)
}

// run runs a job and, when it completed (it didn't panic and the service isn't stopping), records its slot.
func (s *Scheduler) run(ctx context.Context, job Job, slot time.Time) {
	if !s.execute(ctx, job) || ctx.Err() != nil || s.runs == nil {
		return
	}
	if err := s.runs.RecordJobRun(ctx, job.Name, slot); err != nil {
		s.log.Error("Scheduler: recording the run failed", "job", job.Name, "error", err)
	}
}

func (s *Scheduler) execute(ctx context.Context, job Job) (completed bool) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("Scheduler: job panicked", "job", job.Name, "panic", r)
			completed = false
		}
	}()
	started := time.Now()
	s.log.Info("Scheduler: job started", "job", job.Name)
	job.Run(ctx)
	s.log.Info("Scheduler: job finished", "job", job.Name, "took", time.Since(started).Round(time.Second))
	return true
}

// NextRun returns the next hour:minute in location strictly after now. Days with a DST change keep the wall-clock
// time (time.Date normalizes a time skipped by the change).
func NextRun(now time.Time, hour, minute int, location *time.Location) time.Time {
	local := now.In(location)
	next := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, location)
	if !next.After(local) {
		next = time.Date(local.Year(), local.Month(), local.Day()+1, hour, minute, 0, 0, location)
	}
	return next
}

// PreviousRun returns the last hour:minute in location at or before now.
func PreviousRun(now time.Time, hour, minute int, location *time.Location) time.Time {
	local := now.In(location)
	previous := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, location)
	if previous.After(local) {
		previous = time.Date(local.Year(), local.Month(), local.Day()-1, hour, minute, 0, 0, location)
	}
	return previous
}
