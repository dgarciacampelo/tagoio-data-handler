// This file holds the public station page (opened from the QR code of a station), its refresh API, and the
// payment requests a driver sends from it.

package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
)

// stationQuery is the query of a station page: the selected connector, the number of connectors to show before
// the station reports it (noc), and a status forced for demos (force-status).
type stationQuery struct {
	connectorID int
	noc         int
	nocParam    string // As received, kept in the connector links
	forceStatus chargepoints.Status
	forceParam  string // As received, kept in the refresh URL
}

func parseStationQuery(values url.Values) stationQuery {
	q := stationQuery{connectorID: 1, noc: 1, nocParam: values.Get("noc"), forceParam: values.Get("force-status")}
	if cid, err := strconv.Atoi(values.Get("cid")); err == nil {
		q.connectorID = cid
	}
	if noc, err := strconv.Atoi(q.nocParam); err == nil {
		q.noc = max(1, noc)
	}
	if status, ok := chargepoints.ParseStatus(q.forceParam); ok {
		q.forceStatus = status
	} else {
		q.forceParam = ""
	}
	return q
}

// StationPage renders the public page of a station.
func (a *App) StationPage(w http.ResponseWriter, r *http.Request, poolCode int, station string) {
	q := parseStationQuery(r.URL.Query())

	connectors := q.noc
	if stored, ok, err := a.Store.StationConnectorCount(r.Context(), station); err == nil && ok {
		connectors = stored
	}
	q.connectorID = max(1, min(q.connectorID, connectors))

	view := dto.StationPage{
		PoolCode: poolCode, StationName: station, ConnectorID: q.connectorID,
		Connectors: []dto.ConnectorLink{},
		PollURL:    a.stationPollURL(poolCode, station, q),
		Live:       a.stationLive(r.Context(), poolCode, station, q),
	}
	if connectors > 1 {
		for id := 1; id <= connectors; id++ {
			params := url.Values{"cid": {strconv.Itoa(id)}}
			if q.nocParam != "" {
				params.Set("noc", q.nocParam)
			}
			view.Connectors = append(view.Connectors, dto.ConnectorLink{ID: id, Href: "?" + params.Encode(), Selected: id == q.connectorID})
		}
	}
	a.page(w, "station", "Velo Energy - Panel Público", view)
}

func (a *App) stationPollURL(poolCode int, station string, q stationQuery) string {
	params := url.Values{"cid": {strconv.Itoa(q.connectorID)}}
	if q.forceParam != "" {
		params.Set("force-status", q.forceParam)
	}
	return fmt.Sprintf("/api/station/%d/%s?%s", poolCode, url.PathEscape(station), params.Encode())
}

// StationLiveAPI returns the refreshed part of a station page (GET /api/station/{pool_code}/{station_name}).
func (a *App) StationLiveAPI(w http.ResponseWriter, r *http.Request) {
	if !a.allowCall(w, r, a.pollLimit) {
		return
	}
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	station, ok := pathText(w, r, "station_name")
	if !ok {
		return
	}
	q := parseStationQuery(r.URL.Query())
	q.connectorID = max(1, q.connectorID)
	writeJSON(w, http.StatusOK, a.stationLive(r.Context(), poolCode, station, q))
}

// stationLive builds the refreshed part of a station page: what the page shows for the connector's status.
func (a *App) stationLive(ctx context.Context, poolCode int, station string, q stationQuery) dto.StationLive {
	key := chargepoints.ConnectorKey{PoolCode: poolCode, StationName: station, ConnectorID: q.connectorID}
	status, known := a.ChargePoints.Connector(key)
	if q.forceStatus != "" {
		status = q.forceStatus
	} else if !known {
		status = chargepoints.StatusUnavailable
	}
	session, hasSession := a.ChargePoints.ActiveSession(key)
	config := a.Pools.GetOrLoad(ctx, poolCode)

	live := dto.StationLive{
		ConfigLoaded: config.Loaded,
		Status:       dto.ConnectorStatus(status),
		Panel:        dto.StationPanelNone,
		SessionCard:  dto.SessionCardNone,
		ShowMetrics:  status == chargepoints.StatusCharging,
		Operator:     operatorInfo(config),
		Rates:        ratesInfo(config),
	}
	switch status {
	case chargepoints.StatusAvailable:
		live.Panel = dto.StationPanelPlugIn
	case chargepoints.StatusPreparing:
		live.Panel = dto.StationPanelPaymentForm
	}
	if status.InSession() {
		switch {
		case !hasSession:
			live.SessionCard = dto.SessionCardStarting
		case session.WithPayment:
			live.SessionCard = dto.SessionCardStop
		default:
			live.SessionCard = dto.SessionCardPrivate
		}
	}
	if hasSession {
		live.Metrics = &dto.SessionMetrics{
			EnergyKWh:       float64(session.Energy),
			PowerKW:         float64(session.Power) / 1000,
			DurationMinutes: strings.TrimSpace(strings.TrimSuffix(session.Time, "min")),
			Cost:            float64(session.Cost),
		}
	}
	return live
}

func operatorInfo(c pools.Config) dto.OperatorInfo {
	phone := strings.Join(strings.Fields(c.CPOPhone), "")
	phoneHref := "tel:" + phone
	if !strings.HasPrefix(phone, "+") {
		phoneHref = "tel:+34" + phone // Spanish numbers are stored without the country code
	}
	return dto.OperatorInfo{
		Name: c.CPOName, FiscalID: c.CPOFiscalID, Address: c.CPOAddress,
		Phone: c.CPOPhone, PhoneHref: phoneHref, Email: c.CPOEmail, EmailHref: "mailto:" + c.CPOEmail, Web: c.CPOWeb,
	}
}

func ratesInfo(c pools.Config) dto.RatesInfo {
	return dto.RatesInfo{
		OffPeak: c.RateOffPeak, Flat: c.RateFlat, Peak: c.RatePeak,
		VATPercent:    int(math.Round(c.VAT * 100)),
		PreauthAmount: int(c.PreauthAmount),
	}
}

// region Payment requests

// ChargeRequest asks the CSMS to send the driver a payment link (POST /api/charge-request). The connector must be
// known; the pre-authorization amount comes from the pool configuration, never from the client.
func (a *App) ChargeRequest(w http.ResponseWriter, r *http.Request) {
	if !a.allowCall(w, r, a.chargeLimit) {
		return
	}
	var body dto.ChargeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid charge request")
		return
	}
	if problem := validateChargeRequest(&body); problem != "" {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, problem)
		return
	}
	key := chargepoints.ConnectorKey{PoolCode: body.PoolCode, StationName: body.StationName, ConnectorID: body.ConnectorID}
	if _, known := a.ChargePoints.Connector(key); !known {
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Unknown connector")
		return
	}

	amount := a.Pools.GetOrLoad(r.Context(), body.PoolCode).PreauthAmount
	a.Broker.Publish(events.VPOSStart{
		Header:      events.NewHeader(events.TypeVPOSStartTransaction, body.PoolCode),
		StationName: body.StationName, ConnectorID: body.ConnectorID, Email: body.Email,
		Amount: amount, Currency: "EUR", RequiresInvoice: body.RequiresInvoice,
		ReceiptEmail: body.ReceiptEmail, ReceiptName: body.ReceiptName,
		ReceiptFiscalID: body.ReceiptFiscalID, ReceiptAddress: body.ReceiptAddress,
	})
	a.Log.Info("Payment link requested", "pool_code", body.PoolCode, "station", body.StationName, "connector", body.ConnectorID, "amount", amount)
	writeJSON(w, http.StatusOK, dto.ChargeRequestResult{Status: "pending_authorization", AmountRequested: amount})
}

// validateChargeRequest normalizes a charge request and returns what is wrong with it ("" when valid).
func validateChargeRequest(body *dto.ChargeRequest) string {
	body.StationName = strings.TrimSpace(body.StationName)
	body.Email = strings.TrimSpace(body.Email)
	if body.PoolCode <= 0 || body.StationName == "" || body.ConnectorID <= 0 {
		return "pool_code, station_name and connector_id are required"
	}
	if !validEmail(body.Email) {
		return "email is not a valid address"
	}
	if !body.RequiresInvoice {
		body.ReceiptFiscalID, body.ReceiptName, body.ReceiptAddress, body.ReceiptEmail = nil, nil, nil, nil
		return ""
	}
	for _, field := range []**string{&body.ReceiptFiscalID, &body.ReceiptName, &body.ReceiptAddress, &body.ReceiptEmail} {
		if *field == nil || strings.TrimSpace(**field) == "" {
			return "All invoice fields are mandatory when requires_invoice is true"
		}
		trimmed := strings.TrimSpace(**field)
		*field = &trimmed
	}
	if !validEmail(*body.ReceiptEmail) {
		return "receipt_email is not a valid address"
	}
	return ""
}

// validEmail reports whether s is a plain email address (no display name).
func validEmail(s string) bool {
	address, err := mail.ParseAddress(s)
	return err == nil && address.Address == s && strings.Contains(s, ".")
}

// StopRequest asks the CSMS to stop a paid session (POST /api/stop-request). Only a connector with a paid session
// in progress can be stopped from the page.
func (a *App) StopRequest(w http.ResponseWriter, r *http.Request) {
	if !a.allowCall(w, r, a.chargeLimit) {
		return
	}
	var body dto.StopRequest
	if err := decodeJSON(w, r, &body); err != nil || body.PoolCode <= 0 || strings.TrimSpace(body.StationName) == "" {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid stop request")
		return
	}
	key := chargepoints.ConnectorKey{PoolCode: body.PoolCode, StationName: strings.TrimSpace(body.StationName), ConnectorID: body.ConnectorID}
	session, ok := a.ChargePoints.ActiveSession(key)
	if !ok || !session.WithPayment {
		writeAPIError(w, http.StatusConflict, dto.ErrorCodeConflict, "No paid session in progress on this connector")
		return
	}

	a.Broker.Publish(events.VPOSStop{
		Header:      events.NewHeader(events.TypeVPOSStopTransaction, key.PoolCode),
		StationName: key.StationName, ConnectorID: key.ConnectorID,
	})
	a.Log.Info("Stop requested", "pool_code", key.PoolCode, "station", key.StationName, "connector", key.ConnectorID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Stop request dispatched"})
}

// endregion
