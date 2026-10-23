// This file holds the JSON helpers shared by every handler.

package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

const maxJSONBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, code dto.ErrorCode, message string) {
	writeJSON(w, status, dto.APIError{Code: code, Message: message})
}

// decodeJSON reads a JSON body (at most 1 MiB, nothing after the value).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("unexpected data after the JSON body")
	}
	return nil
}

// pathInt reads an integer path value; it answers 400 and reports false when it is not one.
func pathInt(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, name+" must be an integer")
		return 0, false
	}
	return n, true
}

// pathText reads a non-empty path value; it answers 400 and reports false when it is empty.
func pathText(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := strings.TrimSpace(r.PathValue(name))
	if value == "" {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, name+" is required")
		return "", false
	}
	return value, true
}
