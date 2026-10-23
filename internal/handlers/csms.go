// This file holds the machine endpoints called by the CSMS (neos-ocpp) and the notification service. Their paths,
// bodies and answers are contracts of those services.

package handlers

import (
	"net/http"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/jsonx"
	"github.com/dgarciacampelo/tagoio-handler/internal/provisioning"
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
