// This file holds the admin operations of the machine API (HTTP Basic): the cleanup of every pool's routine
// variables, an immediate database backup, and the deletion of one variable of a pool. The long ones run in the
// background and answer 202 at once; a job already running is not started twice.

package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

// CleanupAllPools starts the cleanup of every pool's routine variables (POST /v1/trigger-task/pool-variable-cleanup).
func (a *App) CleanupAllPools(w http.ResponseWriter, _ *http.Request) {
	a.startJob(w, "pool-variable-cleanup", a.Maintenance.CleanupAllPools)
}

// BackupNow starts a database backup to Telegram (POST /v1/trigger-task/backup-to-telegram).
func (a *App) BackupNow(w http.ResponseWriter, _ *http.Request) {
	a.startJob(w, "backup-to-telegram", func(ctx context.Context) { a.Maintenance.Backup(ctx, true) })
}

// DeletePoolVariable deletes every record of a variable of a pool
// (DELETE /v1/trigger-task/single-variable/{pool_code}/{variable}).
func (a *App) DeletePoolVariable(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	variable, ok := pathText(w, r, "variable")
	if !ok {
		return
	}
	removed, err := a.Bucket.Delete(r.Context(), poolCode, variable, "", 0)
	if err != nil {
		a.Log.Error("Variable deletion failed", "pool_code", poolCode, "variable", variable, "error", err)
		writeAPIError(w, http.StatusBadGateway, dto.ErrorCodeServiceUnavailable, "TagoIO deletion failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pool_code": poolCode, "variable": variable, "removed": removed})
}

func (a *App) startJob(w http.ResponseWriter, name string, run func(ctx context.Context)) {
	if !a.jobs.start(name, run) {
		writeAPIError(w, http.StatusConflict, dto.ErrorCodeConflict, name+" is already running")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"job": name, "status": "started"})
}

// jobRunner runs named background jobs, one run per name at a time, under the jobs context.
type jobRunner struct {
	ctx     context.Context
	log     *slog.Logger
	mu      sync.Mutex
	running map[string]bool
}

func newJobRunner(ctx context.Context, log *slog.Logger) *jobRunner {
	if ctx == nil {
		ctx = context.Background()
	}
	return &jobRunner{ctx: ctx, log: log, running: map[string]bool{}}
}

// start runs a job in the background; it reports false when the job is already running.
func (j *jobRunner) start(name string, run func(ctx context.Context)) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running[name] {
		return false
	}
	j.running[name] = true

	go func() {
		started := time.Now()
		defer func() {
			if r := recover(); r != nil {
				j.log.Error("Job panicked", "job", name, "panic", r)
			}
			j.mu.Lock()
			delete(j.running, name)
			j.mu.Unlock()
			j.log.Info("Job finished", "job", name, "took", time.Since(started).Round(time.Second))
		}()
		j.log.Info("Job started", "job", name)
		run(j.ctx)
	}()
	return true
}
