// Package provisioning creates the TagoIO resources of a new charging point operator (CPO), from the sign-up form
// the notification service forwards: a dashboard user, and the MASTER device whose bucket holds the new charging
// pool's data, with its default configuration. It also ensures the device of an existing pool for the CSMS.
package provisioning

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// serverAlias tags the devices this service creates with the CSMS that manages them.
const serverAlias = "NEOS"

// Bucket writes a pool's bucket.
type Bucket interface {
	Insert(ctx context.Context, poolCode int, records ...tagoio.DataInput) error
}

// Service provisions CPOs and pool devices.
type Service struct {
	tago     *tagoio.Client
	devices  *devices.Registry
	store    *store.Store
	bucket   Bucket
	log      *slog.Logger
	location *time.Location

	poolCodes sync.Mutex // Pool code allocation, so two sign-ups never get the same code
}

// New creates the provisioning service.
func New(tago *tagoio.Client, registry *devices.Registry, s *store.Store, b Bucket, log *slog.Logger, location *time.Location) *Service {
	return &Service{tago: tago, devices: registry, store: s, bucket: b, log: log, location: location}
}

// Result is the outcome of a CPO provisioning.
type Result struct {
	UserCreated   bool
	DeviceCreated bool
	PoolCode      int
	DeviceID      string
	Password      string
	ErrorMessage  string
}

// ProvisionCPO creates the TagoIO user and pool device of a new CPO. A user creation failure stops it before the
// device; the default configuration is written in the background.
func (s *Service) ProvisionCPO(ctx context.Context, form Form) (Result, error) {
	s.poolCodes.Lock()
	defer s.poolCodes.Unlock()

	poolCode, err := s.nextPoolCode(ctx)
	if err != nil {
		return Result{}, err
	}
	password, err := generatePassword(16)
	if err != nil {
		return Result{}, err
	}
	result := Result{PoolCode: poolCode, Password: password}

	err = s.tago.CreateUser(ctx, tagoio.NewUser{
		Name: form.ManagerName, Email: form.UserEmail, Password: password, Language: "es", Timezone: "Europe/Madrid",
		Phone: form.ContactPhone, Company: form.CompanyName, Active: true,
		Tags: []tagoio.Tag{
			{Key: "name", Value: form.ManagerName},
			{Key: "group_id", Value: strconv.Itoa(poolCode)}, // The user only sees this pool
			{Key: "installation_type", Value: "BUSINESS"},
			{Key: "plan", Value: "ADVANCED"},
			{Key: "user_type", Value: "MANAGER"},
			{Key: "business_name", Value: form.CompanyName},
			{Key: "business_email", Value: form.ContactEmail},
			{Key: "business_phone", Value: form.ContactPhone},
		},
	})
	if err != nil {
		s.log.Error("Provisioning: user creation failed", "pool_code", poolCode, "error", err)
		result.ErrorMessage = "Failed to create TagoIO user. Aborting device creation."
		return result, nil
	}
	result.UserCreated = true

	advancedPlan := strings.Contains(form.PaymentSystem, "TPV") // Pools with payment terminals
	device, err := s.createDevice(ctx, poolCode, advancedPlan)
	if err != nil {
		s.log.Error("Provisioning: device creation failed", "pool_code", poolCode, "error", err)
		result.ErrorMessage = "User created, but device provisioning failed."
		return result, nil
	}
	result.DeviceCreated = true
	result.DeviceID = device.DeviceID

	go s.writeDefaults(poolCode, form)
	s.log.Info("Provisioning: CPO provisioned", "pool_code", poolCode, "company", form.CompanyName)
	return result, nil
}

// EnsureDevice returns the MASTER device of a pool, creating it (with the ADVANCED plan) when the account has
// none; created reports which.
func (s *Service) EnsureDevice(ctx context.Context, poolCode int) (d devices.Device, created bool, err error) {
	name := s.devices.DeviceName(poolCode)
	d, _, found, err := s.devices.FindByName(ctx, name)
	if err != nil {
		return devices.Device{}, false, err
	}
	if found {
		d.PoolCode = poolCode
		if err := s.devices.Register(ctx, d); err != nil {
			s.log.Warn("Provisioning: registering found device failed", "pool_code", poolCode, "error", err)
		}
		return d, false, nil
	}
	d, err = s.createDevice(ctx, poolCode, true)
	return d, err == nil, err
}

// createDevice creates and registers a pool's MASTER device.
func (s *Service) createDevice(ctx context.Context, poolCode int, advancedPlan bool) (devices.Device, error) {
	name := s.devices.DeviceName(poolCode)
	plan := "BASIC"
	if advancedPlan {
		plan = "ADVANCED"
	}
	id, token, err := s.tago.CreateDevice(ctx, tagoio.NewDevice{
		Name:        name,
		Description: "Registrado automáticamente en la siguiente fecha: " + time.Now().In(s.location).Format(time.RFC3339) + ".",
		Active:      true, Visible: true, Type: "mutable",
		Tags: []tagoio.Tag{
			{Key: "name", Value: name},
			{Key: "installation_type", Value: "BUSINESS"},
			{Key: "group_id", Value: strconv.Itoa(poolCode)},
			{Key: "manager_id", Value: "Unknown"},
			{Key: "type", Value: "MASTER"},
			{Key: "plan", Value: plan},
			{Key: "server_alias", Value: serverAlias},
		},
	})
	if err != nil {
		return devices.Device{}, err
	}
	d := devices.Device{PoolCode: poolCode, DeviceID: id, Token: token}
	if err := s.devices.Register(ctx, d); err != nil {
		return devices.Device{}, fmt.Errorf("provisioning: registering device %s: %w", name, err)
	}
	return d, nil
}

// writeDefaults writes the default configuration of a new pool (flat 0.40 €/kWh rates, 10 % VAT, 5 kW, no load
// balancing, 40 € pre-authorization) and its operator details.
func (s *Service) writeDefaults(poolCode int, form Form) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	records := []tagoio.DataInput{
		{Variable: "load_balancing_mode", Value: "Ninguno", Group: "1"},
		{Variable: "max_installation_power", Value: 5000, Group: "1"},
		{Variable: "max_grid_power_consumption", Value: 5000, Group: "1"},
		{Variable: "withholding_amount", Value: 40.0, Group: "1"},
		{Variable: "rate_costs", Value: 0, Group: "1", Metadata: map[string]any{"valle": 0.4, "llanas": 0.4, "punta": 0.4, "IVA": 0.1}},
		{Variable: "operator_info", Value: 0, Group: "1", Metadata: map[string]any{
			"web": "", "nombre": form.CompanyName, "CIF": form.FiscalID, "direccion": form.CompanyAddress,
			"telefono": form.ContactPhone, "correo": form.ContactEmail,
		}},
	}
	for _, record := range records {
		if err := s.bucket.Insert(ctx, poolCode, record); err != nil {
			s.log.Error("Provisioning: writing default variable failed", "pool_code", poolCode, "variable", record.Variable, "error", err)
		}
	}
}

// nextPoolCode returns the next pool code: the two digits of the year and a sequence from 1001 (261001, 261002…),
// above every code of the year in the store and in the account. The caller holds the allocation lock.
func (s *Service) nextPoolCode(ctx context.Context) (int, error) {
	year := time.Now().In(s.location).Format("06")
	sequence := 1000

	consider := func(code string) {
		if len(code) == 6 && strings.HasPrefix(code, year) {
			if n, err := strconv.Atoi(code[2:]); err == nil {
				sequence = max(sequence, n)
			}
		}
	}

	if code, ok, err := s.store.MaxPoolCode(ctx); err != nil {
		return 0, err
	} else if ok {
		consider(strconv.Itoa(code))
	}
	devices, err := s.tago.ListDevices(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("provisioning: listing devices: %w", err)
	}
	for _, d := range devices {
		if code, ok := strings.CutPrefix(d.Name, "MASTER-BUSINESS-"); ok {
			consider(strings.TrimSpace(code))
		}
	}
	return strconv.Atoi(year + strconv.Itoa(sequence+1))
}

// generatePassword returns a random password of letters, digits and symbols.
func generatePassword(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	password := make([]byte, length)
	for i := range password {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		password[i] = alphabet[n.Int64()]
	}
	return string(password), nil
}
