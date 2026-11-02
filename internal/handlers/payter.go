// This file holds the Payter terminals page of the eMSP dashboard and its APIs. Every change goes to the CSMS
// instances as a manage_payter_terminal event (only the Payter CSMS applies it), and the Payter CSMS reads the whole
// list at startup (GET /api/payter-terminals), since the events sent while it was down are lost.

package handlers

import (
	"errors"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

const payterPageTitle = "Velo Energy - Terminales Payter"

// payterSerialPattern matches a Payter P6x serial number, as the CSMS validates it.
var payterSerialPattern = regexp.MustCompile(`^P6X[0-9A-Z]+$`)

// PayterPage renders the Payter terminals (GET /emsp-dashboard/payter).
func (a *App) PayterPage(w http.ResponseWriter, r *http.Request) {
	terminals, err := a.Store.PayterTerminals(r.Context())
	if err != nil {
		a.Log.Error("Payter terminals: reading failed", "error", err)
		a.page(w, "payter", payterPageTitle, nil)
		return
	}

	pools := map[int]*dto.PayterPool{}
	pool := func(poolCode int) *dto.PayterPool {
		if p, ok := pools[poolCode]; ok {
			return p
		}
		p := &dto.PayterPool{PoolCode: poolCode, Stations: []string{}}
		if config := a.Pools.Get(poolCode); config.Loaded {
			p.CPOName = config.CPOName
		}
		pools[poolCode] = p
		return p
	}
	for poolCode, stations := range a.ChargePoints.StationStatuses() {
		p := pool(poolCode)
		for name, connectors := range stations {
			if len(connectors) > 0 { // Only connector 0 (the station itself) reported
				p.Stations = append(p.Stations, name)
			}
		}
		slices.Sort(p.Stations)
	}
	for _, t := range terminals {
		pool(t.PoolCode)
	}

	view := dto.PayterPage{Terminals: payterTerminalsDTO(terminals), Pools: []dto.PayterPool{}}
	for _, poolCode := range slices.Sorted(maps.Keys(pools)) {
		view.Pools = append(view.Pools, *pools[poolCode])
	}
	a.page(w, "payter", payterPageTitle, view)
}

// PayterTerminalsAPI lists every terminal, active or not, for the CSMS (GET /api/payter-terminals).
func (a *App) PayterTerminalsAPI(w http.ResponseWriter, r *http.Request) {
	terminals, err := a.Store.PayterTerminals(r.Context())
	if err != nil {
		a.Log.Error("Payter terminals: reading failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Payter terminals unavailable")
		return
	}
	writeJSON(w, http.StatusOK, payterTerminalsDTO(terminals))
}

// SavePayterTerminalAPI creates or updates a terminal (PUT /api/admin/payter-terminals/{serial_number}) and answers
// with every terminal.
func (a *App) SavePayterTerminalAPI(w http.ResponseWriter, r *http.Request) {
	serial, ok := payterSerial(w, r)
	if !ok {
		return
	}
	var input dto.PayterTerminalInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid JSON body: "+err.Error())
		return
	}
	if input.PoolCode < 100000 || input.PoolCode > 999999 {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "pool_code must have 6 digits")
		return
	}
	stations, problem := payterStations(input)
	if problem != "" {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, problem)
		return
	}

	moved, err := a.Store.SavePayterTerminal(r.Context(), store.PayterTerminal{
		SerialNumber: serial, PoolCode: input.PoolCode, IsPoolShared: input.IsPoolShared, LinkedStations: stations,
	})
	if errors.Is(err, store.ErrSharedTerminalExists) {
		writeAPIError(w, http.StatusConflict, dto.ErrorCodeConflict, err.Error())
		return
	}
	if err != nil {
		a.Log.Error("Payter terminal: saving failed", "serial_number", serial, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Payter terminal not saved")
		return
	}

	a.Broker.Publish(events.PayterTerminalManagement{
		Header:       events.NewHeader(events.TypeManagePayterTerminal, input.PoolCode),
		SerialNumber: serial, Action: events.PayterTerminalCreate, IsPoolShared: input.IsPoolShared, LinkedStations: stations,
	})
	a.Log.Info("Payter terminal saved", "serial_number", serial, "pool_code", input.PoolCode,
		"pool_shared", input.IsPoolShared, "stations", stations, "moved_from", moved)
	a.writePayterTerminals(w, r)
}

// DeactivatePayterTerminalAPI deactivates a terminal (DELETE /api/admin/payter-terminals/{serial_number}) and
// answers with every terminal. The terminal is kept: the CSMS keeps the payments made on it.
func (a *App) DeactivatePayterTerminalAPI(w http.ResponseWriter, r *http.Request) {
	serial, ok := payterSerial(w, r)
	if !ok {
		return
	}
	poolCode, found, err := a.Store.DeactivatePayterTerminal(r.Context(), serial)
	switch {
	case err != nil:
		a.Log.Error("Payter terminal: deactivation failed", "serial_number", serial, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Payter terminal not deactivated")
		return
	case !found:
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Unknown Payter terminal")
		return
	}

	a.Broker.Publish(events.PayterTerminalManagement{
		Header:       events.NewHeader(events.TypeManagePayterTerminal, poolCode),
		SerialNumber: serial, Action: events.PayterTerminalDelete, LinkedStations: []string{},
	})
	a.Log.Info("Payter terminal deactivated", "serial_number", serial, "pool_code", poolCode)
	a.writePayterTerminals(w, r)
}

func (a *App) writePayterTerminals(w http.ResponseWriter, r *http.Request) {
	terminals, err := a.Store.PayterTerminals(r.Context())
	if err != nil {
		a.Log.Error("Payter terminals: reading failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Payter terminals unavailable")
		return
	}
	writeJSON(w, http.StatusOK, payterTerminalsDTO(terminals))
}

// payterSerial reads the serial number of the path, in upper case; it answers 422 and reports false when invalid.
func payterSerial(w http.ResponseWriter, r *http.Request) (string, bool) {
	serial := strings.ToUpper(strings.TrimSpace(r.PathValue("serial_number")))
	if !payterSerialPattern.MatchString(serial) {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "serial_number must be a Payter P6X serial number")
		return "", false
	}
	return serial, true
}

// payterStations returns the stations a terminal serves, without blanks or repeats (none when it is shared by the
// pool), or the reason they are invalid.
func payterStations(input dto.PayterTerminalInput) ([]string, string) {
	if input.IsPoolShared {
		return []string{}, ""
	}
	stations := []string{}
	for _, name := range input.LinkedStations {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(stations, name) {
			continue
		}
		if strings.Contains(name, "/") {
			return nil, "station names can't contain '/'"
		}
		stations = append(stations, name)
	}
	if len(stations) == 0 {
		return nil, "a terminal that isn't shared by the pool needs at least one station"
	}
	return stations, ""
}

func payterTerminalsDTO(terminals []store.PayterTerminal) []dto.PayterTerminal {
	result := make([]dto.PayterTerminal, 0, len(terminals))
	for _, t := range terminals {
		result = append(result, dto.PayterTerminal{
			SerialNumber: t.SerialNumber, PoolCode: t.PoolCode, IsPoolShared: t.IsPoolShared,
			LinkedStations: t.LinkedStations, IsActive: t.IsActive, UpdatedAt: t.UpdatedAt,
		})
	}
	return result
}
