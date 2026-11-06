// This file holds the page of a finished charging session and the API of its payments.

package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/csms"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/sessionview"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

const (
	sessionPageTitle = "Velo Energy - Sesión de recarga"
	// sessionMatchesLimit bounds the sessions listed for a transaction id: one per charge point that reached it.
	sessionMatchesLimit = 20
)

// SessionPage renders a finished session (GET /emsp-dashboard/sessions/{pool_code}/{station_name}/{connector_id}/
// {transaction_id}). When the history has no session there, it lists the sessions with that transaction id.
func (a *App) SessionPage(w http.ResponseWriter, r *http.Request) {
	key, ok := sessionKeyOf(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	session, err := a.Store.SessionByKey(r.Context(), key.poolCode, key.stationName, key.connectorID, key.transactionID)
	if errors.Is(err, store.ErrNotFound) {
		a.sessionMatches(w, r, key.transactionID, nil)
		return
	}
	if err != nil {
		a.Log.Error("Session page: reading the session failed", "transaction_id", key.transactionID, "error", err)
		a.page(w, "session", sessionPageTitle, nil)
		return
	}

	telemetry, err := a.Store.SessionTelemetry(r.Context(), session.PoolCode, session.StationName, session.ConnectorID,
		session.TransactionID)
	if err != nil {
		a.Log.Error("Session page: reading the meter values failed", "transaction_id", key.transactionID, "error", err)
		a.page(w, "session", sessionPageTitle, nil)
		return
	}
	detail := sessionview.Build(session, telemetry, a.location(), time.Now())
	detail.CPOName = a.Pools.Get(session.PoolCode).CPOName
	a.page(w, "session", sessionPageTitle, dto.SessionPage{
		TransactionID: session.TransactionID, Session: &detail, Matches: []dto.AuditSession{},
	})
}

// SessionByTransactionPage finds a finished session by its transaction id alone (GET /emsp-dashboard/sessions/
// {transaction_id}): it goes to the session when one charge point reached that id, and lists them otherwise.
func (a *App) SessionByTransactionPage(w http.ResponseWriter, r *http.Request) {
	transactionID, err := strconv.ParseInt(r.PathValue("transaction_id"), 10, 64)
	if err != nil || transactionID <= 0 {
		http.NotFound(w, r)
		return
	}
	a.sessionMatches(w, r, transactionID, func(sessions []store.SessionSummary) bool {
		if len(sessions) != 1 {
			return false
		}
		s := sessions[0]
		http.Redirect(w, r, sessionview.URL(s.PoolCode, s.StationName, s.ConnectorID, s.TransactionID), http.StatusFound)
		return true
	})
}

// sessionMatches renders the sessions with a transaction id, unless pick handles them first.
func (a *App) sessionMatches(w http.ResponseWriter, r *http.Request, transactionID int64, pick func([]store.SessionSummary) bool) {
	sessions, err := a.Store.SessionsByTransaction(r.Context(), transactionID, sessionMatchesLimit)
	if err != nil {
		a.Log.Error("Session page: listing the sessions of a transaction failed", "transaction_id", transactionID, "error", err)
		a.page(w, "session", sessionPageTitle, nil)
		return
	}
	if pick != nil && pick(sessions) {
		return
	}
	view := dto.SessionPage{TransactionID: transactionID, Matches: make([]dto.AuditSession, 0, len(sessions))}
	for _, s := range sessions {
		view.Matches = append(view.Matches, auditSession(s))
	}
	a.page(w, "session", sessionPageTitle, view)
}

// SessionPaymentsAPI returns the payments the CSMS stored for a finished session (GET /api/admin/sessions/{pool_code}/
// {station_name}/{connector_id}/{transaction_id}/payments). It asks the CSMS for the payments of the session's pool
// on its start day and the day before (a payment is created a few minutes before its session starts), and keeps
// those of its charge point and transaction.
func (a *App) SessionPaymentsAPI(w http.ResponseWriter, r *http.Request) {
	key, ok := sessionKeyOf(r)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "pool_code, connector_id and transaction_id must be integers")
		return
	}
	session, err := a.Store.SessionByKey(r.Context(), key.poolCode, key.stationName, key.connectorID, key.transactionID)
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Session not found")
		return
	}
	if err != nil {
		a.Log.Error("Session payments: reading the session failed", "transaction_id", key.transactionID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Session unavailable")
		return
	}
	day, err := time.ParseInLocation("02/01/2006", session.StartDate, a.location())
	if err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "The session has no valid start date")
		return
	}
	if !a.CSMS.Configured() {
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "CSMS_URLS is not set")
		return
	}

	result, err := a.CSMS.Payments(r.Context(), csms.PaymentsQuery{
		Since: day.AddDate(0, 0, -1), Until: day.AddDate(0, 0, 1), PoolCode: session.PoolCode, Limit: paymentsLimit,
	})
	if err != nil {
		a.Log.Error("Session payments: no CSMS answered", "transaction_id", key.transactionID, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "no CSMS answered")
		return
	}

	cpoName := a.Pools.Get(session.PoolCode).CPOName
	view := dto.SessionPayments{Payments: []dto.PaymentRecord{}}
	view.Source, view.Unanswered = a.csmsSources(result)
	for _, p := range result.Payments {
		if paysFor(p, session) {
			view.Payments = append(view.Payments, paymentRecord(p, cpoName))
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// paysFor reports whether a payment is for the session: same transaction and charge point (a payment that failed
// before its session started has no transaction, and a Payter one may have no connector).
func paysFor(p csms.Payment, s store.SessionSummary) bool {
	return p.TransactionID != nil && *p.TransactionID == s.TransactionID && p.StationName == s.StationName &&
		(p.ConnectorID == nil || *p.ConnectorID == s.ConnectorID)
}

// sessionKey identifies a finished session: transaction ids are only unique per charge point.
type sessionKey struct {
	poolCode      int
	stationName   string
	connectorID   int
	transactionID int64
}

func sessionKeyOf(r *http.Request) (sessionKey, bool) {
	poolCode, errPool := strconv.Atoi(r.PathValue("pool_code"))
	connectorID, errConnector := strconv.Atoi(r.PathValue("connector_id"))
	transactionID, errTransaction := strconv.ParseInt(r.PathValue("transaction_id"), 10, 64)
	station := r.PathValue("station_name")
	if errPool != nil || errConnector != nil || errTransaction != nil || station == "" {
		return sessionKey{}, false
	}
	return sessionKey{poolCode: poolCode, stationName: station, connectorID: connectorID, transactionID: transactionID}, true
}
