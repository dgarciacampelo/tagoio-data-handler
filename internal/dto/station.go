// This file holds the contract of the public station page: the page drivers open from the QR code of a station.

package dto

// ConnectorStatus is an OCPP 1.6 connector status.
type ConnectorStatus string

const (
	ConnectorStatusAvailable     ConnectorStatus = "Available"
	ConnectorStatusPreparing     ConnectorStatus = "Preparing"
	ConnectorStatusCharging      ConnectorStatus = "Charging"
	ConnectorStatusSuspendedEVSE ConnectorStatus = "SuspendedEVSE"
	ConnectorStatusSuspendedEV   ConnectorStatus = "SuspendedEV"
	ConnectorStatusFinishing     ConnectorStatus = "Finishing"
	ConnectorStatusReserved      ConnectorStatus = "Reserved"
	ConnectorStatusUnavailable   ConnectorStatus = "Unavailable"
	ConnectorStatusFaulted       ConnectorStatus = "Faulted"
)

// StationPanel is the side panel of the station page.
type StationPanel string

const (
	StationPanelNone        StationPanel = "none"
	StationPanelPlugIn      StationPanel = "plug_in"      // Available: asks the driver to plug the vehicle in
	StationPanelPaymentForm StationPanel = "payment_form" // Preparing: the payment link form
)

// SessionCard is the card next to the status during a session.
type SessionCard string

const (
	SessionCardNone     SessionCard = "none"     // No session in progress
	SessionCardStop     SessionCard = "stop"     // Paid session: the driver can stop it
	SessionCardPrivate  SessionCard = "private"  // RFID session: stopped with the card
	SessionCardStarting SessionCard = "starting" // Charging reported, session data not received yet
)

// StationPage is the view model of the station page.
type StationPage struct {
	PoolCode    int             `json:"pool_code"`
	StationName string          `json:"station_name"`
	ConnectorID int             `json:"connector_id"` // Selected connector
	Connectors  []ConnectorLink `json:"connectors"`   // Connector selector (empty for a single connector)
	PollURL     string          `json:"poll_url"`     // GET it every few seconds for a new StationLive
	Live        StationLive     `json:"live"`
}

// ConnectorLink selects a connector of the station.
type ConnectorLink struct {
	ID       int    `json:"id"`
	Href     string `json:"href"`
	Selected bool   `json:"selected"`
}

// StationLive is the part of the station page that refreshes.
type StationLive struct {
	ConfigLoaded bool            `json:"config_loaded"` // False while the pool configuration is being read
	Status       ConnectorStatus `json:"status"`
	Panel        StationPanel    `json:"panel"`
	SessionCard  SessionCard     `json:"session_card"`
	ShowMetrics  bool            `json:"show_metrics"`                                    // Charging: the session metrics are shown
	Metrics      *SessionMetrics `json:"metrics" tstype:"SessionMetrics | null,required"` // Null without session data
	Operator     OperatorInfo    `json:"operator"`
	Rates        RatesInfo       `json:"rates"`
}

// SessionMetrics are the live values of the session.
type SessionMetrics struct {
	EnergyKWh       float64 `json:"energy_kwh"`
	PowerKW         float64 `json:"power_kw"`
	DurationMinutes string  `json:"duration_minutes"` // As reported by the CSMS
	Cost            float64 `json:"cost"`             // €
}

// OperatorInfo are the operator (CPO) details.
type OperatorInfo struct {
	Name      string `json:"name"`
	FiscalID  string `json:"fiscal_id"`
	Address   string `json:"address"`
	Phone     string `json:"phone"`
	PhoneHref string `json:"phone_href"` // tel: link
	Email     string `json:"email"`
	EmailHref string `json:"email_href"` // mailto: link
	Web       string `json:"web"`
}

// RatesInfo are the prices of the pool.
type RatesInfo struct {
	OffPeak       float64 `json:"off_peak"` // €/kWh
	Flat          float64 `json:"flat"`
	Peak          float64 `json:"peak"`
	VATPercent    int     `json:"vat_percent"`
	PreauthAmount int     `json:"preauth_amount"` // € held by the virtual POS
}

// ChargeRequest asks for a payment link to start a session (POST /api/charge-request).
type ChargeRequest struct {
	PoolCode        int     `json:"pool_code"`
	StationName     string  `json:"station_name"`
	ConnectorID     int     `json:"connector_id"`
	Email           string  `json:"email"`
	RequiresInvoice bool    `json:"requires_invoice"`
	ReceiptFiscalID *string `json:"receipt_fiscal_id" tstype:"string | null,required"`
	ReceiptName     *string `json:"receipt_name" tstype:"string | null,required"`
	ReceiptAddress  *string `json:"receipt_address" tstype:"string | null,required"`
	ReceiptEmail    *string `json:"receipt_email" tstype:"string | null,required"`
}

// ChargeRequestResult is the answer of a charge request.
type ChargeRequestResult struct {
	Status          string  `json:"status"` // "pending_authorization"
	AmountRequested float64 `json:"amount_requested"`
}

// StopRequest asks to stop a paid session (POST /api/stop-request).
type StopRequest struct {
	PoolCode    int    `json:"pool_code"`
	StationName string `json:"station_name"`
	ConnectorID int    `json:"connector_id"`
}
