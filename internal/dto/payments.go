// This file holds the contract of the payments page of the eMSP dashboard: the payment history the CSMS serves,
// without personal data (no email, receipt, card hash or masked PAN).

package dto

// PaymentsPage is the view model of the payments page.
type PaymentsPage struct {
	Configured bool           `json:"configured"` // CSMS_URLS is set
	Today      string         `json:"today"`      // Europe/Madrid, "2006-01-02": the default range ends today
	Pools      []PaymentsPool `json:"pools"`      // Pool filter: the pools whose stations reported
}

// PaymentsPool is an option of the pool filter.
type PaymentsPool struct {
	PoolCode int    `json:"pool_code"`
	CPOName  string `json:"cpo_name"`
}

// PaymentsResult is the answer of GET /api/admin/payments?from=2006-01-02&to=2006-01-02[&pool_code=221006]: the
// payments created on those days (Europe/Madrid), newest first.
type PaymentsResult struct {
	Payments   []PaymentRecord `json:"payments"`
	Truncated  bool           `json:"truncated"`  // More payments matched than the CSMS serves at once
	Source     string          `json:"source"`     // Host of the CSMS that answered
	Unanswered []string        `json:"unanswered"` // Hosts of the CSMS asked before it, which didn't answer (logged why)
}

// PaymentRecord is a payment of the history. Amounts are in cents.
type PaymentRecord struct {
	ID            int64   `json:"id"`
	CreatedAt     string  `json:"created_at"` // ISO 8601 with offset
	UpdatedAt     string  `json:"updated_at"`
	PoolCode      int     `json:"pool_code"`
	CPOName       string  `json:"cpo_name"`
	StationName   string  `json:"station_name"`
	ConnectorID   *int    `json:"connector_id" tstype:"number | null,required"`
	Platform      string  `json:"platform"` // PAYCOMET or PAYTER
	Category      string  `json:"category"` // LIVE or TEST
	Status        string  `json:"status"`   // PENDING, AUTHORIZED, COMMITTED, CANCELED or ERROR
	Limit         int     `json:"limit"`    // Preauthorized
	Amount        *int    `json:"amount" tstype:"number | null,required"`
	Currency      string  `json:"currency"`
	TransactionID *int64  `json:"transaction_id" tstype:"number | null,required"`
	SessionEnergy *int    `json:"session_energy" tstype:"number | null,required"` // Wh
	SessionCost   *string `json:"session_cost" tstype:"string | null,required"`   // €, as the session stores it
	Order         *string `json:"order" tstype:"string | null,required"`          // Paycomet order (back office search)
	CardBrand     *string `json:"card_brand" tstype:"string | null,required"`
	Terminal      *string `json:"terminal" tstype:"string | null,required"` // Payter terminal serial number
	ErrorStage    *string `json:"error_stage" tstype:"string | null,required"`
	ErrorCode     *int    `json:"error_code" tstype:"number | null,required"`
	ErrorMessage  *string `json:"error_message" tstype:"string | null,required"`
	ErrorAt       *string `json:"error_at" tstype:"string | null,required"`
}
