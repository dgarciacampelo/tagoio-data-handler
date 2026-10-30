// This file holds the session audits page and its XLSX export.

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

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
