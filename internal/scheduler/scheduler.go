// Package scheduler runs jobs every day at fixed times of a time zone (Europe/Madrid for this service, whatever
// the container's own zone is). A job never overlaps itself: a run still going when its next time comes is
// followed by the next run only once it ends.
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// Job is a daily job.
type Job struct {
	Name   string
	Hour   int
	Minute int
	Run    func(ctx context.Context)
}

// Scheduler runs daily jobs.
type Scheduler struct {
	location *time.Location
	log      *slog.Logger
	jobs     []Job
}

// New creates a scheduler for the given time zone.
func New(location *time.Location, log *slog.Logger) *Scheduler {
	return &Scheduler{location: location, log: log}
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
	for {
		next := NextRun(time.Now(), job.Hour, job.Minute, s.location)
		s.log.Debug("Scheduler: next run", "job", job.Name, "at", next)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.run(ctx, job)
	}
}

func (s *Scheduler) run(ctx context.Context, job Job) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("Scheduler: job panicked", "job", job.Name, "panic", r)
		}
	}()
	started := time.Now()
	s.log.Info("Scheduler: job started", "job", job.Name)
	job.Run(ctx)
	s.log.Info("Scheduler: job finished", "job", job.Name, "took", time.Since(started).Round(time.Second))
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
