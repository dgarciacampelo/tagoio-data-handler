// This file holds the read-only TagoIO inspection APIs (package inspect), served twice: to the service credentials,
// for the `server tagoio` command run inside the container, and to the admin session.

package handlers

import (
	"errors"
	"net/http"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/inspect"
)

// TagoDevicesAPI lists the devices of the TagoIO account (GET /api/tagoio/devices).
func (a *App) TagoDevicesAPI(w http.ResponseWriter, r *http.Request) {
	report, err := a.Inspect.Devices(r.Context())
	if err != nil {
		a.tagoReadFailed(w, "devices", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// TagoPoolAPI reports a pool across TagoIO and the service (GET /api/tagoio/pools/{pool_code}).
func (a *App) TagoPoolAPI(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	if poolCode <= 0 {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "pool_code must be positive")
		return
	}
	report, err := a.Inspect.Pool(r.Context(), poolCode)
	if err != nil {
		a.tagoReadFailed(w, "pool", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// TagoUsersAPI lists the run users by email and/or group_id tag (GET /api/tagoio/users?email=…&group_id=…).
func (a *App) TagoUsersAPI(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	users, err := a.Inspect.Users(r.Context(), inspect.UserQuery{Email: query.Get("email"), GroupID: query.Get("group_id")})
	if errors.Is(err, inspect.ErrEmptyQuery) {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "email or group_id is required")
		return
	}
	if err != nil {
		a.tagoReadFailed(w, "users", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (a *App) tagoReadFailed(w http.ResponseWriter, what string, err error) {
	a.Log.Error("TagoIO inspection failed", "read", what, "error", err)
	writeAPIError(w, http.StatusBadGateway, dto.ErrorCodeServiceUnavailable, "TagoIO read failed: "+err.Error())
}
