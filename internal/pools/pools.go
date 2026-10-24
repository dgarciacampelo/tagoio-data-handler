// Package pools caches the configuration of each charging pool, read from its TagoIO bucket: the operator (CPO)
// details, the energy rates and VAT, the payment pre-authorization amount, the installation power and the load
// balancing mode. The public station page and the payment requests read it; the TagoIO analyses update it in
// place when the CPO changes a value.
package pools

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// Config is a pool's configuration. Loaded is false until it was read from TagoIO; the defaults apply meanwhile.
type Config struct {
	Loaded bool

	CPOName     string
	CPOFiscalID string
	CPOAddress  string
	CPOPhone    string
	CPOEmail    string
	CPOWeb      string

	RateOffPeak float64 // €/kWh
	RateFlat    float64
	RatePeak    float64
	VAT         float64 // Fraction (0.1 for 10 %)

	PreauthAmount     float64 // € held on the driver's card by the virtual POS
	MaxPower          float64 // W
	LoadBalancingMode string
}

// Defaults returns the configuration used until a pool's own is loaded (the Python service's defaults).
func Defaults() Config {
	return Config{
		CPOName: "PRUEBA DE DATOS S.L.", CPOFiscalID: "B74454166", CPOAddress: "OLLONIEGO",
		CPOPhone: "985790454", CPOEmail: "comercial@velo-energy.com", CPOWeb: "https://velo-energy.com",
		RateOffPeak: 0.4, RateFlat: 0.4, RatePeak: 0.4, VAT: 0.10,
		PreauthAmount: 40.0, MaxPower: 5000.0, LoadBalancingMode: "Ninguno",
	}
}

// Reader reads a pool's bucket.
type Reader interface {
	LastValue(ctx context.Context, poolCode int, variable string) (*tagoio.DataRecord, error)
	List(ctx context.Context, poolCode int, variable string, qty int) ([]tagoio.DataRecord, error)
}

const (
	loadConcurrency = 10               // Pools loaded at once (TagoIO rate limits)
	retryInterval   = time.Minute      // Minimum time between two load attempts of a pool that failed
	loadTimeout     = 30 * time.Second // Per pool
)

type entry struct {
	config      Config
	updatedAt   time.Time // Last in-place update (an analysis)
	attemptedAt time.Time // Last load attempt
}

// Cache holds the configuration of every pool.
type Cache struct {
	reader Reader
	log    *slog.Logger

	mu      sync.RWMutex
	entries map[int]*entry
}

// NewCache creates an empty cache.
func NewCache(reader Reader, log *slog.Logger) *Cache {
	return &Cache{reader: reader, log: log, entries: map[int]*entry{}}
}

// Get returns a pool's configuration (the defaults, not loaded, when it was never read).
func (c *Cache) Get(poolCode int) Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if e, ok := c.entries[poolCode]; ok {
		return e.config
	}
	return Defaults()
}

// GetOrLoad returns a pool's configuration, loading it first when it was never loaded (at most once a minute per
// pool, so an unreachable TagoIO does not slow every page view).
func (c *Cache) GetOrLoad(ctx context.Context, poolCode int) Config {
	c.mu.RLock()
	e, ok := c.entries[poolCode]
	due := !ok || (!e.config.Loaded && time.Since(e.attemptedAt) >= retryInterval)
	c.mu.RUnlock()
	if due {
		c.load(ctx, poolCode)
	}
	return c.Get(poolCode)
}

// LoadAll loads the configuration of the given pools, a few at a time.
func (c *Cache) LoadAll(ctx context.Context, poolCodes []int) {
	c.log.Info("Pools: loading configurations", "pools", len(poolCodes))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(loadConcurrency)
	for _, code := range poolCodes {
		g.Go(func() error {
			c.load(ctx, code)
			return nil
		})
	}
	_ = g.Wait()
	c.log.Info("Pools: configurations loaded")
}

// Update changes a pool's configuration in place (the analyses call it when the CPO changes a value).
func (c *Cache) Update(poolCode int, change func(*Config)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entry(poolCode)
	change(&e.config)
	e.updatedAt = time.Now()
	c.log.Info("Pools: configuration updated in place", "pool_code", poolCode)
}

// entry returns a pool's entry, creating it with the defaults; the caller holds the write lock.
func (c *Cache) entry(poolCode int) *entry {
	e, ok := c.entries[poolCode]
	if !ok {
		e = &entry{config: Defaults()}
		c.entries[poolCode] = e
	}
	return e
}

// load reads a pool's configuration from its bucket. Values that fail to load keep their current value, and a
// value changed in place while loading is not overwritten by the (older) one read from TagoIO.
func (c *Cache) load(ctx context.Context, poolCode int) {
	started := time.Now()
	c.mu.Lock()
	c.entry(poolCode).attemptedAt = started
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()

	var changes []func(*Config)
	failed := false
	read := func(variable string) *tagoio.DataRecord {
		record, err := c.reader.LastValue(ctx, poolCode, variable)
		if err != nil {
			failed = true
			c.log.Warn("Pools: reading configuration value failed", "pool_code", poolCode, "variable", variable, "error", err)
		}
		return record
	}

	if r := read("operator_info"); r != nil {
		changes = append(changes, func(cfg *Config) { applyOperatorInfo(cfg, r.Metadata) })
	}
	if r := read("rate_costs"); r != nil {
		changes = append(changes, func(cfg *Config) { applyRates(cfg, r.Metadata) })
	}
	if r := read("max_installation_power"); r != nil {
		if power, err := tagoio.ValueFloat(r.Value); err == nil {
			changes = append(changes, func(cfg *Config) { cfg.MaxPower = power })
		}
	}
	if r := read("withholding_amount"); r != nil {
		if amount, err := tagoio.ValueFloat(r.Value); err == nil {
			changes = append(changes, func(cfg *Config) { cfg.PreauthAmount = amount })
		}
	}
	if r := read("load_balancing_mode"); r != nil {
		if mode := tagoio.ValueString(r.Value); mode != "" {
			changes = append(changes, func(cfg *Config) { cfg.LoadBalancingMode = mode })
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entry(poolCode)
	if e.updatedAt.Before(started) {
		for _, change := range changes {
			change(&e.config)
		}
	}
	if !failed {
		e.config.Loaded = true
		c.log.Info("Pools: configuration loaded", "pool_code", poolCode, "cpo", e.config.CPOName,
			"rates", []float64{e.config.RateOffPeak, e.config.RateFlat, e.config.RatePeak}, "vat", e.config.VAT,
			"preauth_amount", e.config.PreauthAmount)
	}
}

// applyOperatorInfo copies the operator_info metadata (Spanish keys, as the TagoIO dashboard writes them).
func applyOperatorInfo(cfg *Config, meta map[string]any) {
	set := func(dst *string, key string) {
		if v, ok := meta[key]; ok && v != nil {
			*dst = tagoio.ValueString(v)
		}
	}
	set(&cfg.CPOName, "nombre")
	set(&cfg.CPOFiscalID, "CIF")
	set(&cfg.CPOAddress, "direccion")
	set(&cfg.CPOPhone, "telefono")
	set(&cfg.CPOEmail, "correo")
	set(&cfg.CPOWeb, "web")
}

// applyRates copies the rate_costs metadata: valle (off-peak), llanas (flat), punta (peak) and IVA (VAT, as a
// fraction or a percentage).
func applyRates(cfg *Config, meta map[string]any) {
	set := func(dst *float64, key string) {
		if v, ok := meta[key]; ok {
			if f, err := tagoio.ValueFloat(v); err == nil {
				*dst = f
			}
		}
	}
	set(&cfg.RateOffPeak, "valle")
	set(&cfg.RateFlat, "llanas")
	set(&cfg.RatePeak, "punta")
	set(&cfg.VAT, "IVA")
	cfg.VAT = VATFraction(cfg.VAT)
}

// VATFraction returns a VAT as a fraction: values above 1 are percentages (10 means 10 %).
func VATFraction(vat float64) float64 {
	if vat > 1 {
		return roundTo(vat/100, 6)
	}
	return vat
}

// VATPercentage returns a VAT as a percentage: values up to 1 are fractions (0.1 means 10 %).
func VATPercentage(vat float64) float64 {
	if vat <= 1 {
		return roundTo(vat*100, 6)
	}
	return vat
}

// SplitList splits a comma-separated list, trimming the items and dropping the empty ones.
func SplitList(s string) []string {
	items := []string{}
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
