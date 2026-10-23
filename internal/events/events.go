// Package events defines the events this service publishes on /api/stream for the CSMS instances (neos-ocpp) and
// the broker that fans them out. The service is an anti-corruption layer: the CSMS only sees these typed events,
// never TagoIO's payloads.
//
// ! These structs are a wire contract: neos-ocpp parses them with its own models (interface/sse_events.py there),
// so field names, types and nullability must not change without changing neos-ocpp first. Optional fields are
// pointers without omitempty, so they are sent as null, like the Python service did.
package events

// Type identifies an event; neos-ocpp dispatches on it.
type Type string

const (
	TypeChangeAvailability      Type = "change_availability"
	TypeChangeMaxGridPower      Type = "change_max_grid_power"
	TypeManageRFID              Type = "manage_rfid"
	TypeChangeCPOInfo           Type = "change_cpo_info"
	TypeChangeRateList          Type = "change_rate_list"
	TypeChangeLoadBalancingMode Type = "change_load_balancing_mode"
	TypeOCPPRequest             Type = "ocpp_request"
	TypePowerConsumptionUpdate  Type = "power_consumption_update"
	TypeVPOSStartTransaction    Type = "virtual_POS_start_transaction"
	TypeVPOSStopTransaction     Type = "virtual_POS_stop_transaction"
)

// Event is any event of this package.
type Event interface {
	header() Header
}

// Header holds the fields every event starts with.
type Header struct {
	PoolCode  int  `json:"pool_code"`
	EventType Type `json:"event_type"`
}

func (h Header) header() Header { return h }

// NewHeader returns the header of an event of the given type for a pool.
func NewHeader(t Type, poolCode int) Header {
	return Header{PoolCode: poolCode, EventType: t}
}

// region Pool management (CPO actions in the TagoIO management dashboard)

// ChangeAvailability asks the CSMS to toggle the operativity of a station.
type ChangeAvailability struct {
	Header
	StationName string `json:"station_name"`
}

// MaxGridPower sets the maximum power the pool may draw from the grid.
type MaxGridPower struct {
	Header
	MaxPowerWatts float64 `json:"max_power_watts"`
}

// RFID actions.
const (
	RFIDCreate = "create"
	RFIDDelete = "delete"
)

// RFIDManagement creates or deletes an RFID card of the pool.
type RFIDManagement struct {
	Header
	CardID    string   `json:"card_id"`    // Lower case
	Action    string   `json:"action"`     // RFIDCreate or RFIDDelete
	LinkedCPs []string `json:"linked_cps"` // Station serials the card may charge at (never null)
	Alias     *string  `json:"alias"`      // Upper case; null on delete
	Email     *string  `json:"email"`
}

// CPOInfo updates the operator (CPO) details shown to the drivers and printed on the receipts.
type CPOInfo struct {
	Header
	Name     string `json:"name"`
	FiscalID string `json:"fiscal_id"`
	Address  string `json:"address"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	Web      string `json:"web"`
}

// RateList updates the energy rates and the payment pre-authorization amount.
type RateList struct {
	Header
	RateOffPeak       float64 `json:"rate_off_peak"` // €/kWh
	RateFlat          float64 `json:"rate_flat"`
	RatePeak          float64 `json:"rate_peak"`
	VAT               float64 `json:"vat"`                // ! Percentage (10 for 10 %): neos-ocpp divides it by 100
	WithholdingAmount float64 `json:"withholding_amount"` // €
}

// LoadBalancing selects the dynamic load balancing mode of the pool.
type LoadBalancing struct {
	Header
	SelectedMode string `json:"selected_mode"`
}

// PowerUpdate reports the power the installation draws from the grid, from its MQTT meter.
type PowerUpdate struct {
	Header
	MeterWatts float64 `json:"meter_watts"`
}

// endregion

// region Station management (OCPP commands of the dashboard's debug tab)

// OCPPRequest replicates an OCPP command; Payload is one of the *Payload types below.
type OCPPRequest struct {
	Header
	Payload any `json:"payload"`
}

// OCPP request names (the "request" field of the payloads).
const (
	RequestStatusNotification     = "status_notification"
	RequestChangeAvailability     = "change_availability"
	RequestReset                  = "reset"
	RequestRemoteStartTransaction = "remote_start_transaction"
	RequestRemoteStopTransaction  = "remote_stop_transaction"
	RequestUnlockConnector        = "unlock_connector"
)

type StatusNotificationPayload struct {
	Request      string   `json:"request"`
	StationNames []string `json:"station_names"`
	ConnectorID  int      `json:"connector_id"`
}

type ChangeAvailabilityPayload struct {
	Request          string   `json:"request"`
	StationNames     []string `json:"station_names"`
	ConnectorID      int      `json:"connector_id"`
	AvailabilityType string   `json:"availability_type"` // "Operative" or "Inoperative" (OCPP 1.6)
}

type ResetPayload struct {
	Request      string   `json:"request"`
	StationNames []string `json:"station_names"`
	ResetType    string   `json:"reset_type"` // "Soft" or "Hard"
}

type RemoteStartPayload struct {
	Request      string   `json:"request"`
	StationNames []string `json:"station_names"`
	ConnectorID  int      `json:"connector_id"`
	IDTag        string   `json:"id_tag"`
}

type RemoteStopPayload struct {
	Request       string `json:"request"`
	TransactionID int    `json:"transaction_id"`
}

type UnlockConnectorPayload struct {
	Request      string   `json:"request"`
	StationNames []string `json:"station_names"`
	ConnectorID  int      `json:"connector_id"`
}

// endregion

// region Virtual POS (drivers paying from the public station page)

// VPOSStart asks the CSMS to send the driver a payment link and start the session once paid.
type VPOSStart struct {
	Header
	StationName     string  `json:"station_name"`
	ConnectorID     int     `json:"connector_id"`
	Email           string  `json:"email"`
	Amount          float64 `json:"amount"` // Pre-authorization, in euros
	Currency        string  `json:"currency"`
	RequiresInvoice bool    `json:"requires_invoice"`
	ReceiptEmail    *string `json:"receipt_email"`
	ReceiptName     *string `json:"receipt_name"`
	ReceiptFiscalID *string `json:"receipt_fiscal_id"`
	ReceiptAddress  *string `json:"receipt_address"`
}

// VPOSStop asks the CSMS to stop a paid session.
type VPOSStop struct {
	Header
	StationName string `json:"station_name"`
	ConnectorID int    `json:"connector_id"`
}

// endregion
