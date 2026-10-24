// Package chargepoints keeps the live state of the charging stations reported by the CSMS: the status of every
// connector (with the quarantine of the stations that flood status changes) and the charging sessions in
// progress. It mirrors that state to the pool's TagoIO dashboards and stores what must survive a restart: the
// connector statuses, the station profiles and the session history and telemetry (for the audits).
package chargepoints

import (
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/jsonx"
)

// Status is an OCPP 1.6 connector status.
type Status string

const (
	StatusAvailable     Status = "Available"
	StatusPreparing     Status = "Preparing"
	StatusCharging      Status = "Charging"
	StatusSuspendedEVSE Status = "SuspendedEVSE"
	StatusSuspendedEV   Status = "SuspendedEV"
	StatusFinishing     Status = "Finishing"
	StatusReserved      Status = "Reserved"
	StatusUnavailable   Status = "Unavailable"
	StatusFaulted       Status = "Faulted"
)

// Statuses lists every OCPP status.
var Statuses = []Status{
	StatusAvailable, StatusPreparing, StatusCharging, StatusSuspendedEVSE, StatusSuspendedEV,
	StatusFinishing, StatusReserved, StatusUnavailable, StatusFaulted,
}

// ParseStatus returns the status named s, ignoring case.
func ParseStatus(s string) (Status, bool) {
	for _, status := range Statuses {
		if strings.EqualFold(string(status), strings.TrimSpace(s)) {
			return status, true
		}
	}
	return "", false
}

// InSession reports whether the status belongs to a session in progress (energy flowing or paused).
func (s Status) InSession() bool {
	return s == StatusCharging || s == StatusSuspendedEV || s == StatusSuspendedEVSE
}

// Connection statuses reported by the CSMS.
const (
	ConnectionOnline  = "Online"
	ConnectionBooting = "Booting"
	ConnectionOffline = "Offline"
)

// Session steps reported by the CSMS.
const (
	StepStarted    = "STARTED"
	StepInProgress = "INPROGRESS"
	StepCompleted  = "COMPLETED"
)

// spanishStatus are the status texts of the TagoIO dashboards.
var spanishStatus = map[string]string{
	"Available":     "Disponible",
	"Preparing":     "Preparando",
	"Charging":      "Cargando",
	"SuspendedEVSE": "Suspendido (cargador)",
	"SuspendedEV":   "Suspendido (coche)",
	"Finishing":     "Finalizando carga",
	"Reserved":      "Reservado",
	"Unavailable":   "No disponible",
	"Faulted":       "Error",
	"Booting":       "Conectando",
	"Offline":       "Desconectado",
	"Online":        "Conectado",
}

// translateStatus returns the dashboard text of a connector: its connection state while the station boots or is
// offline, else its status.
func translateStatus(status, connection string) string {
	key := status
	if connection == ConnectionBooting || connection == ConnectionOffline {
		key = connection
	}
	if text, ok := spanishStatus[key]; ok {
		return text
	}
	return "Desconocido"
}

// ConnectorKey identifies a connector; connector 0 is the station itself.
type ConnectorKey struct {
	PoolCode    int
	StationName string
	ConnectorID int
}

// StatusUpdate is a connector status change sent by the CSMS.
//
// ! Wire contract: neos-ocpp sends it as ChargePointUpdateBody (tagoio_data_handler.py there).
type StatusUpdate struct {
	ConnectorID        jsonx.Int `json:"connector_id"`
	ConnectionStatus   string    `json:"connection_status"`
	ChargePointStatus  string    `json:"charge_point_status"`
	AvailabilityType   string    `json:"availability_type"`
	ErrorCode          string    `json:"charge_point_error_code"`
	HasPublicDashboard bool      `json:"has_public_dashboard"`

	PoolCode    int    `json:"-"` // From the URL
	StationName string `json:"-"`
}

// ConnectorState is the live state of a connector, returned to the CSMS after a status update.
type ConnectorState struct {
	PoolCode       int        `json:"pool_code"`
	StationName    string     `json:"station_name"`
	ConnectorID    int        `json:"connector_id"`
	Status         Status     `json:"charge_point_status"`
	Quarantined    bool       `json:"is_quarantined"`
	QuarantineEnd  *time.Time `json:"quarantine_end"`
	LastUpdate     *time.Time `json:"last_update"`
	OmittedUpdates int        `json:"omitted_updates"` // Updates not sent to TagoIO during the quarantine
}

// SessionUpdate is a charging session update sent by the CSMS, at start, on every meter value and at the end.
//
// ! Wire contract: neos-ocpp sends it as ChargingSessionUpdate (tagoio_data_handler.py there).
type SessionUpdate struct {
	PoolCode      jsonx.Int `json:"pool_code"`
	StationName   string    `json:"station_name"`
	ConnectorID   jsonx.Int `json:"connector_id"`
	TransactionID jsonx.Int `json:"transaction_id"`

	CardAlias string `json:"card_alias"`
	CardCode  string `json:"card_code"`
	DisplayID string `json:"display_id"` // "<station> [<connector>]"

	StartDate         string    `json:"start_date"` // DD/MM/YYYY
	StartTime         string    `json:"start_time"` // HH:MM
	Step              string    `json:"step"`
	StartMeterValue   jsonx.Int `json:"start_meter_value"` // Wh
	LastMeterValue    jsonx.Int `json:"last_meter_value"`
	LastMeterTS       string    `json:"last_meter_ts"`       // ISO 8601
	CurrentTariffBand string    `json:"current_tariff_band"` // "Peak", "Flat" or "Off-Peak"

	RateOffPeak jsonx.Float `json:"rate_off_peak"` // €/kWh, frozen at the session start
	RateFlat    jsonx.Float `json:"rate_flat"`
	RatePeak    jsonx.Float `json:"rate_peak"`

	EnergyOffPeak jsonx.Int `json:"energy_off_peak"` // Wh per band, cumulative
	EnergyFlat    jsonx.Int `json:"energy_flat"`
	EnergyPeak    jsonx.Int `json:"energy_peak"`

	Energy     jsonx.Float `json:"energy"` // kWh
	EnergyUnit string      `json:"energy_unit"`
	Cost       jsonx.Float `json:"cost"`
	CostUnit   string      `json:"cost_unit"`
	Power      jsonx.Int   `json:"power"` // W
	PowerUnit  string      `json:"power_unit"`
	Time       string      `json:"time"` // Duration, as shown (minutes)

	WithPayment        bool    `json:"with_payment"`
	HasPublicDashboard bool    `json:"has_public_dashboard"`
	StopMotive         *string `json:"stop_motive"`
	TimeBand           *string `json:"time_band"` // "HH:MM - HH:MM", only when the session ended
}

// Key returns the connector of the session.
func (u SessionUpdate) Key() ConnectorKey {
	return ConnectorKey{PoolCode: int(u.PoolCode), StationName: u.StationName, ConnectorID: int(u.ConnectorID)}
}

// Ended reports whether the update closes the session.
func (u SessionUpdate) Ended() bool {
	return u.Step == StepCompleted || (u.TimeBand != nil && *u.TimeBand != "")
}

// withDefaults fills the units the CSMS may leave out.
func (u SessionUpdate) withDefaults() SessionUpdate {
	if u.EnergyUnit == "" {
		u.EnergyUnit = "KWh"
	}
	if u.CostUnit == "" {
		u.CostUnit = "€"
	}
	if u.PowerUnit == "" {
		u.PowerUnit = "W"
	}
	return u
}
