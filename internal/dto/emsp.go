// This file holds the contract of the admin pages: the eMSP dashboard, the QR posters and the session audits.

package dto

// EmspPage is the view model of the eMSP dashboard.
type EmspPage struct {
	Pools []EmspPool `json:"pools"`
}

// EmspPool is a charging pool with its known stations.
type EmspPool struct {
	PoolCode  int           `json:"pool_code"`
	CPOName   string        `json:"cpo_name"`
	AuditsURL string        `json:"audits_url"`
	Stations  []EmspStation `json:"stations"`
}

// EmspStation is a station of a pool.
type EmspStation struct {
	Name       string          `json:"name"`
	Connectors []EmspConnector `json:"connectors"`
	Link       StationLink     `json:"link"`
}

// EmspConnector is a connector of a station.
type EmspConnector struct {
	ID     int             `json:"id"`
	Status ConnectorStatus `json:"status"`
}

// StationLink is the public page of a station and its QR code (GET /api/admin/station-link for a station that
// has not reported yet).
type StationLink struct {
	PoolCode    int    `json:"pool_code"`
	StationName string `json:"station_name"`
	CPOName     string `json:"cpo_name"`
	URL         string `json:"url"`          // Public page (short link)
	QRImageURL  string `json:"qr_image_url"` // PNG
	PosterA5URL string `json:"poster_a5_url"`
	PosterA4URL string `json:"poster_a4_url"`
}

// PosterSize is the paper size of a QR poster.
type PosterSize string

const (
	PosterSizeA5 PosterSize = "a5"
	PosterSizeA4 PosterSize = "a4"
)

// PosterPage is the view model of a printable QR poster.
type PosterPage struct {
	Size PosterSize  `json:"size"`
	Link StationLink `json:"link"`
}

// AuditsPage is the view model of the session audits.
type AuditsPage struct {
	PoolCode int            `json:"pool_code"` // 0: every pool
	Sessions []AuditSession `json:"sessions"`
}

// AuditSession is a finished session with its frozen rates and per-band energy.
type AuditSession struct {
	TransactionID  int64   `json:"transaction_id"`
	PoolCode       int     `json:"pool_code"`
	StationName    string  `json:"station_name"`
	ConnectorID    int     `json:"connector_id"`
	CardAlias      string  `json:"card_alias"`
	StartDate      string  `json:"start_date"`
	TimeBand       string  `json:"time_band"`
	TotalEnergyKWh float64 `json:"total_energy_kwh"`
	Cost           float64 `json:"cost"`
	ExportURL      string  `json:"export_url"` // XLSX with every meter value
	OffPeakWh      int64   `json:"off_peak_wh"`
	FlatWh         int64   `json:"flat_wh"`
	PeakWh         int64   `json:"peak_wh"`
	RateOffPeak    float64 `json:"rate_off_peak"` // €/kWh
	RateFlat       float64 `json:"rate_flat"`
	RatePeak       float64 `json:"rate_peak"`
}
