// This file holds the admin correction API of a pool's TagoIO access: linking a pool to an existing run user.

package handlers

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/provisioning"
)

// linkPoolUserInput is the body of POST /api/admin/pools/{pool_code}/users.
type linkPoolUserInput struct {
	Email string `json:"email"`
}

// linkPoolUserResult is its answer.
type linkPoolUserResult struct {
	PoolCode      int      `json:"pool_code"`
	DeviceID      string   `json:"device_id"`
	DeviceCreated bool     `json:"device_created"` // The pool had no MASTER device: one was created, without configuration
	UserID        string   `json:"user_id"`
	TagAdded      bool     `json:"tag_added"` // false when the user already had the pool's group_id tag
	GroupIDs      []string `json:"group_ids"` // Every group_id tag of the user now
}

// LinkPoolUserAPI gives the existing TagoIO run user with that email access to a pool: it ensures the pool's MASTER
// device and adds the pool's group_id tag to the user, keeping its other tags (POST
// /api/admin/pools/{pool_code}/users, body {"email": "..."}). It fixes a sign-up whose email already had a user (a
// second pool of the same CPO). Calling it again is harmless.
func (a *App) LinkPoolUserAPI(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	if poolCode <= 0 {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "pool_code must be positive")
		return
	}
	var input linkPoolUserInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid JSON body: "+err.Error())
		return
	}
	email := strings.TrimSpace(input.Email)
	if address, err := mail.ParseAddress(email); err != nil || address.Address != email {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "email must be a plain email address")
		return
	}

	result, err := a.Provisioning.LinkPool(r.Context(), poolCode, email)
	if errors.Is(err, provisioning.ErrUserNotFound) {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "No TagoIO user has that email")
		return
	}
	if err != nil {
		a.Log.Error("Pool user link failed", "pool_code", poolCode, "error", err)
		writeAPIError(w, http.StatusBadGateway, dto.ErrorCodeServiceUnavailable, "Pool not linked: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, linkPoolUserResult{
		PoolCode: poolCode, DeviceID: result.DeviceID, DeviceCreated: result.DeviceCreated, UserID: result.UserID,
		TagAdded: result.TagAdded, GroupIDs: result.GroupIDs,
	})
}
