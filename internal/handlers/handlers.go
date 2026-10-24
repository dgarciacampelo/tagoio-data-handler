// Package handlers holds the HTTP handlers of the service: the machine endpoints of the CSMS and the notification
// service (HTTP Basic), the event stream, the public station page with its payment requests, and the admin pages
// (eMSP dashboard, audits, login) with their JSON APIs.
package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/maintenance"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/provisioning"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

// Deps are the services the handlers use.
type Deps struct {
	Log          *slog.Logger
	Config       *config.Config
	Store        *store.Store
	Broker       *events.Broker
	ChargePoints *chargepoints.Service
	Pools        *pools.Cache
	Devices      *devices.Registry
	Bucket       *bucket.Service
	Provisioning *provisioning.Service
	Maintenance  *maintenance.Service
	UI           *webui.Renderer

	// Jobs is the context of the background jobs the handlers start; it ends at shutdown.
	Jobs context.Context
}

// App holds the handlers.
type App struct {
	Deps

	jobs        *jobRunner
	loginLimit  *windowLimiter
	chargeLimit *windowLimiter
	pollLimit   *windowLimiter
}

// New creates the handlers.
func New(deps Deps) *App {
	return &App{
		Deps:        deps,
		jobs:        newJobRunner(deps.Jobs, deps.Log),
		loginLimit:  newWindowLimiter(10, 15*time.Minute),
		chargeLimit: newWindowLimiter(10, time.Minute),
		pollLimit:   newWindowLimiter(120, time.Minute), // A page polls every 5 s: 12 per minute and connector
	}
}
