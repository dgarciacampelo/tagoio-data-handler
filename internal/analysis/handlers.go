// This file holds the handler of each analysis. The field positions are the order of the TagoIO dashboard forms,
// as the Python service read them; the variable names are the confirmed ones (see scope.go).

package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/poolactions"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// Fallback for a missing or malformed withholding (pre-authorization) amount, in euros.
const defaultWithholding = 40.0

// region Change availability

var fieldStationName = field{position: 0}

// changeAvailability asks the CSMS to toggle a station's operativity.
func (s *Service) changeAvailability(ctx context.Context, sc scope) {
	poolCode, ok := s.poolOf(ctx, sc, "change_availability")
	if !ok {
		return
	}
	station := sc.str(fieldStationName)
	if station == "" {
		s.log.Error("Analysis: change_availability without station", "pool_code", poolCode)
		return
	}
	s.publisher.Publish(events.ChangeAvailability{
		Header:      events.NewHeader(events.TypeChangeAvailability, poolCode),
		StationName: station,
	})
}

// endregion

// region Max grid power

var fieldMaxPower = field{position: 0}

// changeMaxGridPower sets the maximum power the installation may draw from the grid.
func (s *Service) changeMaxGridPower(ctx context.Context, sc scope) {
	poolCode, ok := s.poolOf(ctx, sc, "change_max_grid_power")
	if !ok {
		return
	}
	watts, err := tagoio.ValueFloat(sc.value(fieldMaxPower))
	if err != nil {
		s.log.Error("Analysis: invalid max grid power", "pool_code", poolCode, "error", err)
		return
	}
	// The dashboard form stores the value itself
	_ = s.actions.SetMaxPower(ctx, poolCode, watts, false)
}

// endregion

// region RFID cards

var (
	fieldCardID    = field{position: 0}
	fieldCardAlias = field{position: 1}
	fieldCardEmail = field{position: 2}
	fieldCardCPs   = field{position: 3}
	fieldCardOp    = field{position: 4}
)

const feedbackRFID = "validation_rfid" // Toast variable of the RFID form

// manageRFID creates or deletes an RFID card: the CSMS gets the event, and the card record of the pool's bucket
// (one per card, grouped by the card ID in upper case) is replaced or deleted. A create comes from the form (five
// fields, the last one "create"); a delete from the cards table (the card record alone).
func (s *Service) manageRFID(ctx context.Context, sc scope) {
	poolCode, ok := s.poolOf(ctx, sc, "manage_rfid")
	if !ok {
		return
	}
	card := strings.TrimSpace(sc.str(fieldCardID))
	if card == "" {
		s.log.Error("Analysis: manage_rfid without card", "pool_code", poolCode)
		s.bucket.Feedback(poolCode, feedbackRFID, false)
		return
	}
	switch {
	case len(sc.records) == 1:
		var linkedCPs []string
		if r := sc.record(fieldCardID); r != nil {
			linkedCPs = pools.SplitList(tagoio.ValueString(r.Metadata["cps"]))
		}
		err := s.actions.DeleteCard(ctx, poolCode, card, linkedCPs)
		s.report(poolCode, feedbackRFID, "deleting RFID card", err)

	case sc.str(fieldCardOp) == "create":
		err := s.actions.SaveCard(ctx, poolCode, poolactions.Card{
			ID: card, Alias: sc.str(fieldCardAlias), Email: sc.str(fieldCardEmail),
			LinkedCPs: pools.SplitList(sc.str(fieldCardCPs)),
		})
		s.report(poolCode, feedbackRFID, "storing RFID card", err)

	default:
		s.log.Error("Analysis: manage_rfid scope is neither a create nor a delete", "pool_code", poolCode, "variables", sc.variables())
		s.bucket.Feedback(poolCode, feedbackRFID, false)
	}
}

// endregion

// region CPO info

var (
	fieldCPOName     = field{position: 0}
	fieldCPOFiscalID = field{position: 1}
	fieldCPOAddress  = field{position: 2}
	fieldCPOPhone    = field{position: 3}
	fieldCPOEmail    = field{position: 4}
	fieldCPOWeb      = field{position: 5}
)

// changeCPOInfo updates the operator details.
func (s *Service) changeCPOInfo(ctx context.Context, sc scope) {
	poolCode, ok := s.poolOf(ctx, sc, "change_cpo_info")
	if !ok {
		return
	}
	// Not stored in the bucket: the dashboard form keeps its own values
	_, _ = s.actions.SetCPOInfo(ctx, poolCode, poolactions.CPOInfo{
		Name: sc.str(fieldCPOName), FiscalID: sc.str(fieldCPOFiscalID), Address: sc.str(fieldCPOAddress),
		Phone: sc.str(fieldCPOPhone), Email: sc.str(fieldCPOEmail), Web: sc.str(fieldCPOWeb),
	}, false)
}

// endregion

// region Rates

var (
	fieldRateOffPeak = field{position: 0}
	fieldRateFlat    = field{position: 1}
	fieldRatePeak    = field{position: 2}
	fieldRateVAT     = field{position: 3}
	fieldWithholding = field{position: 4}
)

const feedbackRate = "validation_rate" // Toast variable of the rates form

// changeRateList updates the energy rates, the VAT and the pre-authorization amount: the CSMS gets the event, the
// rate_costs and withholding_amount records of the bucket are replaced and the configuration cache is updated.
func (s *Service) changeRateList(ctx context.Context, sc scope) {
	poolCode, ok := s.poolOf(ctx, sc, "change_rate_list")
	if !ok {
		return
	}

	var rates [4]float64
	for i, f := range []field{fieldRateOffPeak, fieldRateFlat, fieldRatePeak, fieldRateVAT} {
		value, err := tagoio.ValueFloat(sc.value(f))
		if err != nil {
			s.log.Error("Analysis: invalid rate", "pool_code", poolCode, "position", f.position, "error", err)
			s.bucket.Feedback(poolCode, feedbackRate, false)
			return
		}
		rates[i] = value
	}
	withholding, err := tagoio.ValueFloat(sc.value(fieldWithholding))
	if err != nil {
		withholding = defaultWithholding
	}
	err = s.actions.SetRates(ctx, poolCode, poolactions.Rates{
		OffPeak: rates[0], Flat: rates[1], Peak: rates[2], VAT: rates[3], Withholding: withholding,
	})
	s.report(poolCode, feedbackRate, "storing rates", err)
}

// endregion

// region Load balancing

var fieldBalancingMode = field{position: 0}

// changeLoadBalancingMode selects the load balancing mode: the CSMS gets the event first, then the
// load_balancing_mode record of the pool's bucket is replaced, since the dashboard input doesn't store it, and the
// configuration cache is updated. An empty or unknown mode is still sent to the CSMS, which ignores it, but is
// neither stored nor cached. The form has no feedback toast: a failed write is only logged.
func (s *Service) changeLoadBalancingMode(ctx context.Context, sc scope) {
	poolCode, ok := s.poolOf(ctx, sc, "change_load_balancing_mode")
	if !ok {
		return
	}
	mode := sc.str(fieldBalancingMode)
	label, err := s.actions.SetLoadBalancingMode(ctx, poolCode, mode)
	switch {
	case errors.Is(err, poolactions.ErrUnknownMode):
		s.log.Warn("Analysis: unknown load balancing mode not stored", "pool_code", poolCode, "mode", mode)
	case err != nil:
		s.log.Error("Analysis: storing load balancing mode failed", "pool_code", poolCode, "mode", label, "error", err)
	}
}

// endregion

// region OCPP requests (debug tab)

var (
	// Confirmed from a real scope (neos-ocpp src/interface/_ocpp_requests_scope.json).
	fieldOCPPStations  = field{names: []string{"ocpp_cp"}, position: 0}
	fieldOCPPRequest   = field{names: []string{"ocpp_request"}, position: 1}
	fieldOCPPConnector = field{names: []string{"params_connector"}, position: 2}
	// Not confirmed yet: positions only.
	fieldOCPPResetType     = field{position: 2}
	fieldOCPPTransactionID = field{position: 2}
	fieldOCPPAvailability  = field{position: 3}
	fieldOCPPIDTag         = field{position: 3}
)

// ocppRequest replicates an OCPP command on the selected stations.
func (s *Service) ocppRequest(ctx context.Context, sc scope) {
	poolCode, ok := s.poolOf(ctx, sc, "ocpp_request")
	if !ok {
		return
	}
	stations := pools.SplitList(sc.str(fieldOCPPStations))
	request := sc.str(fieldOCPPRequest)

	connector := func() (int, bool) {
		id, err := tagoio.ValueInt(sc.value(fieldOCPPConnector))
		if err != nil {
			s.log.Error("Analysis: invalid OCPP connector", "pool_code", poolCode, "request", request, "error", err)
			return 0, false
		}
		return id, true
	}

	var payload any
	switch request {
	case events.RequestStatusNotification:
		id, ok := connector()
		if !ok {
			return
		}
		payload = events.StatusNotificationPayload{Request: request, StationNames: stations, ConnectorID: id}

	case events.RequestChangeAvailability:
		id, ok := connector()
		if !ok {
			return
		}
		availability := "Inoperative"
		if strings.EqualFold(sc.str(fieldOCPPAvailability), "available") {
			availability = "Operative"
		}
		payload = events.ChangeAvailabilityPayload{Request: request, StationNames: stations, ConnectorID: id, AvailabilityType: availability}

	case events.RequestReset:
		resetType := capitalize(sc.str(fieldOCPPResetType))
		if resetType != "Soft" && resetType != "Hard" {
			s.log.Error("Analysis: invalid OCPP reset type", "pool_code", poolCode, "reset_type", resetType)
			return
		}
		payload = events.ResetPayload{Request: request, StationNames: stations, ResetType: resetType}

	case events.RequestRemoteStartTransaction:
		id, ok := connector()
		if !ok {
			return
		}
		payload = events.RemoteStartPayload{Request: request, StationNames: stations, ConnectorID: id, IDTag: sc.str(fieldOCPPIDTag)}

	case events.RequestRemoteStopTransaction:
		transactionID, err := tagoio.ValueInt(sc.value(fieldOCPPTransactionID))
		if err != nil {
			s.log.Error("Analysis: invalid OCPP transaction ID", "pool_code", poolCode, "error", err)
			return
		}
		payload = events.RemoteStopPayload{Request: request, TransactionID: transactionID}

	case events.RequestUnlockConnector:
		id, ok := connector()
		if !ok {
			return
		}
		payload = events.UnlockConnectorPayload{Request: request, StationNames: stations, ConnectorID: id}

	default:
		s.log.Warn("Analysis: unsupported OCPP request ignored", "pool_code", poolCode, "request", request)
		return
	}

	s.publisher.Publish(events.OCPPRequest{Header: events.NewHeader(events.TypeOCPPRequest, poolCode), Payload: payload})
}

// capitalize returns s with its first letter in upper case and the rest in lower case, like Python's
// str.capitalize ("HARD" -> "Hard").
func capitalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// endregion

// region Power consumption (MQTT meter)

// bivocomTopicPrefix starts the topic of the Bivocom grid meters ("Bivocom/MASTER-BUSINESS-221006"). They report P
// in kW: 221006 sent 38–48 at mid-morning on 2026-11-19, against an 80 kW contract and one charger at 6.6 kW. As watts,
// the CSMS took the building as idle, and its dynamic load balancing never lowered the chargers.
const bivocomTopicPrefix = "Bivocom/"

// powerConsumptionUpdate reports the power the installation draws from the grid. The record comes in one of
// three shapes: the parsed MQTT JSON (params.P), the raw MQTT message (topic and a JSON payload string), or a
// plain variable (value). Records forwarded by the MQTT integration may have no device; the pool code is then
// the end of the topic ("Bivocom/MASTER-BUSINESS-221006").
func (s *Service) powerConsumptionUpdate(ctx context.Context, sc scope) {
	if len(sc.records) == 0 {
		return
	}
	r := sc.records[0]

	var (
		watts float64
		err   error
	)
	switch {
	case r.Params != nil && r.Params["P"] != nil:
		watts, err = tagoio.ValueFloat(r.Params["P"])
	case r.Topic != "" && len(r.Payload) > 0:
		watts, err = mqttPayloadPower(r.Payload)
	default:
		watts, err = tagoio.ValueFloat(r.Value)
	}
	if err != nil {
		s.log.Error("Analysis: invalid power value", "device_id", r.Device, "topic", r.Topic, "error", err)
		return
	}

	topic := r.Topic
	if topic == "" {
		topic = tagoio.ValueString(r.Metadata["mqtt_topic"])
	}
	if strings.HasPrefix(topic, bivocomTopicPrefix) {
		watts *= 1000 // A Bivocom meter reports P in kW
	}

	poolCode, ok := 0, false
	if r.Device != "" {
		poolCode, ok = s.pools.PoolByDevice(ctx, r.Device)
	}
	if !ok {
		poolCode, ok = poolFromTopic(topic)
	}
	if !ok {
		s.log.Error("Analysis: unknown pool for power update", "device_id", r.Device, "topic", r.Topic)
		return
	}

	s.publisher.Publish(events.PowerUpdate{
		Header:     events.NewHeader(events.TypePowerConsumptionUpdate, poolCode),
		MeterWatts: watts,
	})
}

// mqttPayloadPower reads params.P from a raw MQTT payload: a JSON string holding the message, or the message.
func mqttPayloadPower(raw json.RawMessage) (float64, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		raw = json.RawMessage(text)
	}
	var message struct {
		Params map[string]any `json:"params"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&message); err != nil {
		return 0, err
	}
	if message.Params["P"] == nil {
		return 0, nil // As before: a message without P reports 0 W
	}
	return tagoio.ValueFloat(message.Params["P"])
}

// poolFromTopic extracts the pool code at the end of an MQTT topic.
func poolFromTopic(topic string) (int, bool) {
	i := strings.LastIndex(topic, "-")
	if i < 0 {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(topic[i+1:]))
	return code, err == nil && code > 0
}

// endregion

// report logs a failed bucket write and shows the dashboard toast of the outcome.
func (s *Service) report(poolCode int, feedbackVariable, action string, err error) {
	if err != nil {
		s.log.Error("Analysis: "+action+" failed", "pool_code", poolCode, "error", err)
	}
	s.bucket.Feedback(poolCode, feedbackVariable, err == nil)
}
