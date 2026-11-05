// This file holds the machine endpoints called by the CSMS (neos-ocpp) and the notification service. Their paths,
// bodies and answers are contracts of those services.

package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/jsonx"
	"github.com/dgarciacampelo/tagoio-handler/internal/provisioning"
	"github.com/dgarciacampelo/tagoio-handler/internal/series"
)

// ChargePointUpdate applies a connector status change
// (POST /v1/charge-point-update/{pool_code}/{station_name}/{connector_id}).
func (a *App) ChargePointUpdate(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	station, ok := pathText(w, r, "station_name")
	if !ok {
		return
	}
	connectorID, ok := pathInt(w, r, "connector_id")
	if !ok {
		return
	}

	var update chargepoints.StatusUpdate
	if err := decodeJSON(w, r, &update); err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "Invalid charge point update: "+err.Error())
		return
	}
	update.PoolCode, update.StationName, update.ConnectorID = poolCode, station, jsonx.Int(connectorID) // The URL wins

	state, err := a.ChargePoints.UpdateStatus(r.Context(), update)
	if err != nil {
		a.Log.Error("Charge point update failed", "pool_code", poolCode, "station", station, "connector", connectorID, "error", err)
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// ChargingSessionUpdate applies a charging session update
// (POST /v1/charging-session-update/{pool_code}/{station_name}/{connector_id}).
func (a *App) ChargingSessionUpdate(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	station, ok := pathText(w, r, "station_name")
	if !ok {
		return
	}
	connectorID, ok := pathInt(w, r, "connector_id")
	if !ok {
		return
	}

	var update chargepoints.SessionUpdate
	if err := decodeJSON(w, r, &update); err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "Invalid session update: "+err.Error())
		return
	}
	update.PoolCode, update.StationName, update.ConnectorID = jsonx.Int(poolCode), station, jsonx.Int(connectorID)

	if err := a.ChargePoints.UpdateSession(r.Context(), update); err != nil {
		a.Log.Error("Charging session update failed", "pool_code", poolCode, "transaction_id", int64(update.TransactionID), "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Session update not stored")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// PoolSetup returns a pool's full setup for the CSMS, creating its TagoIO device when it has none
// (GET /api/pools/{pool_code}).
func (a *App) PoolSetup(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	if poolCode <= 0 {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "pool_code must be positive")
		return
	}

	_, created, err := a.Provisioning.EnsureDevice(r.Context(), poolCode)
	if err != nil {
		a.Log.Error("Pool setup: device unavailable", "pool_code", poolCode, "error", err)
		writeAPIError(w, http.StatusBadGateway, dto.ErrorCodeServiceUnavailable, "TagoIO device unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.Pools.FetchSetup(r.Context(), poolCode, created))
}

// provisioningResponse is the answer of POST /api/provision-cpo.
//
// ! Wire contract: neos-notification-mgmt reads it (create_user.py there).
type provisioningResponse struct {
	UserCreated       bool              `json:"user_created"`
	DeviceCreated     bool              `json:"device_created"`
	PoolCode          *int              `json:"pool_code"`
	DeviceID          *string           `json:"device_id"`
	GeneratedPassword *string           `json:"generated_password"`
	FormData          provisioning.Form `json:"form_data"` // Echoed with the Spanish form keys
	ErrorMessage      *string           `json:"error_message"`
}

// ProvisionCPO creates the TagoIO user and pool device of a new CPO (POST /api/provision-cpo). It always answers
// 200 with the outcome, except for an invalid form.
func (a *App) ProvisionCPO(w http.ResponseWriter, r *http.Request) {
	var form provisioning.Form
	if err := decodeJSON(w, r, &form); err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "Invalid provisioning form: "+err.Error())
		return
	}

	result, err := a.Provisioning.ProvisionCPO(r.Context(), form)
	response := provisioningResponse{FormData: form}
	if err != nil {
		a.Log.Error("CPO provisioning failed", "error", err)
		message := "Provisioning failed: " + err.Error()
		response.ErrorMessage = &message
		writeJSON(w, http.StatusOK, response)
		return
	}

	response.UserCreated, response.DeviceCreated = result.UserCreated, result.DeviceCreated
	response.PoolCode = &result.PoolCode
	response.GeneratedPassword = &result.Password
	if result.DeviceID != "" {
		response.DeviceID = &result.DeviceID
	}
	if result.ErrorMessage != "" {
		response.ErrorMessage = &result.ErrorMessage
	}
	writeJSON(w, http.StatusOK, response)
}

// SessionSeries returns the power and energy series of a charging session, which the email of a finished session
// charts (GET /api/sessions/{pool_code}/{station_name}/{connector_id}/{transaction_id}/series). The CSMS asks for it
// before it posts the session's last meter value, so it passes the readings the telemetry doesn't hold yet:
// started_at (the session's start, with nothing delivered), and stopped_at with energy (the final reading, Wh
// delivered since the start). It answers 404 when the session has nothing to chart: the email then has no chart.
func (a *App) SessionSeries(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	station, ok := pathText(w, r, "station_name")
	if !ok {
		return
	}
	connectorID, ok := pathInt(w, r, "connector_id")
	if !ok {
		return
	}
	transactionID, ok := pathInt(w, r, "transaction_id")
	if !ok {
		return
	}
	added, err := seriesReadings(r.URL.Query())
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, err.Error())
		return
	}

	telemetry, err := a.Store.SessionTelemetry(r.Context(), poolCode, station, connectorID, int64(transactionID))
	if err != nil {
		a.Log.Error("Session series: reading telemetry failed", "pool_code", poolCode, "transaction_id", transactionID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Telemetry unavailable")
		return
	}
	if len(telemetry) == 0 {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "No telemetry stored for this session")
		return
	}
	s, err := series.Build(append(series.FromTelemetry(telemetry), added...))
	if errors.Is(err, series.ErrNoSeries) {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "No energy delivery to chart for this session")
		return
	}
	if err != nil {
		a.Log.Error("Session series failed", "pool_code", poolCode, "transaction_id", transactionID, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Series unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// seriesReadings reads the readings the CSMS adds to the stored ones: started_at, with no energy delivered yet, and
// stopped_at with energy, the final reading. Both are optional; the timestamps are ISO 8601 with their UTC offset.
func seriesReadings(query url.Values) ([]series.Reading, error) {
	var readings []series.Reading
	if value := query.Get("started_at"); value != "" {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, errors.New("started_at must be an ISO 8601 timestamp with its UTC offset")
		}
		readings = append(readings, series.Reading{At: at})
	}

	stoppedAt, energy := query.Get("stopped_at"), query.Get("energy")
	if (stoppedAt == "") != (energy == "") {
		return nil, errors.New("stopped_at and energy go together")
	}
	if stoppedAt != "" {
		at, err := time.Parse(time.RFC3339Nano, stoppedAt)
		if err != nil {
			return nil, errors.New("stopped_at must be an ISO 8601 timestamp with its UTC offset")
		}
		wh, err := strconv.ParseInt(energy, 10, 64)
		if err != nil || wh < 0 {
			return nil, errors.New("energy must be a whole, non-negative number of Wh")
		}
		readings = append(readings, series.Reading{At: at, Energy: wh})
	}
	return readings, nil
}
