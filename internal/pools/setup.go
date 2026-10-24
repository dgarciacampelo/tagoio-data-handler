// This file builds the full pool setup the CSMS reads when it builds a charging pool (GET /api/pools/{pool_code}),
// including the RFID cards.

package pools

import (
	"context"
	"math"

	"golang.org/x/sync/errgroup"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// maxCards is the number of RFID cards read per pool.
const maxCards = 100

// RFIDCard is an RFID card of a pool.
type RFIDCard struct {
	CardID    string   `json:"card_id"`
	CardAlias string   `json:"card_alias"`
	LinkedTo  []string `json:"linked_to"` // Station serials (never null)
	Email     *string  `json:"email"`
}

// Setup is the pool setup sent to the CSMS.
//
// ! Wire contract: neos-ocpp parses it as PoolDeviceSetupResponse (charge_points/charging_pool_builder.py).
type Setup struct {
	PoolCode             int        `json:"pool_code"`
	IsNewlyCreated       bool       `json:"is_newly_created"`
	LoadBalancingMode    string     `json:"load_balancing_mode"`
	MaxInstallationPower float64    `json:"max_installation_power"`
	RateOffPeak          float64    `json:"rate_off_peak"`
	RateFlat             float64    `json:"rate_flat"`
	RatePeak             float64    `json:"rate_peak"`
	VAT                  float64    `json:"vat"` // Fraction
	PreauthAmount        float64    `json:"preauth_amount"`
	CPOName              *string    `json:"cpo_name"`
	CPOFiscalID          *string    `json:"cpo_fiscal_id"`
	CPOAddress           *string    `json:"cpo_address"`
	CPOPhone             *string    `json:"cpo_phone"`
	CPOEmail             *string    `json:"cpo_email"`
	CPOWeb               *string    `json:"cpo_web"`
	Cards                []RFIDCard `json:"cards"`
}

// newSetup returns the setup of a pool without configuration in TagoIO (the CSMS defaults).
func newSetup(poolCode int, isNewlyCreated bool) Setup {
	return Setup{
		PoolCode: poolCode, IsNewlyCreated: isNewlyCreated, LoadBalancingMode: "Ninguno",
		MaxInstallationPower: 5000, RateOffPeak: 0.4, RateFlat: 0.4, RatePeak: 0.4, VAT: 0.1, PreauthAmount: 40,
		Cards: []RFIDCard{},
	}
}

// FetchSetup reads a pool's full setup from its bucket. A newly created device has no data yet, so it gets the
// defaults. Values that fail to load keep their defaults, as before.
func (c *Cache) FetchSetup(ctx context.Context, poolCode int, isNewlyCreated bool) Setup {
	setup := newSetup(poolCode, isNewlyCreated)
	if isNewlyCreated {
		return setup
	}

	var (
		cards                                          []tagoio.DataRecord
		operator, power, balancing, rates, withholding *tagoio.DataRecord
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(6)
	g.Go(func() error { cards = c.list(gctx, poolCode, "card_id", maxCards); return nil })
	g.Go(func() error { operator = c.last(gctx, poolCode, "operator_info"); return nil })
	g.Go(func() error { power = c.last(gctx, poolCode, "max_installation_power"); return nil })
	g.Go(func() error { balancing = c.last(gctx, poolCode, "load_balancing_mode"); return nil })
	g.Go(func() error { rates = c.last(gctx, poolCode, "rate_costs"); return nil })
	g.Go(func() error { withholding = c.last(gctx, poolCode, "withholding_amount"); return nil })
	_ = g.Wait()

	for _, r := range cards {
		cardID := tagoio.ValueString(r.Value)
		if cardID == "" {
			continue
		}
		card := RFIDCard{CardID: cardID, CardAlias: cardID, LinkedTo: []string{}}
		if alias := tagoio.ValueString(r.Metadata["alias"]); alias != "" {
			card.CardAlias = alias
		}
		card.LinkedTo = SplitList(tagoio.ValueString(r.Metadata["cps"]))
		if v, ok := r.Metadata["email"]; ok && v != nil {
			email := tagoio.ValueString(v)
			card.Email = &email
		}
		setup.Cards = append(setup.Cards, card)
	}

	if operator != nil {
		meta := operator.Metadata
		setup.CPOName = metaString(meta, "nombre")
		setup.CPOFiscalID = metaString(meta, "CIF")
		setup.CPOAddress = metaString(meta, "direccion")
		setup.CPOPhone = metaString(meta, "telefono")
		setup.CPOEmail = metaString(meta, "correo")
		setup.CPOWeb = metaString(meta, "web")
	}
	if power != nil {
		if f, err := tagoio.ValueFloat(power.Value); err == nil {
			setup.MaxInstallationPower = f
		}
	}
	if balancing != nil {
		if mode := tagoio.ValueString(balancing.Value); mode != "" {
			setup.LoadBalancingMode = mode
		}
	}
	if rates != nil {
		cfg := Config{RateOffPeak: setup.RateOffPeak, RateFlat: setup.RateFlat, RatePeak: setup.RatePeak, VAT: setup.VAT}
		applyRates(&cfg, rates.Metadata)
		setup.RateOffPeak, setup.RateFlat, setup.RatePeak, setup.VAT = cfg.RateOffPeak, cfg.RateFlat, cfg.RatePeak, cfg.VAT
	}
	if withholding != nil {
		if f, err := tagoio.ValueFloat(withholding.Value); err == nil {
			setup.PreauthAmount = f
		}
	}
	return setup
}

func (c *Cache) last(ctx context.Context, poolCode int, variable string) *tagoio.DataRecord {
	record, err := c.reader.LastValue(ctx, poolCode, variable)
	if err != nil {
		c.log.Warn("Pools: reading setup value failed", "pool_code", poolCode, "variable", variable, "error", err)
	}
	return record
}

func (c *Cache) list(ctx context.Context, poolCode int, variable string, qty int) []tagoio.DataRecord {
	records, err := c.reader.List(ctx, poolCode, variable, qty)
	if err != nil {
		c.log.Warn("Pools: reading setup list failed", "pool_code", poolCode, "variable", variable, "error", err)
	}
	return records
}

// metaString returns a metadata value as a string, or nil when the key is missing or null.
func metaString(meta map[string]any, key string) *string {
	v, ok := meta[key]
	if !ok || v == nil {
		return nil
	}
	s := tagoio.ValueString(v)
	return &s
}

// roundTo rounds f to the given number of decimals (to undo float noise such as 0.1*100 = 10.000000000000002).
func roundTo(f float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	return math.Round(f*scale) / scale
}
