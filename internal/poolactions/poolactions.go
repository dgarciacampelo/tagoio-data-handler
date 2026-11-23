// Package poolactions applies the changes a CPO makes to a charging pool: RFID cards, energy rates, installation
// power, load balancing mode, operator details and station resets. Each action publishes its event for the CSMS
// and keeps the pool's bucket and configuration cache in step. The TagoIO analyses (the former dashboard forms)
// and the embedded management panel share these actions, so both change a pool the same way.
package poolactions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/pools"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// Bucket writes a pool's bucket.
type Bucket interface {
	Delete(ctx context.Context, poolCode int, variable, group string, keepWeeks int) (int, error)
	Replace(ctx context.Context, poolCode int, variable, group string, record tagoio.DataInput) error
}

// PoolConfigs is the pool configuration cache.
type PoolConfigs interface {
	Update(poolCode int, change func(*pools.Config))
}

// Publisher publishes events for the CSMS.
type Publisher interface {
	Publish(events.Event)
}

// Service applies the pool actions.
type Service struct {
	bucket    Bucket
	configs   PoolConfigs
	publisher Publisher
	log       *slog.Logger
}

// New creates the service.
func New(bucket Bucket, configs PoolConfigs, publisher Publisher, log *slog.Logger) *Service {
	return &Service{bucket: bucket, configs: configs, publisher: publisher, log: log}
}

// ErrUnknownMode is returned for a load balancing mode that is neither a known label nor a known code.
var ErrUnknownMode = errors.New("poolactions: unknown load balancing mode")

// region RFID cards

// CardVariable is the bucket variable of the RFID cards: one record per card, grouped by the card ID in upper case.
const CardVariable = "card_id"

// Card is an RFID card of a pool.
type Card struct {
	ID        string   // As the reader sends it; stored in lower case
	Alias     string   // Upper case; the ID when empty
	Email     string   // Optional: where the session summaries go
	LinkedCPs []string // Stations where the card may charge
}

// SaveCard creates a card or replaces it: the CSMS upserts it, and the card's bucket record is replaced.
func (s *Service) SaveCard(ctx context.Context, poolCode int, card Card) error {
	id := strings.TrimSpace(card.ID)
	if id == "" {
		return errors.New("poolactions: card without ID")
	}
	cardID, group := strings.ToLower(id), strings.ToUpper(id)
	alias := strings.ToUpper(strings.TrimSpace(card.Alias))
	if alias == "" {
		alias = group
	}
	email := strings.TrimSpace(card.Email)
	linked := nonNil(card.LinkedCPs)

	s.publisher.Publish(events.RFIDManagement{
		Header: events.NewHeader(events.TypeManageRFID, poolCode),
		CardID: cardID, Action: events.RFIDCreate, LinkedCPs: linked,
		Alias: &alias, Email: optional(email),
	})
	record := tagoio.DataInput{Variable: CardVariable, Value: cardID, Group: group,
		Metadata: map[string]any{"alias": alias, "email": email, "cps": strings.Join(linked, ", ")}}
	if err := s.bucket.Replace(ctx, poolCode, CardVariable, group, record); err != nil {
		return fmt.Errorf("poolactions: storing card %s: %w", group, err)
	}
	return nil
}

// DeleteCard removes a card: the CSMS deletes it, and its bucket record is deleted.
func (s *Service) DeleteCard(ctx context.Context, poolCode int, id string, linkedCPs []string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("poolactions: card without ID")
	}
	s.publisher.Publish(events.RFIDManagement{
		Header: events.NewHeader(events.TypeManageRFID, poolCode),
		CardID: strings.ToLower(id), Action: events.RFIDDelete, LinkedCPs: nonNil(linkedCPs),
	})
	if _, err := s.bucket.Delete(ctx, poolCode, CardVariable, strings.ToUpper(id), 0); err != nil {
		return fmt.Errorf("poolactions: deleting card %s: %w", strings.ToUpper(id), err)
	}
	return nil
}

// endregion

// region Rates

// Rates are a pool's energy rates (€/kWh, VAT included), its VAT and the pre-authorization amount of the virtual
// POS (€).
type Rates struct {
	OffPeak, Flat, Peak float64
	VAT                 float64 // As a fraction (0.21) or a percentage (21)
	Withholding         float64
}

// SetRates updates the rates: the CSMS gets the event, the rate_costs and withholding_amount records of the bucket
// are replaced and the configuration cache is updated (even when a bucket write fails, as the CSMS already has
// them).
func (s *Service) SetRates(ctx context.Context, poolCode int, r Rates) error {
	vatFraction := pools.VATFraction(r.VAT)
	s.publisher.Publish(events.RateList{
		Header:      events.NewHeader(events.TypeChangeRateList, poolCode),
		RateOffPeak: r.OffPeak, RateFlat: r.Flat, RatePeak: r.Peak,
		VAT: pools.VATPercentage(r.VAT), WithholdingAmount: r.Withholding,
	})

	ratesRecord := tagoio.DataInput{Variable: "rate_costs", Value: "0", Group: "1",
		Metadata: map[string]any{"valle": r.OffPeak, "llanas": r.Flat, "punta": r.Peak, "IVA": vatFraction}}
	withholdingRecord := tagoio.DataInput{Variable: "withholding_amount", Value: r.Withholding, Group: "1"}
	err := s.bucket.Replace(ctx, poolCode, "rate_costs", "", ratesRecord)
	if err == nil {
		err = s.bucket.Replace(ctx, poolCode, "withholding_amount", "", withholdingRecord)
	}

	s.configs.Update(poolCode, func(c *pools.Config) {
		c.RateOffPeak, c.RateFlat, c.RatePeak, c.VAT, c.PreauthAmount = r.OffPeak, r.Flat, r.Peak, vatFraction, r.Withholding
	})
	if err != nil {
		return fmt.Errorf("poolactions: storing rates: %w", err)
	}
	return nil
}

// endregion

// region Installation power

// MaxPowerVariable is the bucket variable of the power the installation may draw from the grid (W), as the
// management dashboard shows it.
const MaxPowerVariable = "max_grid_power_consumption"

// SetMaxPower sets the maximum power the installation may draw from the grid: the CSMS gets the event and the
// configuration cache is updated. With store, the dashboard record is replaced too (the TagoIO form writes it
// itself, the embedded panel doesn't).
func (s *Service) SetMaxPower(ctx context.Context, poolCode int, watts float64, store bool) error {
	s.publisher.Publish(events.MaxGridPower{
		Header:        events.NewHeader(events.TypeChangeMaxGridPower, poolCode),
		MaxPowerWatts: watts,
	})
	s.configs.Update(poolCode, func(c *pools.Config) { c.MaxPower = watts })
	if !store {
		return nil
	}
	record := tagoio.DataInput{Variable: MaxPowerVariable, Value: watts, Group: "1"}
	if err := s.bucket.Replace(ctx, poolCode, MaxPowerVariable, "", record); err != nil {
		return fmt.Errorf("poolactions: storing installation power: %w", err)
	}
	return nil
}

// endregion

// region Load balancing

// BalancingModes are the load balancing modes: the CSMS code and the label the dashboards show and store.
var BalancingModes = []struct{ Code, Label string }{
	{"NONE", "Ninguno"},
	{"STATICNORMAL", "Estático normal"},
	{"DYNAMICNORMAL", "Dinámico normal"},
	{"DYNAMICBOOST", "Dinámico boost"},
}

// BalancingModeLabel returns the dashboard label of a load balancing mode given by its label or its code, in any
// case, as the CSMS reads it; false for an empty or unknown mode.
func BalancingModeLabel(mode string) (string, bool) {
	mode = strings.TrimSpace(mode)
	for _, m := range BalancingModes {
		if strings.EqualFold(mode, m.Code) || strings.EqualFold(mode, m.Label) {
			return m.Label, true
		}
	}
	return "", false
}

// SetLoadBalancingMode selects the load balancing mode: the CSMS gets the event first, then the
// load_balancing_mode record of the bucket is replaced and the configuration cache is updated. An empty or unknown
// mode is still sent to the CSMS, which ignores it, but is neither stored nor cached (ErrUnknownMode). It returns
// the label of the stored mode.
func (s *Service) SetLoadBalancingMode(ctx context.Context, poolCode int, mode string) (string, error) {
	s.publisher.Publish(events.LoadBalancing{
		Header:       events.NewHeader(events.TypeChangeLoadBalancingMode, poolCode),
		SelectedMode: mode,
	})
	label, known := BalancingModeLabel(mode)
	if !known {
		return "", ErrUnknownMode
	}
	s.configs.Update(poolCode, func(c *pools.Config) { c.LoadBalancingMode = label })
	record := tagoio.DataInput{Variable: "load_balancing_mode", Value: label, Group: "1"}
	if err := s.bucket.Replace(ctx, poolCode, "load_balancing_mode", "", record); err != nil {
		return label, fmt.Errorf("poolactions: storing load balancing mode: %w", err)
	}
	return label, nil
}

// endregion

// region Operator details

// CPOInfo are the operator (CPO) details shown to the drivers.
type CPOInfo struct {
	Name, FiscalID, Address, Phone, Email, Web string
}

// SetCPOInfo updates the operator details: the CSMS gets the event and the configuration cache is updated. With
// store, the operator_info record of the bucket is replaced too, so the details survive a restart of this service
// and the management dashboard shows them.
func (s *Service) SetCPOInfo(ctx context.Context, poolCode int, info CPOInfo, store bool) (CPOInfo, error) {
	info = CPOInfo{
		Name: strings.TrimSpace(info.Name), FiscalID: CleanFiscalID(info.FiscalID),
		Address: strings.TrimSpace(info.Address), Phone: strings.TrimSpace(info.Phone),
		Email: strings.TrimSpace(info.Email), Web: strings.TrimSpace(info.Web),
	}
	s.publisher.Publish(events.CPOInfo{
		Header: events.NewHeader(events.TypeChangeCPOInfo, poolCode),
		Name:   info.Name, FiscalID: info.FiscalID, Address: info.Address,
		Phone: info.Phone, Email: info.Email, Web: info.Web,
	})
	s.configs.Update(poolCode, func(c *pools.Config) {
		c.CPOName, c.CPOFiscalID, c.CPOAddress = info.Name, info.FiscalID, info.Address
		c.CPOPhone, c.CPOEmail, c.CPOWeb = info.Phone, info.Email, info.Web
	})
	if !store {
		return info, nil
	}
	// Spanish metadata keys, as the dashboard table and the configuration cache read them
	record := tagoio.DataInput{Variable: "operator_info", Value: 0, Group: "1", Metadata: map[string]any{
		"nombre": info.Name, "CIF": info.FiscalID, "direccion": info.Address,
		"telefono": info.Phone, "correo": info.Email, "web": info.Web,
	}}
	if err := s.bucket.Replace(ctx, poolCode, "operator_info", "", record); err != nil {
		return info, fmt.Errorf("poolactions: storing operator details: %w", err)
	}
	return info, nil
}

// CleanFiscalID removes the spaces and dots of a tax ID ("B-74.454 166" -> "B-74454166").
func CleanFiscalID(id string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '.' {
			return -1
		}
		return r
	}, id)
}

// endregion

// region Stations

// ResetStation asks the CSMS for a soft reset (OCPP Reset, type Soft) of a station.
func (s *Service) ResetStation(poolCode int, stationName string) {
	s.publisher.Publish(events.OCPPRequest{
		Header:  events.NewHeader(events.TypeOCPPRequest, poolCode),
		Payload: events.ResetPayload{Request: events.RequestReset, StationNames: []string{stationName}, ResetType: "Soft"},
	})
}

// endregion

// optional returns nil for an empty string, so the event carries null.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNil(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}
