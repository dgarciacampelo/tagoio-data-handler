// Package devices keeps the registry of the charging pools' TagoIO devices: for each pool code, its MASTER device
// ID and device token (needed to read and write the pool's bucket). The registry lives in memory, backed by the
// store, and is completed from the TagoIO account when a pool or device is unknown.
package devices

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// unknownDeviceTTL is how long a device ID that is not in the account is remembered as unknown, so a stream of
// triggers from it does not rescan the account each time.
const unknownDeviceTTL = 10 * time.Minute

// ErrUnknownPool is returned for a pool without a device in the account.
var ErrUnknownPool = errors.New("devices: unknown pool")

// Device is a pool's TagoIO device.
type Device = store.Device

// Registry maps pool codes to their devices.
type Registry struct {
	store  *store.Store
	tago   *tagoio.Client
	prefix string // Device name prefix of the pools (TAGO_DEVICE_PREFIX)
	log    *slog.Logger

	mu       sync.RWMutex
	byPool   map[int]Device
	byDevice map[string]int       // Device ID -> pool code
	unknown  map[string]time.Time // Device ID -> when the last scan did not find it

	scans singleflight.Group
}

// NewRegistry creates an empty registry; call Load before using it.
func NewRegistry(s *store.Store, tago *tagoio.Client, prefix string, log *slog.Logger) *Registry {
	return &Registry{
		store: s, tago: tago, prefix: prefix, log: log,
		byPool: map[int]Device{}, byDevice: map[string]int{}, unknown: map[string]time.Time{},
	}
}

// Load fills the registry from the store. When the store has no device yet, it scans the TagoIO account.
func (r *Registry) Load(ctx context.Context) error {
	stored, err := r.store.Devices(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	for _, d := range stored {
		r.set(d)
	}
	r.mu.Unlock()

	if len(stored) == 0 {
		r.log.Info("Devices: store is empty, scanning the TagoIO account")
		if err := r.Rescan(ctx); err != nil {
			return err
		}
	}
	r.log.Info("Devices: registry loaded", "pools", len(r.PoolCodes()))
	return nil
}

// set records a device; the caller holds the write lock.
func (r *Registry) set(d Device) {
	if old, ok := r.byPool[d.PoolCode]; ok && old.DeviceID != d.DeviceID {
		delete(r.byDevice, old.DeviceID)
	}
	r.byPool[d.PoolCode] = d
	r.byDevice[d.DeviceID] = d.PoolCode
	delete(r.unknown, d.DeviceID)
	delete(r.unknown, "pool:"+strconv.Itoa(d.PoolCode))
}

// PoolCodes returns the registered pool codes, in no particular order.
func (r *Registry) PoolCodes() []int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	codes := make([]int, 0, len(r.byPool))
	for code := range r.byPool {
		codes = append(codes, code)
	}
	return codes
}

// Devices returns a copy of every registered device.
func (r *Registry) Devices() []Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	devices := make([]Device, 0, len(r.byPool))
	for _, d := range r.byPool {
		devices = append(devices, d)
	}
	return devices
}

// ByPool returns the device of a pool. An unregistered pool triggers a rescan of the TagoIO account.
func (r *Registry) ByPool(ctx context.Context, poolCode int) (Device, error) {
	r.mu.RLock()
	d, ok := r.byPool[poolCode]
	r.mu.RUnlock()
	if ok {
		return d, nil
	}
	if r.recentlyMissed("pool:" + strconv.Itoa(poolCode)) {
		return Device{}, fmt.Errorf("%w %d", ErrUnknownPool, poolCode)
	}

	if err := r.Rescan(ctx); err != nil {
		return Device{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.byPool[poolCode]; ok {
		return d, nil
	}
	r.markMissed("pool:" + strconv.Itoa(poolCode))
	return Device{}, fmt.Errorf("%w %d", ErrUnknownPool, poolCode)
}

// markMissed remembers that a scan did not find a key; the caller holds the write lock. Public pages take any
// pool code, so the expired entries are pruned before the map grows large.
func (r *Registry) markMissed(key string) {
	if len(r.unknown) >= 10_000 {
		for k, missedAt := range r.unknown {
			if time.Since(missedAt) >= unknownDeviceTTL {
				delete(r.unknown, k)
			}
		}
	}
	r.unknown[key] = time.Now()
}

// recentlyMissed reports whether a key (device ID or "pool:<code>") was not found by a recent scan.
func (r *Registry) recentlyMissed(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	missedAt, missed := r.unknown[key]
	return missed && time.Since(missedAt) < unknownDeviceTTL
}

// PoolByDevice returns the pool code of a device ID. An unknown device triggers one rescan of the account, unless
// the same device was not found recently.
func (r *Registry) PoolByDevice(ctx context.Context, deviceID string) (int, bool) {
	r.mu.RLock()
	code, ok := r.byDevice[deviceID]
	r.mu.RUnlock()
	if ok {
		return code, true
	}
	if r.recentlyMissed(deviceID) {
		return 0, false
	}

	r.log.Warn("Devices: unknown device, rescanning the TagoIO account", "device_id", deviceID)
	if err := r.Rescan(ctx); err != nil {
		r.log.Error("Devices: rescan failed", "error", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if code, ok := r.byDevice[deviceID]; ok {
		return code, true
	}
	r.markMissed(deviceID)
	return 0, false
}

// Register stores a device in the registry and the store.
func (r *Registry) Register(ctx context.Context, d Device) error {
	if err := r.store.UpsertDevice(ctx, d); err != nil {
		return err
	}
	r.mu.Lock()
	r.set(d)
	r.mu.Unlock()
	return nil
}

// Rescan reads every pool device of the TagoIO account and registers them. Concurrent calls share one scan.
func (r *Registry) Rescan(ctx context.Context) error {
	_, err, _ := r.scans.Do("rescan", func() (any, error) {
		// The scan outlives the request that started it: other callers wait for it too.
		ctx := context.WithoutCancel(ctx)
		devices, err := r.tago.ListDevices(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("devices: listing TagoIO devices: %w", err)
		}

		registered := 0
		for _, td := range devices {
			poolCode, ok := r.PoolCodeFromName(td.Name)
			if !ok {
				continue
			}
			r.mu.RLock()
			known, isKnown := r.byPool[poolCode]
			r.mu.RUnlock()
			if isKnown && known.DeviceID == td.ID {
				continue // Keep the stored token: it works, and fetching it again costs a call per device
			}

			token, err := r.tago.DeviceToken(ctx, td.ID)
			if err != nil || token == "" {
				r.log.Warn("Devices: no token for device", "device", td.Name, "error", err)
				continue
			}
			if err := r.Register(ctx, Device{PoolCode: poolCode, DeviceID: td.ID, Token: token}); err != nil {
				r.log.Error("Devices: registering device failed", "device", td.Name, "error", err)
				continue
			}
			registered++
		}
		r.log.Info("Devices: account scanned", "devices", len(devices), "newly_registered", registered)
		return nil, nil
	})
	return err
}

// FindByName looks up a device of the account by its exact name and returns it with its newest token and whether
// it has the ADVANCED plan tag.
func (r *Registry) FindByName(ctx context.Context, name string) (d Device, advancedPlan, found bool, err error) {
	devices, err := r.tago.ListDevices(ctx, name)
	if err != nil {
		return Device{}, false, false, fmt.Errorf("devices: looking up %s: %w", name, err)
	}
	for _, td := range devices {
		if td.Name != name {
			continue
		}
		token, err := r.tago.DeviceToken(ctx, td.ID)
		if err != nil {
			return Device{}, false, false, fmt.Errorf("devices: reading token of %s: %w", name, err)
		}
		if token == "" {
			return Device{}, false, false, fmt.Errorf("devices: %s has no token", name)
		}
		poolCode, _ := r.PoolCodeFromName(name)
		return Device{PoolCode: poolCode, DeviceID: td.ID, Token: token}, td.TagValue("plan") == "ADVANCED", true, nil
	}
	return Device{}, false, false, nil
}

// DeviceName returns the name of a pool's MASTER device, e.g. MASTER-BUSINESS-261001.
func (r *Registry) DeviceName(poolCode int) string {
	return "MASTER-BUSINESS-" + strconv.Itoa(poolCode)
}

// PoolCodeFromName extracts the pool code of a pool device name (MASTER-<INSTALLATION>-<POOL>).
func (r *Registry) PoolCodeFromName(name string) (int, bool) {
	if !strings.Contains(name, r.prefix) {
		return 0, false
	}
	parts := strings.Split(name, "-")
	if len(parts) < 3 {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil || code <= 0 {
		return 0, false
	}
	return code, true
}
