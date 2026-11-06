package chargepoints

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/jsonx"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

type recordingBucket struct {
	mu      sync.Mutex
	records []tagoio.DataInput
}

func (b *recordingBucket) Insert(_ context.Context, _ int, records ...tagoio.DataInput) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, records...)
	return nil
}

// newTestService returns a service whose dashboard writes are collected by draining the queue.
func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, &recordingBucket{}, slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

// drain returns the queued dashboard writes.
func drain(s *Service) []tagoio.DataInput {
	var records []tagoio.DataInput
	for {
		select {
		case p := <-s.pushes:
			records = append(records, p.records...)
		default:
			return records
		}
	}
}

func statusUpdate(connector int, connection, status, errorCode string) StatusUpdate {
	return StatusUpdate{
		PoolCode: 261001, StationName: "ST1", ConnectorID: jsonx.Int(connector),
		ConnectionStatus: connection, ChargePointStatus: status, AvailabilityType: "Operative",
		ErrorCode: errorCode, HasPublicDashboard: true,
	}
}

func TestStatusUpdateWritesDashboardsAndStore(t *testing.T) {
	ctx := context.Background()
	s, db := newTestService(t)

	state, err := s.UpdateStatus(ctx, statusUpdate(1, ConnectionOnline, "Available", "NoError"))
	if err != nil || state.Status != StatusAvailable {
		t.Fatalf("UpdateStatus = %+v, %v", state, err)
	}
	records := drain(s)
	if len(records) != 2 {
		t.Fatalf("dashboard writes = %+v; want the station row and the public variable", records)
	}
	if records[0].Variable != "state" || records[0].Metadata["state_1"] != "Disponible" || records[0].Metadata["connection_state"] != "Online" {
		t.Errorf("management row = %+v", records[0])
	}
	if records[1].Variable != "state_ST1_1" || records[1].Value != "Disponible" {
		t.Errorf("public variable = %+v", records[1])
	}

	statuses, _ := db.ConnectorStatuses(ctx)
	if len(statuses) != 1 || statuses[0].Status != "Available" {
		t.Errorf("stored statuses = %+v", statuses)
	}
	if noc, ok, _ := db.StationConnectorCount(ctx, "ST1"); !ok || noc != 1 {
		t.Errorf("station profile noc = %d (ok %v); want 1", noc, ok)
	}
}

func TestStationOfflineCascadesToItsConnectors(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestService(t)
	for _, id := range []int{1, 2} {
		if _, err := s.UpdateStatus(ctx, statusUpdate(id, ConnectionOnline, "Charging", "NoError")); err != nil {
			t.Fatal(err)
		}
	}
	drain(s)

	if _, err := s.UpdateStatus(ctx, statusUpdate(0, ConnectionOffline, "Available", "NoError")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{0, 1, 2} {
		status, ok := s.Connector(ConnectorKey{261001, "ST1", id})
		if !ok || status != StatusUnavailable {
			t.Errorf("connector %d = %q (known %v); want Unavailable", id, status, ok)
		}
	}
	for _, r := range drain(s) {
		if r.Variable == "state" && r.Metadata["state_1"] != "Desconectado" {
			t.Errorf("an offline station must show 'Desconectado', got %+v", r.Metadata)
		}
	}
}

func TestUnderVoltageQuarantineHoldsBackDashboardWrites(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestService(t)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	fault := statusUpdate(1, ConnectionOnline, "Faulted", "UnderVoltage")
	state, _ := s.UpdateStatus(ctx, fault)
	if !state.Quarantined || len(drain(s)) == 0 {
		t.Fatalf("the fault that starts the quarantine must be shown: %+v", state)
	}

	// Flapping during the quarantine: stored, counted, not written to the dashboards.
	for _, status := range []string{"Available", "Faulted", "Available"} {
		now = now.Add(time.Minute)
		state, _ = s.UpdateStatus(ctx, statusUpdate(1, ConnectionOnline, status, "UnderVoltage"))
	}
	if !state.Quarantined || state.OmittedUpdates != 3 || len(drain(s)) != 0 {
		t.Fatalf("during the quarantine: %+v; want 3 omitted updates and no dashboard write", state)
	}

	// After 30 minutes, a non-fault status ends it.
	now = now.Add(30 * time.Minute)
	state, _ = s.UpdateStatus(ctx, statusUpdate(1, ConnectionOnline, "Available", "NoError"))
	if state.Quarantined || state.OmittedUpdates != 0 || len(drain(s)) == 0 {
		t.Fatalf("after the quarantine: %+v; want it lifted and the status written", state)
	}

	// A plugged vehicle (Preparing) always lifts it.
	s.UpdateStatus(ctx, fault)
	drain(s)
	state, _ = s.UpdateStatus(ctx, statusUpdate(1, ConnectionOnline, "Preparing", "NoError"))
	if state.Quarantined {
		t.Fatalf("Preparing must lift the quarantine")
	}
}

func TestUnknownStatusIsRejected(t *testing.T) {
	s, _ := newTestService(t)
	if _, err := s.UpdateStatus(context.Background(), statusUpdate(1, ConnectionOnline, "Bogus", "NoError")); err == nil {
		t.Fatal("an unknown status must be rejected")
	}
}

func sessionUpdate(t *testing.T, raw string) SessionUpdate {
	t.Helper()
	var u SessionUpdate
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("decoding session update: %v", err)
	}
	return u
}

const sessionJSON = `{"pool_code":261001,"station_name":"ST1","connector_id":1,"transaction_id":42,"card_alias":"VPOS",
	"card_code":"c","display_id":"ST1 [1]","start_date":"22/10/2026","start_time":"09:00","step":"%s",
	"start_meter_value":1000,"last_meter_value":6000.0,"last_meter_ts":"2026-10-22T07:30:00+00:00",
	"current_tariff_band":"Flat","rate_off_peak":0.1,"rate_flat":"0.2","rate_peak":0.3,"energy_off_peak":0,
	"energy_flat":5000,"energy_peak":0,"energy":5.0,"cost":1.0,"power":7400,"time":"30","with_payment":true,
	"has_public_dashboard":true,"stop_motive":%s,"time_band":%s}`

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	s, db := newTestService(t)
	key := ConnectorKey{261001, "ST1", 1}
	if _, err := s.UpdateStatus(ctx, statusUpdate(1, ConnectionOnline, "Preparing", "NoError")); err != nil {
		t.Fatal(err)
	}
	drain(s)

	// In progress: kept for the public page, charging inferred, telemetry stored.
	inProgress := sessionUpdate(t, fmt.Sprintf(sessionJSON, "INPROGRESS", "null", "null"))
	if err := s.UpdateSession(ctx, inProgress); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ActiveSession(key); !ok {
		t.Fatal("the session in progress must be kept")
	}
	if status, _ := s.Connector(key); status != StatusCharging {
		t.Fatalf("status = %q; want Charging inferred from the meter values", status)
	}
	writes := drain(s)
	if !hasVariable(writes, "active_cs_data") || !hasVariable(writes, "energy_ST1_1") || !hasVariable(writes, "state") {
		t.Fatalf("dashboard writes = %+v; want the active session, the public values and the resynchronized status", writes)
	}

	// Completed: history stored once, public values reset, session forgotten.
	completed := sessionUpdate(t, fmt.Sprintf(sessionJSON, "COMPLETED", `"EVDisconnected"`, `"09:00 - 09:30"`))
	if err := s.UpdateSession(ctx, completed); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ActiveSession(key); ok {
		t.Fatal("the ended session must be forgotten")
	}
	writes = drain(s)
	if !hasVariable(writes, "charging_session_data") {
		t.Errorf("a paid session must be added to the dashboard history: %+v", writes)
	}
	for _, w := range writes {
		if w.Variable == "energy_ST1_1" && w.Value != "0.0 KWh" {
			t.Errorf("public energy after the end = %v; want 0.0 KWh", w.Value)
		}
	}
	if err := s.UpdateSession(ctx, completed); err != nil || len(drain(s)) != 0 {
		t.Fatalf("a repeated end must be ignored without dashboard writes (err %v)", err)
	}

	telemetry, _ := db.SessionTelemetry(ctx, 261001, "ST1", 1, 42)
	sessions, _ := db.RecentSessions(ctx, 261001, 10)
	if len(telemetry) != 1 || len(sessions) != 1 || sessions[0].RateFlat != 0.2 {
		t.Fatalf("telemetry %d rows, sessions %+v; want 1 telemetry row (same timestamp) and the session with its rates", len(telemetry), sessions)
	}
}

// backfillUpdate is a session update of transaction 42 sent late, at the given time.
func backfillUpdate(t *testing.T, step, at string, meter int) SessionUpdate {
	t.Helper()
	stopMotive, timeBand := "null", "null"
	if step == StepCompleted {
		stopMotive, timeBand = `"EVDisconnected"`, `"09:00 - 09:30"`
	}
	u := sessionUpdate(t, fmt.Sprintf(sessionJSON, step, stopMotive, timeBand))
	u.LastMeterTS, u.LastMeterValue = at, jsonx.Int(meter)
	return u
}

func TestBackfillStoresMeterValuesWithoutDashboardWrites(t *testing.T) {
	ctx := context.Background()
	s, db := newTestService(t)
	key := ConnectorKey{261001, "ST1", 1}

	for i, at := range []string{"2026-10-22T09:10:00+02:00", "2026-10-22T09:05:00+02:00"} { // In any order
		stored, err := s.BackfillSession(ctx, backfillUpdate(t, StepInProgress, at, 2000+i*1000))
		if err != nil || !stored {
			t.Fatalf("BackfillSession(%s) = %v, %v; want it stored", at, stored, err)
		}
	}
	if stored, err := s.BackfillSession(ctx, backfillUpdate(t, StepInProgress, "2026-10-22T09:05:00+02:00", 3000)); err != nil || stored {
		t.Fatalf("a repeated meter value = %v, %v; want a duplicate", stored, err)
	}

	if writes := drain(s); len(writes) != 0 {
		t.Fatalf("dashboard writes = %+v; a late meter value must not overwrite the live values", writes)
	}
	if _, ok := s.ActiveSession(key); ok {
		t.Fatal("a late meter value must not make its session active")
	}
	if _, known := s.Connector(key); known {
		t.Fatal("a late meter value must not infer the connector's status")
	}
	telemetry, _ := db.SessionTelemetry(ctx, 261001, "ST1", 1, 42)
	if len(telemetry) != 2 || telemetry[0].Timestamp != "2026-10-22T09:05:00+02:00" {
		t.Fatalf("telemetry = %+v; want both meter values, in time order", telemetry)
	}
}

func TestBackfillEndOfUnseenSession(t *testing.T) {
	ctx := context.Background()
	s, db := newTestService(t)
	key := ConnectorKey{261001, "ST1", 1}

	// The handler saw the session running, then was down when it ended
	if err := s.UpdateSession(ctx, backfillUpdate(t, StepInProgress, "2026-10-22T09:05:00+02:00", 2000)); err != nil {
		t.Fatal(err)
	}
	drain(s)

	stored, err := s.BackfillSession(ctx, backfillUpdate(t, StepCompleted, "2026-10-22T09:30:00+02:00", 6000))
	if err != nil || !stored {
		t.Fatalf("BackfillSession(COMPLETED) = %v, %v; want it stored", stored, err)
	}
	if _, ok := s.ActiveSession(key); ok {
		t.Fatal("the session that ended must be forgotten")
	}
	writes := drain(s)
	if !hasVariable(writes, HistoryVariable) || !hasVariable(writes, "active_cs_data") {
		t.Fatalf("dashboard writes = %+v; want the history entry and the connector's reset", writes)
	}
	for _, w := range writes {
		if w.Variable == "energy_ST1_1" && w.Value != "0.0 KWh" {
			t.Errorf("public energy after the late end = %v; want 0.0 KWh", w.Value)
		}
	}
	sessions, _ := db.RecentSessions(ctx, 261001, 10)
	if len(sessions) != 1 || sessions[0].TransactionID != 42 || sessions[0].StopMotive != "EVDisconnected" {
		t.Fatalf("history = %+v; want the session that ended, with its stop motive", sessions)
	}

	// Sent again (the CSMS didn't get the answer): nothing new, no dashboard writes
	if stored, err := s.BackfillSession(ctx, backfillUpdate(t, StepCompleted, "2026-10-22T09:30:00+02:00", 6000)); err != nil || stored {
		t.Fatalf("a repeated late end = %v, %v; want a duplicate", stored, err)
	}
	if writes := drain(s); len(writes) != 0 {
		t.Fatalf("a repeated late end wrote %+v", writes)
	}
}

func TestBackfillEndKeepsTheConnectorsNewerSession(t *testing.T) {
	ctx := context.Background()
	s, db := newTestService(t)
	key := ConnectorKey{261001, "ST1", 1}

	// A newer session (43) is running live when the end of session 42 arrives late
	newer := backfillUpdate(t, StepInProgress, "2026-10-22T10:05:00+02:00", 9000)
	newer.TransactionID = 43
	if err := s.UpdateSession(ctx, newer); err != nil {
		t.Fatal(err)
	}
	drain(s)

	if stored, err := s.BackfillSession(ctx, backfillUpdate(t, StepCompleted, "2026-10-22T09:30:00+02:00", 6000)); err != nil || !stored {
		t.Fatalf("BackfillSession(COMPLETED) = %v, %v; want it stored", stored, err)
	}
	if active, ok := s.ActiveSession(key); !ok || active.TransactionID != 43 {
		t.Fatalf("active session = %+v (%v); want the newer session 43 kept", active, ok)
	}
	writes := drain(s)
	if !hasVariable(writes, HistoryVariable) {
		t.Errorf("dashboard writes = %+v; want the late session's history entry", writes)
	}
	if hasVariable(writes, "active_cs_data") || hasVariable(writes, "energy_ST1_1") {
		t.Errorf("dashboard writes = %+v; the late end must not reset the newer session's live values", writes)
	}
	if sessions, _ := db.RecentSessions(ctx, 261001, 10); len(sessions) != 1 {
		t.Fatalf("history = %+v; want the late session", sessions)
	}
}

func TestBackfillAfterTheLiveEndIsADuplicate(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestService(t)
	end := backfillUpdate(t, StepCompleted, "2026-10-22T09:30:00+02:00", 6000)
	if err := s.UpdateSession(ctx, end); err != nil {
		t.Fatal(err)
	}
	drain(s)

	// The live POST was stored, but its answer was lost: the CSMS's outbox sends it again
	if stored, err := s.BackfillSession(ctx, end); err != nil || stored {
		t.Fatalf("BackfillSession after the live end = %v, %v; want a duplicate", stored, err)
	}
	if writes := drain(s); len(writes) != 0 {
		t.Fatalf("a duplicate end wrote %+v", writes)
	}
}

func TestSessionUpdateValidate(t *testing.T) {
	valid := func() SessionUpdate {
		return backfillUpdate(t, StepInProgress, "2026-10-22T09:05:00.123456+02:00", 2000)
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("a valid update: %v", err)
	}
	naive := valid()
	naive.LastMeterTS = "2026-10-22T09:05:00.123456" // A session the CSMS recovered without its time zone
	if err := naive.Validate(); err != nil {
		t.Fatalf("a timestamp without offset: %v", err)
	}
	started := valid()
	started.Step, started.LastMeterTS = StepStarted, ""
	if err := started.Validate(); err != nil {
		t.Fatalf("a STARTED update carries no meter value: %v", err)
	}

	broken := map[string]func(*SessionUpdate){
		"pool code":      func(u *SessionUpdate) { u.PoolCode = 0 },
		"station":        func(u *SessionUpdate) { u.StationName = " " },
		"connector":      func(u *SessionUpdate) { u.ConnectorID = 0 },
		"transaction id": func(u *SessionUpdate) { u.TransactionID = 0 },
		"step":           func(u *SessionUpdate) { u.Step = "FINISHED" },
		"timestamp":      func(u *SessionUpdate) { u.LastMeterTS = "22/10/2026 09:05" },
	}
	for name, breakIt := range broken {
		u := valid()
		breakIt(&u)
		if err := u.Validate(); err == nil {
			t.Errorf("an update without a valid %s passed", name)
		}
	}
}

func TestDeleteStationForgetsEverything(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestService(t)
	s.UpdateStatus(ctx, statusUpdate(1, ConnectionOnline, "Available", "NoError"))
	existed, err := s.DeleteStation(ctx, 261001, "ST1")
	if err != nil || !existed {
		t.Fatalf("DeleteStation = %v, %v", existed, err)
	}
	if _, ok := s.Connector(ConnectorKey{261001, "ST1", 1}); ok {
		t.Fatal("the deleted station's connector is still known")
	}
	if len(s.StationStatuses()) != 0 {
		t.Fatal("the deleted station is still listed")
	}
}

func TestPyFloat(t *testing.T) {
	for in, want := range map[float64]string{0: "0.0", 12.5: "12.5", 3: "3.0", 0.125: "0.125"} {
		if got := pyFloat(in); got != want {
			t.Errorf("pyFloat(%v) = %q; want %q", in, got, want)
		}
	}
}

func hasVariable(records []tagoio.DataInput, variable string) bool {
	for _, r := range records {
		if r.Variable == variable {
			return true
		}
	}
	return false
}
