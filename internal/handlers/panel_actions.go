// This file holds the changes a CPO makes from the management panel: RFID cards, rates, installation power, load
// balancing mode, operator details and station resets. They go through the same pool actions as the TagoIO
// analyses of the former dashboard forms (poolactions).

package handlers

import (
	"context"
	"errors"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/poolactions"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

const (
	cardReadQty   = 2000            // Card records read at once
	resetCooldown = 2 * time.Minute // Between two resets of the same station
)

var (
	cardIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	emailPattern  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// resetGuard remembers the last reset of each station.
type resetGuard struct {
	mu   sync.Mutex
	last map[string]time.Time
}

// allow records a reset of a station at now, unless the last one was less than resetCooldown ago.
func (g *resetGuard) allow(key string, now time.Time) (time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if last, ok := g.last[key]; ok && now.Sub(last) < resetCooldown {
		return resetCooldown - now.Sub(last), false
	}
	g.last[key] = now
	return 0, true
}

// region Cards

// PanelCardsAPI lists the pool's RFID cards (GET /api/panel/{device_id}/cards).
func (a *App) PanelCardsAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	cards, err := a.panelCards(ctx, caller.poolCode)
	if err != nil {
		a.Log.Error("Panel: reading the cards failed", "pool_code", caller.poolCode, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "Cards not available")
		return
	}

	// What each card spent in the last 30 days, from the history (when it can be read)
	if entries, err := a.panelHistory(ctx, caller.poolCode); err == nil {
		since := time.Now().AddDate(0, 0, -30)
		spent := map[string]float64{}
		for _, e := range entries {
			if e.start.Before(since) {
				break // Newest first
			}
			spent[strings.ToUpper(e.item.Card)] += e.item.Cost
		}
		for i := range cards {
			cards[i].Spent30Days = round(spent[strings.ToUpper(cards[i].Alias)], 2)
		}
	}
	writeJSON(w, http.StatusOK, cards)
}

// panelCards reads the pool's cards, one per card ID (the newest record), sorted by alias.
func (a *App) panelCards(ctx context.Context, poolCode int) ([]dto.PanelCard, error) {
	cards, _, err := a.panelCardRecords(ctx, poolCode)
	return cards, err
}

// panelCardRecords reads the pool's cards, and the groups each card's records are stored under, by card ID in
// upper case: older records were grouped by the ID as typed, and some cards were stored more than once.
func (a *App) panelCardRecords(ctx context.Context, poolCode int) ([]dto.PanelCard, map[string][]string, error) {
	records, err := a.PanelData.List(ctx, poolCode, poolactions.CardVariable, cardReadQty)
	if err != nil {
		return nil, nil, err
	}
	groups := map[string][]string{}
	cards := []dto.PanelCard{}
	for _, r := range records {
		id := strings.ToUpper(strings.TrimSpace(tagoio.ValueString(r.Value)))
		if id == "" {
			id = strings.ToUpper(r.Group)
		}
		if id == "" {
			continue
		}
		_, seen := groups[id]
		if !slices.Contains(groups[id], r.Group) {
			groups[id] = append(groups[id], r.Group)
		}
		if seen {
			continue
		}
		cards = append(cards, dto.PanelCard{
			ID:             id,
			Alias:          strings.TrimSpace(tagoio.ValueString(r.Metadata["alias"])),
			Email:          strings.TrimSpace(tagoio.ValueString(r.Metadata["email"])),
			LinkedStations: pools.SplitList(tagoio.ValueString(r.Metadata["cps"])),
		})
	}
	slices.SortFunc(cards, func(x, y dto.PanelCard) int {
		return strings.Compare(strings.ToLower(x.Alias+x.ID), strings.ToLower(y.Alias+y.ID))
	})
	return cards, groups, nil
}

// PanelCreateCardAPI creates a card (POST /api/panel/{device_id}/cards).
func (a *App) PanelCreateCardAPI(w http.ResponseWriter, r *http.Request) {
	a.saveCard(w, r, "")
}

// PanelUpdateCardAPI changes a card (PUT /api/panel/{device_id}/cards/{card_id}); its ID can't change.
func (a *App) PanelUpdateCardAPI(w http.ResponseWriter, r *http.Request) {
	a.saveCard(w, r, r.PathValue("card_id"))
}

func (a *App) saveCard(w http.ResponseWriter, r *http.Request, pathID string) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	var body dto.PanelCardRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid card body")
		return
	}
	if pathID != "" {
		body.ID = pathID
	}
	card, problem := a.validCard(caller.poolCode, body)
	if problem != "" {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, problem)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	existing, groups, err := a.panelCardRecords(ctx, caller.poolCode)
	if err != nil {
		a.Log.Error("Panel: reading the cards failed", "pool_code", caller.poolCode, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "Cards not available")
		return
	}
	exists := slices.ContainsFunc(existing, func(c dto.PanelCard) bool { return strings.EqualFold(c.ID, card.ID) })
	card.StoredGroups = groups[strings.ToUpper(card.ID)]
	switch {
	case pathID == "" && exists:
		writeAPIError(w, http.StatusConflict, dto.ErrorCodeConflict, "The card already exists")
		return
	case pathID != "" && !exists:
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Unknown card")
		return
	}

	if err := a.Actions.SaveCard(ctx, caller.poolCode, card); err != nil {
		a.panelActionFailed(w, caller, "saving a card", err)
		return
	}
	a.Log.Info("Panel: card saved", "pool_code", caller.poolCode, "card", strings.ToUpper(card.ID), "user_id", caller.claims.UserID, "created", pathID == "")
	w.WriteHeader(http.StatusNoContent)
}

// validCard checks a card request; it returns the card, or what is wrong with it.
func (a *App) validCard(poolCode int, body dto.PanelCardRequest) (poolactions.Card, string) {
	id := strings.TrimSpace(body.ID)
	alias := strings.TrimSpace(body.Alias)
	email := strings.TrimSpace(body.Email)
	switch {
	case !cardIDPattern.MatchString(id):
		return poolactions.Card{}, "id must be 1 to 64 letters, digits or _ . : -"
	case utf8.RuneCountInString(alias) > 60 || strings.Contains(alias, ","):
		return poolactions.Card{}, "alias must be at most 60 characters, without commas"
	case email != "" && (len(email) > 254 || !emailPattern.MatchString(email)):
		return poolactions.Card{}, "email is not valid"
	case len(body.LinkedStations) == 0:
		return poolactions.Card{}, "linked_stations needs at least one station"
	}
	known := a.panelStationNames(poolCode)
	linked := make([]string, 0, len(body.LinkedStations))
	for _, s := range body.LinkedStations {
		s = strings.TrimSpace(s)
		if !slices.Contains(known, s) {
			return poolactions.Card{}, "unknown station " + s
		}
		if !slices.Contains(linked, s) {
			linked = append(linked, s)
		}
	}
	return poolactions.Card{ID: id, Alias: alias, Email: email, LinkedCPs: linked}, ""
}

// PanelDeleteCardAPI deletes a card (DELETE /api/panel/{device_id}/cards/{card_id}).
func (a *App) PanelDeleteCardAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("card_id"))
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	cards, groups, err := a.panelCardRecords(ctx, caller.poolCode)
	if err != nil {
		a.Log.Error("Panel: reading the cards failed", "pool_code", caller.poolCode, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "Cards not available")
		return
	}
	index := slices.IndexFunc(cards, func(c dto.PanelCard) bool { return strings.EqualFold(c.ID, id) })
	if index < 0 {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Unknown card")
		return
	}
	card := cards[index]
	if err := a.Actions.DeleteCard(ctx, caller.poolCode, card.ID, card.LinkedStations, groups[card.ID]...); err != nil {
		a.panelActionFailed(w, caller, "deleting a card", err)
		return
	}
	a.Log.Info("Panel: card deleted", "pool_code", caller.poolCode, "card", cards[index].ID, "user_id", caller.claims.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// endregion

// region Configuration

// PanelRatesAPI changes the rates (PUT /api/panel/{device_id}/rates).
func (a *App) PanelRatesAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	var body dto.PanelRates
	if err := decodeJSON(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid rates body")
		return
	}
	for _, rate := range []float64{body.OffPeak, body.Flat, body.Peak} {
		if !inRange(rate, 0, 5) {
			writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "rates must be between 0 and 5 €/kWh")
			return
		}
	}
	if !inRange(body.VATPercent, 0, 30) || !inRange(body.Withholding, 0, 500) {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "vat_percent must be 0-30 and withholding 0-500")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	// The VAT as a percentage: a value of 1 or less would be taken as a fraction
	vat := body.VATPercent / 100
	err := a.Actions.SetRates(ctx, caller.poolCode, poolactions.Rates{
		OffPeak: body.OffPeak, Flat: body.Flat, Peak: body.Peak, VAT: vat, Withholding: body.Withholding,
	})
	if err != nil {
		a.panelActionFailed(w, caller, "changing the rates", err)
		return
	}
	a.Log.Info("Panel: rates changed", "pool_code", caller.poolCode, "user_id", caller.claims.UserID,
		"rates", []float64{body.OffPeak, body.Flat, body.Peak}, "vat_percent", body.VATPercent)
	writeJSON(w, http.StatusOK, a.panelOverview(ctx, caller.poolCode))
}

// PanelMaxPowerAPI changes the installation power (PUT /api/panel/{device_id}/max-power).
func (a *App) PanelMaxPowerAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	var body dto.PanelMaxPowerRequest
	if err := decodeJSON(w, r, &body); err != nil || !inRange(body.MaxPowerW, 1000, 10_000_000) {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "max_power_w must be between 1000 and 10000000")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	watts := math.Round(body.MaxPowerW)
	if err := a.Actions.SetMaxPower(ctx, caller.poolCode, watts, true); err != nil {
		a.panelActionFailed(w, caller, "changing the installation power", err)
		return
	}
	a.Log.Info("Panel: installation power changed", "pool_code", caller.poolCode, "user_id", caller.claims.UserID, "watts", watts)
	writeJSON(w, http.StatusOK, a.panelOverview(ctx, caller.poolCode))
}

// PanelModeAPI changes the load balancing mode (PUT /api/panel/{device_id}/load-balancing-mode).
func (a *App) PanelModeAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	var body dto.PanelModeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid mode body")
		return
	}
	if _, known := poolactions.BalancingModeLabel(body.Mode); !known {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Unknown load balancing mode")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	label, err := a.Actions.SetLoadBalancingMode(ctx, caller.poolCode, body.Mode)
	if err != nil {
		a.panelActionFailed(w, caller, "changing the load balancing mode", err)
		return
	}
	a.Log.Info("Panel: load balancing mode changed", "pool_code", caller.poolCode, "user_id", caller.claims.UserID, "mode", label)
	writeJSON(w, http.StatusOK, a.panelOverview(ctx, caller.poolCode))
}

// PanelCPOAPI changes the operator details (PUT /api/panel/{device_id}/cpo).
func (a *App) PanelCPOAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	var body dto.PanelCPO
	if err := decodeJSON(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid operator body")
		return
	}
	fields := []string{body.Name, body.FiscalID, body.Address, body.Phone, body.Email, body.Web}
	for _, f := range fields {
		if utf8.RuneCountInString(f) > 200 {
			writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Fields must be at most 200 characters")
			return
		}
	}
	email := strings.TrimSpace(body.Email)
	if strings.TrimSpace(body.Name) == "" || (email != "" && !emailPattern.MatchString(email)) {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "name is required and email must be valid")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	_, err := a.Actions.SetCPOInfo(ctx, caller.poolCode, poolactions.CPOInfo{
		Name: body.Name, FiscalID: body.FiscalID, Address: body.Address, Phone: body.Phone, Email: body.Email, Web: body.Web,
	}, true)
	if err != nil {
		a.panelActionFailed(w, caller, "changing the operator details", err)
		return
	}
	a.Log.Info("Panel: operator details changed", "pool_code", caller.poolCode, "user_id", caller.claims.UserID)
	writeJSON(w, http.StatusOK, a.panelOverview(ctx, caller.poolCode))
}

// endregion

// region Stations

// PanelResetAPI asks the CSMS for a soft reset of a station (POST /api/panel/{device_id}/stations/{station}/reset).
// The CSMS doesn't report the outcome back: the station going offline and online again shows in the stream.
func (a *App) PanelResetAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	station := strings.TrimSpace(r.PathValue("station"))
	if !a.ChargePoints.HasStation(caller.poolCode, station) {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Unknown station")
		return
	}
	now := time.Now()
	if wait, allowed := a.resets.allow(poolStationKey(caller.poolCode, station), now); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeAPIError(w, http.StatusTooManyRequests, dto.ErrorCodeRateLimited, "The station was reset a moment ago")
		return
	}
	a.Actions.ResetStation(caller.poolCode, station)
	a.Log.Info("Panel: station reset sent", "pool_code", caller.poolCode, "station", station, "user_id", caller.claims.UserID)
	writeJSON(w, http.StatusAccepted, dto.PanelResetResult{StationName: station, SentAt: now.UTC().Format(time.RFC3339)})
}

// endregion

// panelActionFailed answers a change that reached the CSMS but whose bucket write failed (TagoIO), or failed
// before it.
func (a *App) panelActionFailed(w http.ResponseWriter, caller panelCaller, action string, err error) {
	a.Log.Error("Panel: "+action+" failed", "pool_code", caller.poolCode, "user_id", caller.claims.UserID, "error", err)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "TagoIO not reachable")
		return
	}
	writeAPIError(w, http.StatusBadGateway, dto.ErrorCodeServiceUnavailable, "The change could not be stored in TagoIO")
}

func inRange(f, low, high float64) bool {
	return !math.IsNaN(f) && f >= low && f <= high
}

func poolStationKey(poolCode int, station string) string {
	return strconv.Itoa(poolCode) + "/" + station
}
