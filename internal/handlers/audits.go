// This file holds the session audits page and its XLSX export.

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/export"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

// auditsLimit is the number of latest sessions listed.
const auditsLimit = 100

// AuditsPage renders the latest finished sessions of every pool (GET /emsp-dashboard/audits) or of one pool
// (GET /emsp-dashboard/audits/{pool_code}).
func (a *App) AuditsPage(w http.ResponseWriter, r *http.Request) {
	poolCode := 0
	if text := r.PathValue("pool_code"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		poolCode = n
	}

	sessions, err := a.Store.RecentSessions(r.Context(), poolCode, auditsLimit)
	if err != nil {
		a.Log.Error("Audits: reading sessions failed", "error", err)
		a.page(w, "audits", "Velo Energy - Auditoría de Sesiones", nil)
		return
	}
	view := dto.AuditsPage{PoolCode: poolCode, Sessions: make([]dto.AuditSession, 0, len(sessions))}
	for _, s := range sessions {
		view.Sessions = append(view.Sessions, dto.AuditSession{
			TransactionID: s.TransactionID, PoolCode: s.PoolCode, StationName: s.StationName, ConnectorID: s.ConnectorID,
			CardAlias: s.CardAlias, StartDate: s.StartDate, TimeBand: s.TimeBand,
			TotalEnergyKWh: s.TotalEnergyKWh, Cost: s.Cost,
			ExportURL: fmt.Sprintf("/api/admin/audits/%d/export", s.ID),
			OffPeakWh: s.EnergyOffPeak, FlatWh: s.EnergyFlat, PeakWh: s.EnergyPeak,
			RateOffPeak: s.RateOffPeak, RateFlat: s.RateFlat, RatePeak: s.RatePeak,
		})
	}
	a.page(w, "audits", "Velo Energy - Auditoría de Sesiones", view)
}

// AuditExport downloads the audit workbook of a session (GET /api/admin/audits/{session_id}/export). The session is
// the history row (SessionSummary.ID), since transaction ids are only unique per charge point.
func (a *App) AuditExport(w http.ResponseWriter, r *http.Request) {
	sessionID, err := strconv.ParseInt(r.PathValue("session_id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "session_id must be an integer")
		return
	}
	session, err := a.Store.Session(r.Context(), sessionID)
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Session not found")
		return
	}
	if err != nil {
		a.Log.Error("Audit export: reading session failed", "session_id", sessionID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Session unavailable")
		return
	}
	transactionID := session.TransactionID
	telemetry, err := a.Store.SessionTelemetry(r.Context(), session.PoolCode, session.StationName, session.ConnectorID, transactionID)
	if err != nil {
		a.Log.Error("Audit export: reading telemetry failed", "transaction_id", transactionID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Telemetry unavailable")
		return
	}
	if len(telemetry) == 0 {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "No telemetry stored for this session")
		return
	}

	workbook, err := export.SessionAudit(session, telemetry)
	if err != nil {
		a.Log.Error("Audit export failed", "transaction_id", transactionID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Export failed")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="charging_session_audit_%d.xlsx"`, transactionID))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(workbook)
}

// maxCardAliasLength bounds a corrected card alias (the CSMS sends RFID aliases and "TPV Virtual"-like labels).
const maxCardAliasLength = 64

// CorrectAuditCardAliasAPI corrects the card alias of a finished session (PUT
// /api/admin/audits/{session_id}/card-alias): in the audits and in the session's record of its pool's TagoIO
// history, so both dashboards show it. Repeating it is harmless, so a failed TagoIO edit is retried by calling it
// again.
func (a *App) CorrectAuditCardAliasAPI(w http.ResponseWriter, r *http.Request) {
	sessionID, err := strconv.ParseInt(r.PathValue("session_id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "session_id must be an integer")
		return
	}
	var input dto.AuditCardAliasInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid JSON body: "+err.Error())
		return
	}
	alias := strings.TrimSpace(input.CardAlias)
	if alias == "" || utf8.RuneCountInString(alias) > maxCardAliasLength {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest,
			fmt.Sprintf("card_alias must have 1 to %d characters", maxCardAliasLength))
		return
	}

	session, err := a.Store.Session(r.Context(), sessionID)
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Session not found")
		return
	}
	if err == nil {
		err = a.Store.UpdateSessionCardAlias(r.Context(), sessionID, alias)
	}
	if err != nil {
		a.Log.Error("Audit card alias: updating the session failed", "session_id", sessionID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Session not updated")
		return
	}

	// The transaction id is only unique per charge point, so a record of another session is left as it is
	group := strconv.FormatInt(session.TransactionID, 10)
	edited, err := a.Bucket.EditGroupMetadata(r.Context(), session.PoolCode, chargepoints.HistoryVariable, group,
		func(metadata map[string]any) bool {
			if fmt.Sprint(metadata["start_date"]) != session.StartDate || fmt.Sprint(metadata["time_band"]) != session.TimeBand {
				return false
			}
			metadata["card_alias"] = alias
			return true
		})
	if err != nil {
		a.Log.Error("Audit card alias: editing the TagoIO history failed", "session_id", sessionID,
			"pool_code", session.PoolCode, "transaction_id", session.TransactionID, "error", err)
		writeAPIError(w, http.StatusBadGateway, dto.ErrorCodeServiceUnavailable,
			"The audits show the new alias, the TagoIO history doesn't yet: try again")
		return
	}

	a.Log.Info("Audit card alias corrected", "session_id", sessionID, "pool_code", session.PoolCode,
		"transaction_id", session.TransactionID, "from", session.CardAlias, "to", alias, "tagoio_records", edited)
	writeJSON(w, http.StatusOK, dto.AuditCardAliasResult{
		SessionID: sessionID, TransactionID: session.TransactionID, PoolCode: session.PoolCode,
		CardAlias: alias, TagoIORecords: edited,
	})
}
