// This file holds the live view of a pool for the embedded management panel: its stations with their connection,
// the status of each connector and the sessions in progress.

package chargepoints

import (
	"slices"
	"strings"
	"time"
)

// StationView is a station of a pool as the management panel shows it.
type StationView struct {
	Name            string
	Connection      string    // ConnectionOnline, ConnectionBooting or ConnectionOffline
	ConnectionSince time.Time // Zero when unknown (before the last restart)
	Connectors      []ConnectorView
}

// ConnectorView is a connector of a station (connector 0, the station itself, is left out).
type ConnectorView struct {
	ID      int
	Status  Status
	Since   time.Time      // When the status last changed; zero when unknown
	Session *SessionUpdate // The session in progress, if any
}

// PoolView returns the stations of a pool, by name, with their connectors in order.
func (s *Service) PoolView(poolCode int) []StationView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byName := map[string]*StationView{}
	for key, c := range s.connectors {
		if key.PoolCode != poolCode {
			continue
		}
		station, ok := byName[key.StationName]
		if !ok {
			conn := s.connections[stationKey{poolCode, key.StationName}]
			station = &StationView{Name: key.StationName, Connection: conn.state, ConnectionSince: conn.since}
			if station.Connection == "" {
				station.Connection = ConnectionOnline
			}
			byName[key.StationName] = station
		}
		if key.ConnectorID == 0 {
			continue
		}
		view := ConnectorView{ID: key.ConnectorID, Status: c.status, Since: c.since}
		if u, ok := s.sessions[key]; ok {
			session := u
			view.Session = &session
		}
		station.Connectors = append(station.Connectors, view)
	}

	stations := make([]StationView, 0, len(byName))
	for _, station := range byName {
		slices.SortFunc(station.Connectors, func(a, b ConnectorView) int { return a.ID - b.ID })
		stations = append(stations, *station)
	}
	slices.SortFunc(stations, func(a, b StationView) int { return strings.Compare(a.Name, b.Name) })
	return stations
}

// PoolVersion returns a number that changes whenever a status or a session of the pool changes.
func (s *Service) PoolVersion(poolCode int) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.versions[poolCode]
}

// HasStation reports whether a station of the pool is known.
func (s *Service) HasStation(poolCode int, stationName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key := range s.connectors {
		if key.PoolCode == poolCode && key.StationName == stationName {
			return true
		}
	}
	return false
}
