// This file holds the payments page of the eMSP dashboard and its API. The payments live in the CSMS database, so
// each request asks the CSMS (GET /api/payments, without personal data): the first one that answers, as both
// production CSMS share that database.

package handlers

import (
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/csms"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

const (
	paymentsPageTitle = "Velo Energy - Pagos"
	paymentsMaxDays   = 366  // Widest range of one request
	paymentsLimit     = 2000 // The most the CSMS serves at once
	dateLayout        = "2006-01-02"
)

// PaymentsPage renders the payment history (GET /emsp-dashboard/payments). The page asks the API for the rows.
func (a *App) PaymentsPage(w http.ResponseWriter, _ *http.Request) {
	view := dto.PaymentsPage{
		Configured: a.CSMS.Configured(),
		Today:      time.Now().In(a.location()).Format(dateLayout),
		Pools:      []dto.PaymentsPool{},
	}
	for _, poolCode := range slices.Sorted(maps.Keys(a.ChargePoints.StationStatuses())) {
		view.Pools = append(view.Pools, dto.PaymentsPool{PoolCode: poolCode, CPOName: a.Pools.Get(poolCode).CPOName})
	}
	a.page(w, "payments", paymentsPageTitle, view)
}

// PaymentsAPI lists the payments created from one day to another, both included (GET /api/admin/payments).
func (a *App) PaymentsAPI(w http.ResponseWriter, r *http.Request) {
	query, ok := a.paymentsQuery(w, r.URL.Query())
	if !ok {
		return
	}
	if !a.CSMS.Configured() {
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "CSMS_URLS is not set")
		return
	}

	result, err := a.CSMS.Payments(r.Context(), query)
	if errors.Is(err, csms.ErrBadQuery) {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, err.Error())
		return
	}
	if err != nil {
		a.Log.Error("Payments: no CSMS answered", "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "no CSMS answered")
		return
	}

	cpoNames := map[int]string{}
	view := dto.PaymentsResult{Payments: make([]dto.PaymentRecord, 0, len(result.Payments)), Truncated: result.Truncated}
	view.Source, view.Unanswered = a.csmsSources(result)
	for _, p := range result.Payments {
		name, known := cpoNames[p.PoolCode]
		if !known {
			name = a.Pools.Get(p.PoolCode).CPOName
			cpoNames[p.PoolCode] = name
		}
		view.Payments = append(view.Payments, paymentRecord(p, name))
	}
	writeJSON(w, http.StatusOK, view)
}

// paymentsQuery reads from, to (Europe/Madrid days, both included) and the optional pool_code.
func (a *App) paymentsQuery(w http.ResponseWriter, values url.Values) (csms.PaymentsQuery, bool) {
	location := a.location()
	from, errFrom := time.ParseInLocation(dateLayout, values.Get("from"), location)
	to, errTo := time.ParseInLocation(dateLayout, values.Get("to"), location)
	if errFrom != nil || errTo != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "from and to must be dates (2006-01-02)")
		return csms.PaymentsQuery{}, false
	}
	until := to.AddDate(0, 0, 1) // Midnight after the last day, whatever the DST change
	if !from.Before(until) || until.Sub(from) > paymentsMaxDays*24*time.Hour+time.Hour {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "from must not be after to, nor a year before it")
		return csms.PaymentsQuery{}, false
	}

	query := csms.PaymentsQuery{Since: from, Until: until, Limit: paymentsLimit}
	if raw := values.Get("pool_code"); raw != "" {
		poolCode, err := strconv.Atoi(raw)
		if err != nil || poolCode <= 0 {
			writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "pool_code must be a pool code")
			return csms.PaymentsQuery{}, false
		}
		query.PoolCode = poolCode
	}
	return query, true
}

// csmsSources returns the host of the CSMS that answered and those of the ones asked before it, logging why each of
// those didn't answer. The data is the same from any of them, but a CSMS that never answers is a deployment fault
// (its name, port or network), which the fallback would otherwise hide.
func (a *App) csmsSources(result csms.PaymentsResult) (source string, unanswered []string) {
	source = hostOf(result.Source)
	unanswered = make([]string, 0, len(result.Failures))
	for _, f := range result.Failures {
		host := hostOf(f.Source)
		a.Log.Warn("CSMS didn't answer, another one did", "csms", host, "answered", source, "error", f.Err)
		unanswered = append(unanswered, host)
	}
	return source, unanswered
}

// hostOf is the host and port of a CSMS base URL, which the pages show.
func hostOf(base string) string {
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host
	}
	return base
}

// location is the time zone of the dates people pick: Europe/Madrid, or UTC when the configuration has none.
func (a *App) location() *time.Location {
	if a.Config != nil && a.Config.Location != nil {
		return a.Config.Location
	}
	return time.UTC
}

func paymentRecord(p csms.Payment, cpoName string) dto.PaymentRecord {
	return dto.PaymentRecord{
		ID: p.ID, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, PoolCode: p.PoolCode, CPOName: cpoName,
		StationName: p.StationName, ConnectorID: p.ConnectorID, Platform: p.Platform, Category: p.Category,
		Status: p.Status, Limit: p.Limit, Amount: p.Amount, Currency: p.Currency, TransactionID: p.TransactionID,
		SessionEnergy: p.SessionEnergy, SessionCost: p.SessionCost, Order: p.Order, CardBrand: p.CardBrand,
		Terminal: p.Terminal, ErrorStage: p.ErrorStage, ErrorCode: p.ErrorCode, ErrorMessage: p.ErrorMessage,
		ErrorAt: p.ErrorAt,
	}
}
