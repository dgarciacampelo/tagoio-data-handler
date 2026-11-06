// This file holds the contract of the page of a finished charging session: its summary, the cost of each tariff band,
// the power and energy chart of its email, its meter values, and the payments the CSMS stored for it.

package dto

// SessionPage is the view model of a session's page (GET /emsp-dashboard/sessions/{pool_code}/{station_name}/
// {connector_id}/{transaction_id}, or /emsp-dashboard/sessions/{transaction_id} when only the id is known).
type SessionPage struct {
	TransactionID int64          `json:"transaction_id"`
	Session       *SessionDetail `json:"session" tstype:"SessionDetail | null,required"` // null: not found, or several sessions have the id
	Matches       []AuditSession `json:"matches"`                                        // The sessions with this transaction id, when Session is null
}

// SessionDetail is a finished session: what the history stored when it ended, and what its meter values tell.
type SessionDetail struct {
	ID            int64  `json:"id"` // History row (SessionSummary.ID)
	TransactionID int64  `json:"transaction_id"`
	PoolCode      int    `json:"pool_code"`
	CPOName       string `json:"cpo_name"`
	StationName   string `json:"station_name"`
	ConnectorID   int    `json:"connector_id"`
	CardAlias     string `json:"card_alias"`
	StartDate     string `json:"start_date"`  // DD/MM/YYYY, as the CSMS sends it
	TimeBand      string `json:"time_band"`   // "HH:MM - HH:MM"
	StopMotive    string `json:"stop_motive"` // CAR, COST, CSMS, DISCONNECT, EVSE, RESERVED, USER, OTHER; "" when not stored

	StartedAt       *string `json:"started_at" tstype:"string | null,required"`       // ISO 8601: start date and minute of the time band
	EndedAt         *string `json:"ended_at" tstype:"string | null,required"`         // ISO 8601: last meter value, else when the end was stored
	DurationSeconds *int64  `json:"duration_seconds" tstype:"number | null,required"` // From StartedAt to EndedAt

	EnergyWh      int64         `json:"energy_wh"` // Last meter value minus the first one
	Cost          float64       `json:"cost"`      // €, as the CSMS charged it
	Bands         []SessionBand `json:"bands"`     // Off-peak, flat and peak, in that order
	EvaluatedCost float64       `json:"evaluated_cost"`
	CostCheck     CostCheck     `json:"cost_check"`

	MeterValueCount int     `json:"meter_value_count"`
	PeakPowerW      *int64  `json:"peak_power_w" tstype:"number | null,required"`     // Highest power the station reported
	MeanPowerW      *int64  `json:"mean_power_w" tstype:"number | null,required"`     // Energy over the time from the start to the last delivery
	LastDeliveryAt  *string `json:"last_delivery_at" tstype:"string | null,required"` // ISO 8601: last meter value that added energy
	IdleSeconds     *int64  `json:"idle_seconds" tstype:"number | null,required"`     // From the last delivery to the end: plugged in, not charging
	TelemetryPurged bool    `json:"telemetry_purged"`                                 // No meter values, and the session is older than their retention

	Series      *SessionSeries      `json:"series" tstype:"SessionSeries | null,required"` // null: nothing to chart
	MeterValues []SessionMeterValue `json:"meter_values"`                                  // In time order

	ExportURL   string `json:"export_url"`   // XLSX audit
	PaymentsURL string `json:"payments_url"` // GET: SessionPayments
	AuditsURL   string `json:"audits_url"`   // The audits of its pool
}

// SessionBand is the energy and cost of a session in a tariff band.
type SessionBand struct {
	Band     TariffBand `json:"band"`
	EnergyWh int64      `json:"energy_wh"`
	Rate     float64    `json:"rate"` // €/kWh, frozen at the session start
	Cost     float64    `json:"cost"` // €, energy times rate (4 decimals)
}

// TariffBand is a tariff band of the CSMS.
type TariffBand string

const (
	TariffBandOffPeak TariffBand = "off_peak"
	TariffBandFlat    TariffBand = "flat"
	TariffBandPeak    TariffBand = "peak"
)

// CostCheck compares the cost the CSMS charged with the one its bands add up to (as the XLSX audit does).
type CostCheck string

const (
	CostCheckOK       CostCheck = "ok"       // At most a cent apart: the CSMS rounds to cents
	CostCheckCapped   CostCheck = "capped"   // Stopped at the authorized amount, which is below what the bands add up to
	CostCheckMismatch CostCheck = "mismatch" // More than a cent apart, for no known reason
)

// SessionSeries is the power and energy series of the session's email (internal/series): consecutive steps of equal
// length, each with its mean power and the energy delivered by its end; null in a gap without meter values.
type SessionSeries struct {
	Start  string   `json:"start"`                             // ISO 8601 (UTC): start of the first step
	Step   int      `json:"step"`                              // Seconds per step
	Power  []*int64 `json:"power" tstype:"(number | null)[]"`  // W
	Energy []*int64 `json:"energy" tstype:"(number | null)[]"` // Wh delivered since the start
}

// SessionMeterValue is a meter value of the session: the rows of the XLSX audit.
type SessionMeterValue struct {
	Timestamp     string  `json:"timestamp"`      // ISO 8601, as the CSMS sent it
	MeterValue    int64   `json:"meter_value"`    // Wh
	Power         int64   `json:"power"`          // W
	Cost          float64 `json:"cost"`           // €, as the CSMS reported it
	EvaluatedCost float64 `json:"evaluated_cost"` // €, the per-band energy times the rates
	Band          string  `json:"band"`           // Active band: "Off-Peak", "Flat" or "Peak"
	OffPeakWh     int64   `json:"off_peak_wh"`    // Cumulative per band
	FlatWh        int64   `json:"flat_wh"`
	PeakWh        int64   `json:"peak_wh"`
}

// SessionPayments is the answer of GET /api/admin/sessions/{pool_code}/{station_name}/{connector_id}/{transaction_id}/
// payments: the payments the CSMS stored for the session (usually one; none for an RFID or free session).
type SessionPayments struct {
	Payments []PaymentRecord `json:"payments"`
	Source   string          `json:"source"` // Host of the CSMS that answered
}
