// This file holds the state service: it applies the CSMS updates in memory and in the store at once, and queues
// the TagoIO dashboard writes, so the CSMS never waits for TagoIO.

package chargepoints

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/jsonx"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

const (
	quarantineLength = 30 * time.Minute // Dashboard updates of a flooding station are held back this long
	pushQueueSize    = 2048             // TagoIO writes waiting to be sent
)

// Bucket writes a pool's bucket.
type Bucket interface {
	Insert(ctx context.Context, poolCode int, records ...tagoio.DataInput) error
}

// stationKey identifies a station.
type stationKey struct {
	poolCode    int
	stationName string
}

// connector is the live state of a connector.
type connector struct {
	status         Status
	quarantined    bool
	quarantineEnd  time.Time
	lastUpdate     time.Time
	omittedUpdates int
}

// push is a queued TagoIO write.
type push struct {
	poolCode int
	records  []tagoio.DataInput
}

// Service keeps the stations' state.
type Service struct {
	store  *store.Store
	bucket Bucket
	log    *slog.Logger
	now    func() time.Time

	mu         sync.RWMutex
	connectors map[ConnectorKey]*connector
	sessions   map[ConnectorKey]SessionUpdate // Sessions in progress
	translated map[stationKey]map[int]string  // Dashboard status text of each connector of a station

	pushes chan push
}

// New creates the service; call Load, then Run.
func New(s *store.Store, b Bucket, log *slog.Logger) *Service {
	return &Service{
		store: s, bucket: b, log: log, now: time.Now,
		connectors: map[ConnectorKey]*connector{},
		sessions:   map[ConnectorKey]SessionUpdate{},
		translated: map[stationKey]map[int]string{},
		pushes:     make(chan push, pushQueueSize),
	}
}

// Load restores the connector statuses saved before the last restart.
func (s *Service) Load(ctx context.Context) error {
	statuses, err := s.store.ConnectorStatuses(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range statuses {
		status, ok := ParseStatus(row.Status)
		if !ok {
			status = StatusUnavailable
		}
		key := ConnectorKey{PoolCode: row.PoolCode, StationName: row.StationName, ConnectorID: row.ConnectorID}
		s.connectors[key] = &connector{status: status}
		s.translatedOf(key)[key.ConnectorID] = translateStatus(string(status), ConnectionOnline)
	}
	s.log.Info("Charge points: statuses restored", "connectors", len(statuses))
	return nil
}

// Run sends the queued TagoIO writes, one at a time and in order, until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			if pending := len(s.pushes); pending > 0 {
				s.log.Warn("Charge points: dashboard writes not sent at shutdown", "pending", pending)
			}
			return
		case p := <-s.pushes:
			pushCtx, cancel := context.WithTimeout(ctx, time.Minute)
			if err := s.bucket.Insert(pushCtx, p.poolCode, p.records...); err != nil {
				s.log.Error("Charge points: dashboard write failed", "pool_code", p.poolCode, "variable", p.records[0].Variable, "error", err)
			}
			cancel()
		}
	}
}

// enqueue queues TagoIO writes for a pool, dropping them (with an error) when the queue is full.
func (s *Service) enqueue(poolCode int, records ...tagoio.DataInput) {
	select {
	case s.pushes <- push{poolCode: poolCode, records: records}:
	default:
		s.log.Error("Charge points: dashboard write queue full, write dropped", "pool_code", poolCode, "variable", records[0].Variable)
	}
}

// translatedOf returns the status texts of a connector's station; the caller holds the write lock.
func (s *Service) translatedOf(key ConnectorKey) map[int]string {
	sk := stationKey{key.PoolCode, key.StationName}
	texts, ok := s.translated[sk]
	if !ok {
		texts = map[int]string{}
		s.translated[sk] = texts
	}
	return texts
}

// region Status updates

// UpdateStatus applies a connector status change. A station going offline marks its connectors unavailable (all of
// them when connector 0, the station itself, reports it). A station reporting an under-voltage fault is put in
// quarantine: its status is stored, but the dashboards are not updated until it recovers or the quarantine ends.
func (s *Service) UpdateStatus(ctx context.Context, u StatusUpdate) (ConnectorState, error) {
	status, ok := ParseStatus(u.ChargePointStatus)
	if !ok && u.ConnectionStatus != ConnectionOffline {
		return ConnectorState{}, fmt.Errorf("unknown charge point status %q", u.ChargePointStatus)
	}
	if u.ConnectionStatus == ConnectionOffline {
		status = StatusUnavailable
		u.ChargePointStatus = string(status)

		if u.ConnectorID == 0 {
			for _, childID := range s.stationConnectors(u.PoolCode, u.StationName) {
				child := u
				child.ConnectorID = jsonx.Int(childID)
				if _, err := s.UpdateStatus(ctx, child); err != nil {
					return ConnectorState{}, err
				}
			}
		}
	}

	key := ConnectorKey{PoolCode: u.PoolCode, StationName: u.StationName, ConnectorID: int(u.ConnectorID)}
	if err := s.store.RegisterConnector(ctx, key.PoolCode, key.StationName, key.ConnectorID); err != nil {
		return ConnectorState{}, err
	}
	if err := s.store.UpsertConnectorStatus(ctx, store.ConnectorStatus{
		PoolCode: key.PoolCode, StationName: key.StationName, ConnectorID: key.ConnectorID, Status: string(status),
	}); err != nil {
		return ConnectorState{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	c, known := s.connectors[key]
	if !known {
		c = &connector{}
		s.connectors[key] = c
	}
	newQuarantine := s.checkQuarantine(c, u, status)
	if c.quarantined && !newQuarantine {
		c.omittedUpdates++
	}
	c.status = status
	c.lastUpdate = s.now()

	// The quarantine holds back the dashboard writes, except the one that starts it (it shows the fault).
	if !c.quarantined || newQuarantine {
		s.pushStatus(key, u)
	}
	return s.stateOf(key, c), nil
}

// checkQuarantine updates the quarantine of a connector for an incoming status; it reports whether a new
// quarantine starts. The caller holds the write lock.
func (s *Service) checkQuarantine(c *connector, u StatusUpdate, status Status) bool {
	now := s.now()
	switch {
	case status == StatusPreparing: // A vehicle was plugged in
		c.quarantined = false
	case u.ConnectionStatus == ConnectionOffline:
		c.quarantined = false
	case c.quarantined && status != StatusFaulted && !c.quarantineEnd.IsZero() && now.After(c.quarantineEnd):
		c.quarantined = false
	case !c.quarantined && status == StatusFaulted && u.ErrorCode == "UnderVoltage":
		c.quarantined = true
		c.quarantineEnd = now.Add(quarantineLength)
		return true
	}
	if !c.quarantined {
		c.omittedUpdates = 0
	}
	return false
}

// pushStatus records the dashboard text of a connector and queues the dashboard writes: the station row of the
// management dashboard (all its connectors) and, for a station with a public dashboard, the connector's own
// variable. The caller holds the write lock.
func (s *Service) pushStatus(key ConnectorKey, u StatusUpdate) {
	texts := s.translatedOf(key)
	texts[key.ConnectorID] = translateStatus(u.ChargePointStatus, u.ConnectionStatus)

	metadata := map[string]any{"connection_state": u.ConnectionStatus}
	for cid, text := range texts {
		metadata["state_"+strconv.Itoa(cid)] = text
	}
	records := []tagoio.DataInput{{Variable: "state", Value: key.StationName, Group: key.StationName, Metadata: metadata}}
	if u.HasPublicDashboard {
		records = append(records, tagoio.DataInput{
			Variable: fmt.Sprintf("state_%s_%d", key.StationName, key.ConnectorID),
			Value:    texts[key.ConnectorID],
			Group:    key.StationName,
		})
	}
	s.enqueue(key.PoolCode, records...)
}

// stationConnectors returns the known connectors of a station, without connector 0.
func (s *Service) stationConnectors(poolCode int, stationName string) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []int
	for key := range s.connectors {
		if key.PoolCode == poolCode && key.StationName == stationName && key.ConnectorID != 0 {
			ids = append(ids, key.ConnectorID)
		}
	}
	slices.Sort(ids)
	return ids
}

func (s *Service) stateOf(key ConnectorKey, c *connector) ConnectorState {
	state := ConnectorState{
		PoolCode: key.PoolCode, StationName: key.StationName, ConnectorID: key.ConnectorID,
		Status: c.status, Quarantined: c.quarantined, OmittedUpdates: c.omittedUpdates,
	}
	if !c.quarantineEnd.IsZero() && c.quarantined {
		end := c.quarantineEnd
		state.QuarantineEnd = &end
	}
	if !c.lastUpdate.IsZero() {
		last := c.lastUpdate
		state.LastUpdate = &last
	}
	return state
}

// endregion

// region Session updates

// UpdateSession applies a charging session update: it stores the meter value (for the audits), keeps the session
// for the public page, stores it in the history when it ends, and queues the dashboard writes.
func (s *Service) UpdateSession(ctx context.Context, u SessionUpdate) error {
	u = u.withDefaults()
	key := u.Key()

	if u.Step == StepInProgress || u.Step == StepCompleted {
		err := s.store.InsertTelemetry(ctx, store.TelemetryRecord{
			PoolCode: key.PoolCode, StationName: key.StationName, ConnectorID: key.ConnectorID,
			TransactionID: int64(u.TransactionID), Timestamp: u.LastMeterTS, MeterValue: int64(u.LastMeterValue),
			Power: int64(u.Power), Cost: float64(u.Cost), CurrentTariffBand: u.CurrentTariffBand,
			EnergyOffPeak: int64(u.EnergyOffPeak), EnergyFlat: int64(u.EnergyFlat), EnergyPeak: int64(u.EnergyPeak),
		})
		if err != nil {
			return err
		}
	}

	if u.Ended() {
		s.mu.Lock()
		delete(s.sessions, key)
		s.mu.Unlock()

		inserted, err := s.store.InsertSession(ctx, store.SessionRecord{
			PoolCode: key.PoolCode, StationName: key.StationName, ConnectorID: key.ConnectorID,
			TransactionID: int64(u.TransactionID), CardAlias: u.CardAlias, StartDate: u.StartDate, TimeBand: deref(u.TimeBand),
			StartMeterValue: int64(u.StartMeterValue), LastMeterValue: int64(u.LastMeterValue), Cost: float64(u.Cost),
			RateOffPeak: float64(u.RateOffPeak), RateFlat: float64(u.RateFlat), RatePeak: float64(u.RatePeak),
			EnergyOffPeak: int64(u.EnergyOffPeak), EnergyFlat: int64(u.EnergyFlat), EnergyPeak: int64(u.EnergyPeak),
		})
		if err != nil {
			return err
		}
		if !inserted {
			s.log.Warn("Charge points: repeated end of session ignored", "transaction_id", int64(u.TransactionID))
			return nil
		}
	} else {
		if err := s.inferCharging(ctx, key); err != nil {
			return err
		}
		s.mu.Lock()
		s.sessions[key] = u
		s.mu.Unlock()
	}

	s.pushSession(u)
	return nil
}

// inferCharging marks a connector as charging when it reports meter values while showing another status (a
// status update was lost, or the service restarted during the session).
func (s *Service) inferCharging(ctx context.Context, key ConnectorKey) error {
	s.mu.Lock()
	c, ok := s.connectors[key]
	stale := ok && !c.status.InSession()
	if stale {
		c.status = StatusCharging
	}
	s.mu.Unlock()
	if !stale {
		return nil
	}
	s.log.Info("Charge points: charging inferred from session telemetry", "pool_code", key.PoolCode, "station", key.StationName, "connector", key.ConnectorID)
	return s.store.UpsertConnectorStatus(ctx, store.ConnectorStatus{
		PoolCode: key.PoolCode, StationName: key.StationName, ConnectorID: key.ConnectorID, Status: string(StatusCharging),
	})
}

// pushSession queues the dashboard writes of a session update: the history entry of an ended (paid) session, the
// active session row of the management dashboard and the public dashboard values.
func (s *Service) pushSession(u SessionUpdate) {
	key := u.Key()
	ended := u.Step == StepCompleted

	if ended && float64(u.Cost) != 0 {
		// The group is the transaction, so the dashboard table shows one row per session.
		s.enqueue(key.PoolCode, tagoio.DataInput{
			Variable: "charging_session_data", Value: int64(u.TransactionID), Group: strconv.FormatInt(int64(u.TransactionID), 10),
			Metadata: map[string]any{
				"card_alias": u.CardAlias, "card_code": u.CardCode, "display_id": u.DisplayID, "start_date": u.StartDate,
				"step": u.Step, "energy": float64(u.Energy), "energy_unit": u.EnergyUnit, "cost": float64(u.Cost),
				"cost_unit": u.CostUnit, "stop_motive": u.StopMotive, "time_band": u.TimeBand,
			},
		})
	}

	connectorLabel := fmt.Sprintf("%s_[%d]", key.StationName, key.ConnectorID)
	s.enqueue(key.PoolCode, tagoio.DataInput{
		Variable: "active_cs_data", Value: connectorLabel, Group: connectorLabel,
		Metadata: map[string]any{
			"card_alias": u.CardAlias, "display_id": u.DisplayID,
			"meter_values": fmt.Sprintf("[%d, %d]", int64(u.StartMeterValue), int64(u.LastMeterValue)),
			"start_date":   u.StartDate, "start_time": u.StartTime, "step": u.Step,
			"power": int64(u.Power), "energy": float64(u.Energy), "time": u.Time,
		},
	})

	if !ended {
		s.resyncCharging(key, u.HasPublicDashboard)
	}
	if !u.HasPublicDashboard {
		return
	}
	energy, cost, duration := float64(u.Energy), float64(u.Cost), u.Time
	if ended { // The public dashboard is reset for the next driver
		energy, cost, duration = 0, 0, "0 min"
	}
	values := map[string]string{
		"energy": pyFloat(energy) + " " + u.EnergyUnit,
		"cost":   pyFloat(cost) + " " + u.CostUnit,
		"time":   duration,
	}
	for _, prefix := range []string{"energy", "cost", "time"} {
		s.enqueue(key.PoolCode, tagoio.DataInput{
			Variable: fmt.Sprintf("%s_%s_%d", prefix, key.StationName, key.ConnectorID), Value: values[prefix], Group: connectorLabel,
		})
	}
}

// resyncCharging rewrites the dashboard status of a connector that sends session data while its dashboard shows
// a status outside a session (after a restart, or a lost update).
func (s *Service) resyncCharging(key ConnectorKey, hasPublicDashboard bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.translatedOf(key)[key.ConnectorID] {
	case spanishStatus["Charging"], spanishStatus["SuspendedEV"], spanishStatus["SuspendedEVSE"]:
		return
	}
	s.log.Info("Charge points: dashboard status resynchronized to charging", "pool_code", key.PoolCode, "station", key.StationName, "connector", key.ConnectorID)
	s.pushStatus(key, StatusUpdate{
		ConnectorID: jsonx.Int(key.ConnectorID), ConnectionStatus: ConnectionOnline, ChargePointStatus: string(StatusCharging),
		HasPublicDashboard: hasPublicDashboard, PoolCode: key.PoolCode, StationName: key.StationName,
	})
}

// endregion

// region Queries

// Connector returns the status of a connector (ok is false when unknown).
func (s *Service) Connector(key ConnectorKey) (Status, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.connectors[key]
	if !ok {
		return "", false
	}
	return c.status, true
}

// ActiveSession returns the session in progress on a connector (ok is false when none).
func (s *Service) ActiveSession(key ConnectorKey) (SessionUpdate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.sessions[key]
	return u, ok
}

// StationStatuses returns, per pool and station, the status of each connector (connector 0 excluded).
func (s *Service) StationStatuses() map[int]map[string]map[int]Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := map[int]map[string]map[int]Status{}
	for key, c := range s.connectors {
		stations, ok := result[key.PoolCode]
		if !ok {
			stations = map[string]map[int]Status{}
			result[key.PoolCode] = stations
		}
		connectors, ok := stations[key.StationName]
		if !ok {
			connectors = map[int]Status{}
			stations[key.StationName] = connectors
		}
		if key.ConnectorID != 0 {
			connectors[key.ConnectorID] = c.status
		}
	}
	return result
}

// PoolConnectors lists the known connectors of a pool (for the bucket cleanup).
func (s *Service) PoolConnectors(poolCode int) []bucket.Connector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []bucket.Connector
	for key := range s.connectors {
		if key.PoolCode == poolCode {
			list = append(list, bucket.Connector{StationName: key.StationName, ConnectorID: key.ConnectorID})
		}
	}
	return list
}

// PoolCodes returns the pools with known stations.
func (s *Service) PoolCodes() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	codes := map[int]bool{}
	for key := range s.connectors {
		codes[key.PoolCode] = true
	}
	return slices.Sorted(maps.Keys(codes))
}

// DeleteStation forgets a station: its profile, its connector statuses and its sessions in progress. It reports
// whether the station existed.
func (s *Service) DeleteStation(ctx context.Context, poolCode int, stationName string) (bool, error) {
	existed, err := s.store.DeleteStation(ctx, poolCode, stationName)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.connectors {
		if key.PoolCode == poolCode && key.StationName == stationName {
			delete(s.connectors, key)
			existed = true
		}
	}
	for key := range s.sessions {
		if key.PoolCode == poolCode && key.StationName == stationName {
			delete(s.sessions, key)
		}
	}
	delete(s.translated, stationKey{poolCode, stationName})
	return existed, nil
}

// endregion

// pyFloat formats a float as Python's str() does (always with a decimal part: 0.0, 12.5), as the dashboards
// showed it.
func pyFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
