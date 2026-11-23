// This file holds the embedded management panel of a pool: the page TagoIO shows in a Custom Widget of the CPO
// dashboard, its session (who may manage which pool, see panelauth), its overview and its live stream.
//
// Every panel API path starts with the pool's TagoIO device ID, as the dashboard knows it, and every call carries
// the panel session in "Authorization: Bearer". The page lives in a third-party iframe, where cookies are blocked,
// so the session never travels as a cookie.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/panelauth"
	"github.com/dgarciacampelo/tagoio-handler/internal/poolactions"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// panelFrameAncestors are the pages that may embed the panel: the TagoIO admin console and TagoIO Run.
const panelFrameAncestors = "frame-ancestors https://*.tago.io https://*.tago.run"

const (
	panelStreamCheck = 2 * time.Second // How often the stream looks for changes of the pool
	panelTimeout     = 30 * time.Second
)

// tagoDeviceID matches a TagoIO object ID.
var tagoDeviceID = regexp.MustCompile(`^[0-9a-f]{24}$`)

// PanelData reads a pool's bucket for the panel.
type PanelData interface {
	LastValue(ctx context.Context, poolCode int, variable string) (*tagoio.DataRecord, error)
	List(ctx context.Context, poolCode int, variable string, qty int) ([]tagoio.DataRecord, error)
}

// panelCaller is who calls a panel API, once its session is checked.
type panelCaller struct {
	poolCode int
	claims   panelauth.Claims
}

// PanelPage renders the panel (GET /panel). It may be framed by the TagoIO pages only.
func (a *App) PanelPage(w http.ResponseWriter, _ *http.Request) {
	header := w.Header()
	header.Del("X-Frame-Options")
	header.Set("Content-Security-Policy", panelFrameAncestors)
	a.page(w, "panel", "Panel de gestión", nil)
}

// PanelSessionAPI starts a panel session (POST /api/panel/session): it identifies the TagoIO token and checks that
// its user may manage the device's pool.
func (a *App) PanelSessionAPI(w http.ResponseWriter, r *http.Request) {
	if !a.allowCall(w, r, a.panelSessionLimit) {
		return
	}
	var body dto.PanelSessionRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid session body")
		return
	}
	deviceID := strings.TrimSpace(body.DeviceID)
	if !tagoDeviceID.MatchString(deviceID) || strings.TrimSpace(body.Token) == "" {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "token and device_id are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	identity, err := a.PanelAuth.Identify(ctx, body.Token)
	switch {
	case errors.Is(err, panelauth.ErrInvalidToken):
		a.Log.Warn("Panel: session refused, invalid TagoIO token", "client", clientAddress(r))
		writeAPIError(w, http.StatusUnauthorized, dto.ErrorCodeUnauthenticated, "TagoIO token not valid")
		return
	case err != nil:
		a.Log.Error("Panel: identifying the TagoIO token failed", "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "TagoIO not reachable")
		return
	}

	poolCode, ok := a.Devices.PoolByDevice(ctx, deviceID)
	if !ok || !identity.Allows(poolCode) {
		// The same answer for an unknown device and a pool of someone else: nothing to learn from it
		a.Log.Warn("Panel: session refused, pool not allowed", "user_id", identity.UserID, "device_id", deviceID,
			"pool_code", poolCode, "group_ids", identity.GroupIDs)
		writeAPIError(w, http.StatusForbidden, dto.ErrorCodeForbidden, "Pool not allowed for this user")
		return
	}

	session, claims := a.PanelAuth.Issue(identity, poolCode)
	kind := dto.PanelUserKindRunUser
	if identity.Admin {
		kind = dto.PanelUserKindAdmin
	}
	a.Log.Info("Panel: session started", "pool_code", poolCode, "user_id", identity.UserID, "admin", identity.Admin)
	writeJSON(w, http.StatusOK, dto.PanelSession{
		Session: session, ExpiresAt: time.Unix(claims.Expiry, 0).UTC().Format(time.RFC3339),
		PoolCode: poolCode, UserKind: kind,
	})
}

// panelAuth checks the session of a panel call for the device in its path; it answers 401 or 404 itself.
func (a *App) panelAuth(w http.ResponseWriter, r *http.Request) (panelCaller, bool) {
	deviceID := r.PathValue("device_id")
	if !tagoDeviceID.MatchString(deviceID) {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Unknown device")
		return panelCaller{}, false
	}
	session, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || session == "" {
		writeAPIError(w, http.StatusUnauthorized, dto.ErrorCodeUnauthenticated, "Panel session required")
		return panelCaller{}, false
	}
	// The signature first: only a signed session may make the registry look up (and maybe rescan) a device
	claims, valid := a.PanelAuth.Parse(session)
	if !valid {
		writeAPIError(w, http.StatusUnauthorized, dto.ErrorCodeUnauthenticated, "Panel session expired or not valid")
		return panelCaller{}, false
	}
	poolCode, known := a.Devices.PoolByDevice(r.Context(), deviceID)
	if !known || poolCode != claims.PoolCode {
		writeAPIError(w, http.StatusUnauthorized, dto.ErrorCodeUnauthenticated, "Panel session not for this pool")
		return panelCaller{}, false
	}
	return panelCaller{poolCode: poolCode, claims: claims}, true
}

// region Overview

// PanelOverviewAPI returns the pool's configuration (GET /api/panel/{device_id}/overview).
func (a *App) PanelOverviewAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, a.panelOverview(ctx, caller.poolCode))
}

func (a *App) panelOverview(ctx context.Context, poolCode int) dto.PanelOverview {
	cfg := a.Pools.GetOrLoad(ctx, poolCode)
	maxPower := cfg.MaxPower
	if r, err := a.PanelData.LastValue(ctx, poolCode, poolactions.MaxPowerVariable); err == nil && r != nil {
		if watts, err := tagoio.ValueFloat(r.Value); err == nil && watts > 0 {
			maxPower = watts
		}
	}

	modes := make([]string, 0, len(poolactions.BalancingModes))
	for _, m := range poolactions.BalancingModes {
		modes = append(modes, m.Label)
	}
	mode, known := poolactions.BalancingModeLabel(cfg.LoadBalancingMode)
	if !known {
		mode = cfg.LoadBalancingMode
	}

	return dto.PanelOverview{
		PoolCode:         poolCode,
		InstallationName: a.Devices.DeviceName(poolCode),
		CPO: dto.PanelCPO{Name: cfg.CPOName, FiscalID: cfg.CPOFiscalID, Address: cfg.CPOAddress,
			Phone: cfg.CPOPhone, Email: cfg.CPOEmail, Web: cfg.CPOWeb},
		Rates: dto.PanelRates{OffPeak: cfg.RateOffPeak, Flat: cfg.RateFlat, Peak: cfg.RatePeak,
			VATPercent: pools.VATPercentage(cfg.VAT), Withholding: cfg.PreauthAmount},
		MaxPowerW:         maxPower,
		LoadBalancingMode: mode,
		BalancingModes:    modes,
		Stations:          a.panelStationNames(poolCode),
		Bands:             weekdayBands,
	}
}

// weekdayBands are the tariff periods of a weekday, as the CSMS bills them (neos-ocpp ENERGY_BAND_TIMES); weekends
// and national holidays are off-peak all day.
var weekdayBands = []dto.PanelTariffPeriod{
	{Band: dto.TariffBandOffPeak, FromHour: 0, ToHour: 8},
	{Band: dto.TariffBandFlat, FromHour: 8, ToHour: 10},
	{Band: dto.TariffBandPeak, FromHour: 10, ToHour: 14},
	{Band: dto.TariffBandFlat, FromHour: 14, ToHour: 18},
	{Band: dto.TariffBandPeak, FromHour: 18, ToHour: 22},
	{Band: dto.TariffBandFlat, FromHour: 22, ToHour: 24},
}

// panelStationNames returns the names of the pool's known stations, sorted.
func (a *App) panelStationNames(poolCode int) []string {
	view := a.ChargePoints.PoolView(poolCode)
	names := make([]string, 0, len(view))
	for _, s := range view {
		names = append(names, s.Name)
	}
	return names
}

// endregion

// region Live stream

// PanelStreamAPI streams the pool's live state (GET /api/panel/{device_id}/stream) as Server-Sent Events: one
// "data:" line with a dto.PanelLive at start and whenever a status or a session of the pool changes, and a comment
// as keep-alive. The stream ends when the session expires; the page then starts a new one.
func (a *App) PanelStreamAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func() error {
		data, err := json.Marshal(a.panelLive(caller.poolCode))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	version := a.ChargePoints.PoolVersion(caller.poolCode)
	if send() != nil {
		return
	}
	expiry := time.Until(time.Unix(caller.claims.Expiry, 0))
	end := time.NewTimer(expiry)
	defer end.Stop()
	check := time.NewTicker(panelStreamCheck)
	defer check.Stop()
	keepAlive := time.NewTicker(streamKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-end.C:
			return
		case <-check.C:
			if current := a.ChargePoints.PoolVersion(caller.poolCode); current != version {
				version = current
				if send() != nil {
					return
				}
			}
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// panelLive builds the live state of a pool.
func (a *App) panelLive(poolCode int) dto.PanelLive {
	view := a.ChargePoints.PoolView(poolCode)
	live := dto.PanelLive{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Stations:    make([]dto.PanelStation, 0, len(view)),
		Sessions:    []dto.PanelLiveSession{},
	}
	for _, s := range view {
		station := dto.PanelStation{Name: s.Name, Connection: dto.PanelConnection(s.Connection),
			ConnectionSince: rfc3339OrNil(s.ConnectionSince), Connectors: make([]dto.PanelConnector, 0, len(s.Connectors))}
		for _, c := range s.Connectors {
			station.Connectors = append(station.Connectors, dto.PanelConnector{
				ID: c.ID, Status: dto.ConnectorStatus(c.Status), Since: rfc3339OrNil(c.Since)})
			if c.Session != nil {
				session := panelLiveSession(*c.Session)
				live.Sessions = append(live.Sessions, session)
				live.TotalPowerW += session.PowerW
			}
		}
		live.Stations = append(live.Stations, station)
	}
	return live
}

func panelLiveSession(u chargepoints.SessionUpdate) dto.PanelLiveSession {
	return dto.PanelLiveSession{
		StationName: u.StationName, ConnectorID: int(u.ConnectorID), TransactionID: int64(u.TransactionID),
		CardAlias: u.CardAlias, StartDate: u.StartDate, StartTime: u.StartTime, Duration: u.Time,
		PowerW: int64(u.Power), EnergyKWh: float64(u.Energy), Cost: float64(u.Cost),
		CurrentBand:   tariffBand(u.CurrentTariffBand),
		EnergyBandsWh: [3]int64{int64(u.EnergyOffPeak), int64(u.EnergyFlat), int64(u.EnergyPeak)},
		LastMeterAt:   u.LastMeterTS,
	}
}

// tariffBand maps the CSMS band names ("Off-Peak", "Flat", "Peak") to the contract's.
func tariffBand(band string) dto.TariffBand {
	switch strings.ToLower(strings.TrimSpace(band)) {
	case "peak":
		return dto.TariffBandPeak
	case "flat":
		return dto.TariffBandFlat
	default:
		return dto.TariffBandOffPeak
	}
}

func rfc3339OrNil(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// endregion
