// Package csms reads from the CSMS instances (neos-ocpp) over their HTTP API. Both production CSMS share one
// database, so they answer with the same data: the client asks them in order and keeps the first answer, which also
// covers one of them being down or not deployed with the API yet.
package csms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/config"
)

const (
	requestTimeout = 10 * time.Second
	maxBodyBytes   = 16 << 20
)

var (
	// ErrNotConfigured means CSMS_URLS is empty.
	ErrNotConfigured = errors.New("csms: no CSMS URL configured")
	// ErrBadQuery means a CSMS rejected the query (HTTP 400): the others would too.
	ErrBadQuery = errors.New("csms: query rejected")
)

// Payment is a payment as GET /api/payments serves it, without personal data. Amounts are in cents.
//
// ! Wire contract: neos-ocpp builds it in local_database/payment_history.py (list_payments).
type Payment struct {
	ID            int64   `json:"id"`
	CreatedAt     string  `json:"created_at"` // ISO 8601 with offset
	UpdatedAt     string  `json:"updated_at"`
	PoolCode      int     `json:"pool_code"`
	StationName   string  `json:"station_name"`
	ConnectorID   *int    `json:"connector_id"`
	Platform      string  `json:"platform"` // PAYCOMET or PAYTER
	Category      string  `json:"category"` // LIVE or TEST
	Status        string  `json:"status"`   // PENDING, AUTHORIZED, COMMITTED, CANCELED or ERROR
	Limit         int     `json:"limit"`
	Amount        *int    `json:"amount"`
	Currency      string  `json:"currency"`
	TransactionID *int64  `json:"transaction_id"`
	SessionEnergy *int    `json:"session_energy"` // Wh
	SessionCost   *string `json:"session_cost"`   // €, as text
	Order         *string `json:"order"`          // Paycomet order
	CardBrand     *string `json:"card_brand"`
	Terminal      *string `json:"terminal"` // Payter terminal serial number
	ErrorStage    *string `json:"error_stage"`
	ErrorCode     *int    `json:"error_code"`
	ErrorMessage  *string `json:"error_message"`
	ErrorAt       *string `json:"error_at"`
}

// PaymentsQuery selects the payments created in [Since, Until), of one pool when PoolCode isn't 0.
type PaymentsQuery struct {
	Since, Until time.Time
	PoolCode     int
	Limit        int // 0: the CSMS default
}

// PaymentsResult is the answer of the first CSMS that served the query.
type PaymentsResult struct {
	Payments  []Payment
	Truncated bool      // More payments matched than the limit
	Source    string    // Base URL of the CSMS that answered
	Failures  []Failure // The CSMS asked before Source, which didn't answer, in order
}

// Failure is a CSMS that didn't answer a query. The data is the same from another one, as they share the database,
// but a CSMS that never answers is a deployment fault to fix (its name, port or network).
type Failure struct {
	Source string // Base URL
	Err    error
}

// Client reads from the CSMS instances with the service credentials, the pair they send to this service.
type Client struct {
	urls  []string
	creds config.Credentials
	http  *http.Client
}

// New creates a client of the CSMS at urls, tried in order.
func New(urls []string, creds config.Credentials, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{urls: urls, creds: creds, http: httpClient}
}

// Configured reports whether there is any CSMS to ask.
func (c *Client) Configured() bool {
	return c != nil && len(c.urls) > 0
}

// Payments returns the payments of the first CSMS that answers, with the failures of those asked before it. A
// rejected query isn't sent to the next one.
func (c *Client) Payments(ctx context.Context, q PaymentsQuery) (PaymentsResult, error) {
	if !c.Configured() {
		return PaymentsResult{}, ErrNotConfigured
	}
	var failures []Failure
	for _, base := range c.urls {
		result, err := c.payments(ctx, base, q)
		if err == nil {
			result.Source, result.Failures = base, failures
			return result, nil
		}
		if errors.Is(err, ErrBadQuery) || ctx.Err() != nil {
			return PaymentsResult{}, err
		}
		failures = append(failures, Failure{Source: base, Err: err})
	}
	errs := make([]error, len(failures))
	for i, f := range failures {
		errs[i] = fmt.Errorf("%s: %w", f.Source, f.Err)
	}
	return PaymentsResult{}, errors.Join(errs...)
}

func (c *Client) payments(ctx context.Context, base string, q PaymentsQuery) (PaymentsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	params := url.Values{}
	params.Set("since", q.Since.Format(time.RFC3339))
	params.Set("until", q.Until.Format(time.RFC3339))
	if q.PoolCode != 0 {
		params.Set("pool_code", strconv.Itoa(q.PoolCode))
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/payments?"+params.Encode(), nil)
	if err != nil {
		return PaymentsResult{}, err
	}
	req.SetBasicAuth(c.creds.User, c.creds.Secret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return PaymentsResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return PaymentsResult{}, err
	}

	if resp.StatusCode != http.StatusOK {
		var answer struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &answer)
		if resp.StatusCode == http.StatusBadRequest {
			return PaymentsResult{}, fmt.Errorf("%w: %s", ErrBadQuery, answer.Error)
		}
		return PaymentsResult{}, errors.New(strings.TrimSpace(fmt.Sprintf("HTTP %d %s", resp.StatusCode, answer.Error)))
	}

	var answer struct {
		Payments  []Payment `json:"payments"`
		Truncated bool      `json:"truncated"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return PaymentsResult{}, fmt.Errorf("decoding the payments: %w", err)
	}
	if answer.Payments == nil {
		answer.Payments = []Payment{}
	}
	return PaymentsResult{Payments: answer.Payments, Truncated: answer.Truncated}, nil
}
