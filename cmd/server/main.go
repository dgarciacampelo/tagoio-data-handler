// Command server runs the TagoIO data handler: the bridge between the TagoIO platform (dashboards, analyses and
// device buckets of every charging pool) and the CSMS instances, plus the public station pages and the admin
// dashboards.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // Europe/Madrid even in a container without time zone data

	"github.com/dgarciacampelo/tagoio-handler/internal/analysis"
	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/handlers"
	"github.com/dgarciacampelo/tagoio-handler/internal/maintenance"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/provisioning"
	"github.com/dgarciacampelo/tagoio-handler/internal/scheduler"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio/realtime"
	"github.com/dgarciacampelo/tagoio-handler/internal/telegram"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

const (
	serviceName     = "TagoIO data handler"
	shutdownTimeout = 15 * time.Second
)

type application struct {
	handlers *handlers.App
	ui       *webui.Renderer
}

func main() {
	checkConfig := flag.Bool("check-config", false, "validate the configuration (environment) and exit")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()}))
	slog.SetDefault(logger)

	if *checkConfig {
		if _, err := config.Load(); err != nil {
			logger.Error("Invalid configuration", "error", err)
			os.Exit(1)
		}
		logger.Info("Configuration is valid")
		return
	}

	if err := run(logger); err != nil {
		logger.Error("Service stopped with an error", "error", err)
		os.Exit(1)
	}
}

// logLevel reads LOG_LEVEL (debug, info, warn, error); info by default.
func logLevel() slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(os.Getenv("LOG_LEVEL"))); err != nil {
		return slog.LevelInfo
	}
	return level
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	logger.Info("Service build", "git_commit", os.Getenv("GIT_COMMIT"), "build_time", os.Getenv("BUILD_TIME"),
		"env", cfg.AppEnv, "dry_run", cfg.DryRun)
	if cfg.DryRun {
		logger.Warn("DRY RUN: events are logged instead of delivered; TagoIO writes and Telegram messages are not sent")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// region Wiring

	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	tago := tagoio.NewClient(cfg.TagoAPIURL, cfg.TagoAccountToken, cfg.DryRun, logger)
	notifier := telegram.New(cfg.TelegramBotToken, cfg.TelegramNoticesChatID, cfg.TelegramBackupsChatID, serviceName, cfg.DryRun, logger)
	broker := events.NewBroker(logger, cfg.DryRun)
	registry := devices.NewRegistry(db, tago, cfg.TagoDevicePrefix, logger)

	// The bucket needs the known connectors (to trim their variables) and the charge points write the bucket: the
	// bucket gets the connector list through an interface, set once both exist.
	connectors := &connectorList{}
	buckets := bucket.New(tago, registry, connectors, logger)
	stations := chargepoints.New(db, buckets, logger)
	connectors.set(stations)

	poolConfigs := pools.NewCache(buckets, logger)
	analyses := analysis.New(registry, buckets, poolConfigs, broker, logger, cfg.DryRun)
	provisioner := provisioning.New(tago, registry, db, buckets, logger, cfg.Location)
	upkeep := maintenance.New(db, tago, registry, buckets, notifier, logger, cfg.Location, cfg.DatabasePath, cfg.BackupsToKeep)

	ui, err := newRenderer(cfg)
	if err != nil {
		logger.Error("Pages disabled: frontend build unavailable", "error", err)
		ui = webui.Disabled(err)
	}

	// endregion

	// region Startup state

	if err := registry.Load(ctx); err != nil {
		logger.Error("Devices: initial load failed (the registry fills itself on demand)", "error", err)
	}
	if err := stations.Load(ctx); err != nil {
		return err
	}

	// endregion

	// region Background workers

	var workers sync.WaitGroup
	startWorker := func(run func(context.Context)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			run(ctx)
		}()
	}

	startWorker(stations.Run)
	startWorker(broker.RunSummaries)
	startWorker(func(ctx context.Context) { poolConfigs.LoadAll(ctx, registry.PoolCodes()) })

	for analysisName, token := range cfg.AnalysisTokens {
		worker := &realtime.Worker{
			Name: string(analysisName), URL: cfg.TagoSSEURL, Token: token, Handle: analyses.Handler(analysisName), Log: logger,
			Rejected: func(name string, err error) {
				notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := notifier.Notify(notifyCtx, "TagoIO rejected the token of analysis "+name+"."); err != nil {
					logger.Error("Telegram notice failed", "error", err)
				}
			},
		}
		startWorker(worker.Run)
	}
	logger.Info("TagoIO realtime: analysis connections started", "analyses", len(cfg.AnalysisTokens))

	jobs := scheduler.New(cfg.Location, logger)
	jobs.Daily("telemetry-purge", 4, 0, upkeep.PurgeTelemetry)
	jobs.Daily("data-amount-check-morning", 8, 0, upkeep.CheckDataAmount)
	jobs.Daily("data-amount-check-evening", 20, 0, upkeep.CheckDataAmount)
	jobs.Daily("monthly-backup", 20, 45, upkeep.MonthlyBackup)
	jobs.Daily("daily-backup", 21, 15, func(ctx context.Context) { upkeep.Backup(ctx, false) })
	startWorker(jobs.Run)
	logger.Info("Maintenance scheduled", "summary", maintenance.Describe())

	// endregion

	// region HTTP server

	app := &application{
		ui: ui,
		handlers: handlers.New(handlers.Deps{
			Log: logger, Config: cfg, Store: db, Broker: broker, ChargePoints: stations, Pools: poolConfigs,
			Devices: registry, Bucket: buckets, Provisioning: provisioner, Maintenance: upkeep, UI: ui, Jobs: ctx,
		}),
	}

	// Request contexts derive from ctx, so the long-lived event streams end at shutdown instead of holding it.
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		// No WriteTimeout: it would cut the event streams.
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
			workers.Wait()
			return fmt.Errorf("HTTP server: %w", err)
		}
	case <-ctx.Done():
		logger.Info("Shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP server shutdown incomplete", "error", err)
	}
	workers.Wait()
	logger.Info("Service stopped")
	return nil

	// endregion
}

// newRenderer creates the page renderer: the Vite dev server in development, the build output in production.
func newRenderer(cfg *config.Config) (*webui.Renderer, error) {
	if cfg.IsDevelopment() {
		return webui.New(webui.Config{DevServerURL: cfg.ViteDevURL})
	}
	return webui.New(webui.Config{DistDir: cfg.UIDistDir})
}

// connectorList hands the charge points' connector list to the bucket, which is created before them.
type connectorList struct {
	mu       sync.RWMutex
	stations *chargepoints.Service
}

func (c *connectorList) set(s *chargepoints.Service) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stations = s
}

func (c *connectorList) PoolConnectors(poolCode int) []bucket.Connector {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.stations == nil {
		return nil
	}
	return c.stations.PoolConnectors(poolCode)
}
