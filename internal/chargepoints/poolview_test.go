package chargepoints

import (
	"context"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/jsonx"
)

func TestPoolViewTracksConnectionsStatusesAndSessions(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	update := func(station string, connector int, connection, status string) {
		t.Helper()
		if _, err := s.UpdateStatus(ctx, StatusUpdate{PoolCode: 220001, StationName: station, ConnectorID: jsonx.Int(connector),
			ConnectionStatus: connection, ChargePointStatus: status}); err != nil {
			t.Fatal(err)
		}
	}
	update("B", 1, ConnectionOnline, "Available")
	update("A", 2, ConnectionOnline, "Available")
	update("A", 1, ConnectionOnline, "Available")
	update("OTHER", 1, ConnectionOnline, "Available")
	version := s.PoolVersion(220001)

	now = now.Add(5 * time.Minute)
	update("A", 1, ConnectionOnline, "Charging")
	if s.PoolVersion(220001) == version {
		t.Error("the pool version did not change with a status update")
	}
	if err := s.UpdateSession(ctx, SessionUpdate{PoolCode: 220001, StationName: "A", ConnectorID: 1, TransactionID: 9,
		Step: StepInProgress, LastMeterTS: "2026-11-23T11:05:00+01:00", Power: 7000}); err != nil {
		t.Fatal(err)
	}

	view := s.PoolView(220001)
	if len(view) != 3 || view[0].Name != "A" || view[1].Name != "B" {
		t.Fatalf("stations = %+v; want A, B, OTHER in order", view)
	}
	a := view[0]
	if len(a.Connectors) != 2 || a.Connectors[0].ID != 1 || a.Connectors[1].ID != 2 {
		t.Fatalf("connectors of A = %+v", a.Connectors)
	}
	if c := a.Connectors[0]; c.Status != StatusCharging || !c.Since.Equal(now) || c.Session == nil || c.Session.TransactionID != 9 {
		t.Errorf("A[1] = %+v; want charging since now, with its session", c)
	}
	if c := a.Connectors[1]; c.Session != nil || !c.Since.Equal(now.Add(-5*time.Minute)) {
		t.Errorf("A[2] = %+v; want no session, unchanged since the first update", c)
	}

	now = now.Add(time.Minute)
	update("B", 0, ConnectionOffline, "")
	view = s.PoolView(220001)
	if b := view[1]; b.Connection != ConnectionOffline || !b.ConnectionSince.Equal(now) || b.Connectors[0].Status != StatusUnavailable {
		t.Errorf("B after going offline = %+v", b)
	}
	if !s.HasStation(220001, "B") || s.HasStation(220001, "C") || s.HasStation(221006, "A") {
		t.Error("HasStation answers wrong")
	}
}

func TestPoolViewAfterARestartShowsTheStationsOnline(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	if _, err := s.UpdateStatus(ctx, StatusUpdate{PoolCode: 220001, StationName: "A", ConnectorID: 1,
		ConnectionStatus: ConnectionOffline}); err != nil {
		t.Fatal(err)
	}

	restarted := New(db, &recordingBucket{}, s.log)
	if err := restarted.Load(ctx); err != nil {
		t.Fatal(err)
	}
	view := restarted.PoolView(220001)
	if len(view) != 1 || view[0].Connection != ConnectionOnline || !view[0].ConnectionSince.IsZero() || !view[0].Connectors[0].Since.IsZero() {
		t.Errorf("view after a restart = %+v; want online, with unknown times", view)
	}
}
