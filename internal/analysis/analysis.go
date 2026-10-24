// Package analysis turns the triggers of the TagoIO analyses (forms and buttons of the TagoIO dashboards, and the
// MQTT meter of each installation) into typed events for the CSMS, and applies their side effects: the RFID cards
// and rates are rewritten in the pool's bucket, the pool configuration cache is updated and the dashboard shows a
// validation toast.
package analysis

import (
	"context"
	"log/slog"

	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio/realtime"
)

// PoolResolver finds the pool of a TagoIO device.
type PoolResolver interface {
	PoolByDevice(ctx context.Context, deviceID string) (int, bool)
}

// Bucket writes a pool's bucket.
type Bucket interface {
	Insert(ctx context.Context, poolCode int, records ...tagoio.DataInput) error
	Delete(ctx context.Context, poolCode int, variable, group string, keepWeeks int) (int, error)
	Replace(ctx context.Context, poolCode int, variable, group string, record tagoio.DataInput) error
	Feedback(poolCode int, variable string, ok bool)
}

// PoolConfigs is the pool configuration cache.
type PoolConfigs interface {
	Update(poolCode int, change func(*pools.Config))
}

// Publisher publishes events for the CSMS.
type Publisher interface {
	Publish(events.Event)
}

// Service holds the analysis handlers.
type Service struct {
	pools     PoolResolver
	bucket    Bucket
	configs   PoolConfigs
	publisher Publisher
	log       *slog.Logger
	logScopes bool // Log the full scope of every trigger (dry run: to capture the payloads)
}

// New creates the analysis service.
func New(resolver PoolResolver, bucket Bucket, configs PoolConfigs, publisher Publisher, log *slog.Logger, logScopes bool) *Service {
	return &Service{pools: resolver, bucket: bucket, configs: configs, publisher: publisher, log: log, logScopes: logScopes}
}

// handlerFunc handles the scope of one trigger.
type handlerFunc func(s *Service, ctx context.Context, sc scope)

var handlers = map[config.Analysis]handlerFunc{
	config.AnalysisChangeAvailability:      (*Service).changeAvailability,
	config.AnalysisChangeMaxGridPower:      (*Service).changeMaxGridPower,
	config.AnalysisManageRFID:              (*Service).manageRFID,
	config.AnalysisChangeCPOInfo:           (*Service).changeCPOInfo,
	config.AnalysisChangeRateList:          (*Service).changeRateList,
	config.AnalysisChangeLoadBalancingMode: (*Service).changeLoadBalancingMode,
	config.AnalysisOCPPRequests:            (*Service).ocppRequest,
	config.AnalysisPowerConsumptionUpdate:  (*Service).powerConsumptionUpdate,
}

// Handler returns the realtime trigger handler of an analysis (nil for an unknown one).
func (s *Service) Handler(analysis config.Analysis) realtime.Handler {
	handle, ok := handlers[analysis]
	if !ok {
		return nil
	}
	return func(ctx context.Context, t realtime.Trigger) {
		sc := newScope(t.Data)
		attrs := []any{"analysis", analysis, "variables", sc.variables()}
		if s.logScopes {
			attrs = append(attrs, "scope", t.Data)
		}
		if analysis == config.AnalysisPowerConsumptionUpdate {
			s.log.Debug("Analysis: trigger received", attrs...) // Every few seconds per installation
		} else {
			s.log.Info("Analysis: trigger received", attrs...)
		}
		handle(s, ctx, sc)
	}
}

// poolOf resolves the pool of the trigger's device, logging when it is unknown.
func (s *Service) poolOf(ctx context.Context, sc scope, what string) (int, bool) {
	device := sc.device()
	if device == "" {
		s.log.Error("Analysis: trigger without device", "event", what)
		return 0, false
	}
	poolCode, ok := s.pools.PoolByDevice(ctx, device)
	if !ok {
		s.log.Error("Analysis: unknown pool for device", "event", what, "device_id", device)
	}
	return poolCode, ok
}
