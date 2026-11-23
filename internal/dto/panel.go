package dto

// region Panel: session

// PanelSessionRequest starts a panel session: the TagoIO token of the person who has the dashboard open, and the
// pool's device as the dashboard selected it.
type PanelSessionRequest struct {
	Token    string `json:"token"`
	DeviceID string `json:"device_id"`
}

// PanelUserKind is who opened the panel.
type PanelUserKind string

const (
	PanelUserKindRunUser PanelUserKind = "run_user" // A CPO in TagoIO Run: the pools of its group_id tags
	PanelUserKindAdmin   PanelUserKind = "admin"    // The TagoIO account itself: every pool
)

// PanelSession is a started panel session: sent as "Authorization: Bearer <session>" on every panel call.
type PanelSession struct {
	Session   string        `json:"session"`
	ExpiresAt string        `json:"expires_at"` // RFC 3339
	PoolCode  int           `json:"pool_code"`
	UserKind  PanelUserKind `json:"user_kind"`
}

// endregion

// region Panel: overview

// PanelOverview is the configuration of a pool, read when the panel opens and after each change.
type PanelOverview struct {
	PoolCode          int                 `json:"pool_code"`
	InstallationName  string              `json:"installation_name"`
	CPO               PanelCPO            `json:"cpo"`
	Rates             PanelRates          `json:"rates"`
	MaxPowerW         float64             `json:"max_power_w"`
	LoadBalancingMode string              `json:"load_balancing_mode"` // Label, e.g. "Dinámico normal"
	BalancingModes    []string            `json:"balancing_modes"`     // Every label, in order
	Stations          []string            `json:"stations"`            // Station names, sorted
	Bands             []PanelTariffPeriod `json:"bands"`               // Weekday tariff periods (weekends and holidays: off-peak)
}

// PanelCPO are the operator details.
type PanelCPO struct {
	Name     string `json:"name"`
	FiscalID string `json:"fiscal_id"`
	Address  string `json:"address"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	Web      string `json:"web"`
}

// PanelRates are the energy rates (€/kWh, VAT included), the VAT and the virtual POS pre-authorization (€).
type PanelRates struct {
	OffPeak     float64 `json:"off_peak"`
	Flat        float64 `json:"flat"`
	Peak        float64 `json:"peak"`
	VATPercent  float64 `json:"vat_percent"`
	Withholding float64 `json:"withholding"`
}

// PanelTariffPeriod is a weekday period of a tariff band, in whole hours [from, to).
type PanelTariffPeriod struct {
	Band     TariffBand `json:"band"`
	FromHour int        `json:"from_hour"`
	ToHour   int        `json:"to_hour"`
}

// endregion

// region Panel: live state (stream)

// PanelConnection is the connection state of a station.
type PanelConnection string

const (
	PanelConnectionOnline  PanelConnection = "Online"
	PanelConnectionBooting PanelConnection = "Booting"
	PanelConnectionOffline PanelConnection = "Offline"
)

// PanelLive is the live state of a pool, sent by the panel stream at start and on every change.
type PanelLive struct {
	GeneratedAt string             `json:"generated_at"` // RFC 3339
	Stations    []PanelStation     `json:"stations"`
	Sessions    []PanelLiveSession `json:"sessions"`
	TotalPowerW int64              `json:"total_power_w"`
}

// PanelStation is a station with its connectors.
type PanelStation struct {
	Name            string           `json:"name"`
	Connection      PanelConnection  `json:"connection"`
	ConnectionSince *string          `json:"connection_since" tstype:"string | null,required"` // RFC 3339; null when unknown
	Connectors      []PanelConnector `json:"connectors"`
}

// PanelConnector is a connector of a station.
type PanelConnector struct {
	ID     int             `json:"id"`
	Status ConnectorStatus `json:"status"`
	Since  *string         `json:"since" tstype:"string | null,required"` // RFC 3339; null when unknown
}

// PanelLiveSession is a charging session in progress.
type PanelLiveSession struct {
	StationName   string     `json:"station_name"`
	ConnectorID   int        `json:"connector_id"`
	TransactionID int64      `json:"transaction_id"`
	CardAlias     string     `json:"card_alias"`
	StartDate     string     `json:"start_date"` // DD/MM/YYYY
	StartTime     string     `json:"start_time"` // HH:MM
	Duration      string     `json:"duration"`   // As the CSMS formats it
	PowerW        int64      `json:"power_w"`
	EnergyKWh     float64    `json:"energy_kwh"`
	Cost          float64    `json:"cost"` // € so far
	CurrentBand   TariffBand `json:"current_band"`
	EnergyBandsWh [3]int64   `json:"energy_bands_wh"` // Off-peak, flat, peak
	LastMeterAt   string     `json:"last_meter_at"`   // ISO 8601, as the CSMS sent it
}

// endregion

// region Panel: history

// PanelHistorySort is the column the history is sorted by.
type PanelHistorySort string

const (
	PanelHistorySortDate    PanelHistorySort = "date"
	PanelHistorySortStation PanelHistorySort = "station"
	PanelHistorySortCard    PanelHistorySort = "card"
	PanelHistorySortMinutes PanelHistorySort = "minutes"
	PanelHistorySortEnergy  PanelHistorySort = "energy"
	PanelHistorySortCost    PanelHistorySort = "cost"
)

// PanelHistoryPage is a page of the finished sessions, with the totals of every session the filter selects.
type PanelHistoryPage struct {
	Items    []PanelHistoryItem `json:"items"`
	Total    int                `json:"total"` // Sessions the filter selects
	Page     int                `json:"page"`  // From 1
	PageSize int                `json:"page_size"`
	Totals   PanelHistoryTotals `json:"totals"`
}

// PanelHistoryItem is a finished session.
type PanelHistoryItem struct {
	TransactionID int64   `json:"transaction_id"`
	StartedAt     string  `json:"started_at"` // Local date and time, "2006-01-02T15:04"
	TimeBand      string  `json:"time_band"`  // "HH:MM - HH:MM"
	Station       string  `json:"station"`    // "<station> [<connector>]"
	Card          string  `json:"card"`
	Minutes       int     `json:"minutes"`
	EnergyKWh     float64 `json:"energy_kwh"`
	Cost          float64 `json:"cost"`
	StopMotive    string  `json:"stop_motive"`
}

// PanelHistoryTotals add up the sessions the filter selects.
type PanelHistoryTotals struct {
	Sessions  int     `json:"sessions"`
	Minutes   int     `json:"minutes"`
	EnergyKWh float64 `json:"energy_kwh"`
	Cost      float64 `json:"cost"`
}

// endregion

// region Panel: cards and changes

// PanelCard is an RFID card of the pool.
type PanelCard struct {
	ID             string   `json:"id"`
	Alias          string   `json:"alias"`
	Email          string   `json:"email"`
	LinkedStations []string `json:"linked_stations"`
	Spent30Days    float64  `json:"spent_30_days"` // € of its sessions in the last 30 days
}

// PanelCardRequest creates or changes a card.
type PanelCardRequest struct {
	ID             string   `json:"id"`
	Alias          string   `json:"alias"`
	Email          string   `json:"email"`
	LinkedStations []string `json:"linked_stations"`
}

// PanelMaxPowerRequest changes the installation power.
type PanelMaxPowerRequest struct {
	MaxPowerW float64 `json:"max_power_w"`
}

// PanelModeRequest changes the load balancing mode.
type PanelModeRequest struct {
	Mode string `json:"mode"`
}

// PanelResetResult is a reset sent to a station.
type PanelResetResult struct {
	StationName string `json:"station_name"`
	SentAt      string `json:"sent_at"` // RFC 3339
}

// endregion
