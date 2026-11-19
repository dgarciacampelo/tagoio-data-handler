// Package provisioning creates the TagoIO resources of a new charging point operator (CPO), from the sign-up form
// the notification service forwards: the MASTER device whose bucket holds the new charging pool's data, with its
// default configuration, and a dashboard user (new, or the existing user of the email, given the pool's group_id).
// It also ensures the device of an existing pool for the CSMS, and links a pool to an existing user.
package provisioning

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
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

// groupTag is the user and device tag that gives a run user access to a pool's device and dashboards.
const groupTag = "group_id"

// ErrUserNotFound is returned when no run user has the given email.
var ErrUserNotFound = errors.New("provisioning: no TagoIO user with that email")

// Result is the outcome of a CPO provisioning, step by step: the pool device first, then the user.
type Result struct {
	PoolCode int

	DeviceCreated bool
	DeviceID      string
	DeviceError   string

	UserCreated bool   // A new run user was created, with Password
	UserExisted bool   // A run user with the form's email already existed: no user was created
	UserLinked  bool   // The existing user has the pool's group_id tag (added now, or it had it already)
	UserError   string // Why the user step failed or was skipped
	Password    string // The new user's password, only when UserCreated

	ErrorMessage string // Every step's failure, "" when the CPO is fully provisioned
}

// Complete reports whether the pool has its device and a user that can reach it.
func (r Result) Complete() bool {
	return r.DeviceCreated && (r.UserCreated || r.UserLinked)
}

// ProvisionCPO creates the pool device of a new CPO, with its default configuration (written in the background),
// and gives the form's email a user that reaches it: a new run user, or, when the email already has one (a second
// pool of the same CPO, or a repeated sign-up), that user with the new pool's group_id tag added. The device goes
// first because it reserves the pool code; a user step failure doesn't undo it, and each step's outcome is returned.
func (s *Service) ProvisionCPO(ctx context.Context, form Form) (Result, error) {
	s.poolCodes.Lock()
	defer s.poolCodes.Unlock()

	poolCode, err := s.nextPoolCode(ctx)
	if err != nil {
		return Result{}, err
	}
	result := Result{PoolCode: poolCode}

	advancedPlan := strings.Contains(form.PaymentSystem, "TPV") // Pools with payment terminals
	device, err := s.createDevice(ctx, poolCode, advancedPlan)
	if err != nil {
		s.log.Error("Provisioning: device creation failed", "pool_code", poolCode, "error", err)
		result.DeviceError = "device creation failed: " + err.Error()
		// Without its device the pool code isn't reserved and could go to the next sign-up: no user gets it.
		result.UserError = "user step skipped: the pool device couldn't be created"
		result.ErrorMessage = joinErrors(result)
		return result, nil
	}
	result.DeviceCreated, result.DeviceID = true, device.DeviceID
	go s.writeDefaults(poolCode, form)

	s.provisionUser(ctx, form, poolCode, &result)
	result.ErrorMessage = joinErrors(result)
	s.log.Info("Provisioning: CPO provisioned", "pool_code", poolCode, "company", form.CompanyName,
		"user_created", result.UserCreated, "user_existed", result.UserExisted, "user_linked", result.UserLinked,
		"complete", result.Complete())
	return result, nil
}

// provisionUser gives the form's email a run user that reaches the pool: it links the existing user, or creates one.
func (s *Service) provisionUser(ctx context.Context, form Form, poolCode int, result *Result) {
	users, err := s.tago.FindUsersByEmail(ctx, form.UserEmail)
	if err != nil {
		// Unknown whether the user exists: try the creation, which TagoIO rejects for a taken email.
		s.log.Warn("Provisioning: user lookup failed, creating the user", "pool_code", poolCode, "error", err)
	}
	if len(users) > 0 {
		result.UserExisted = true
		_, linked, err := s.linkUser(ctx, users[0].ID, poolCode)
		if err != nil {
			s.log.Error("Provisioning: linking the existing user failed", "pool_code", poolCode, "user_id", users[0].ID, "error", err)
			result.UserError = "existing user found, but adding the pool's group_id tag failed: " + err.Error()
			return
		}
		result.UserLinked = true
		s.log.Info("Provisioning: existing user linked to the pool", "pool_code", poolCode, "user_id", users[0].ID, "tag_added", linked)
		return
	}

	password, err := generatePassword(16)
	if err != nil {
		result.UserError = "password generation failed: " + err.Error()
		return
	}
	err = s.tago.CreateUser(ctx, tagoio.NewUser{
		Name: form.ManagerName, Email: form.UserEmail, Password: password, Language: "es", Timezone: "Europe/Madrid",
		Phone: form.ContactPhone, Company: form.CompanyName, Active: true,
		Tags: []tagoio.Tag{
			{Key: "name", Value: form.ManagerName},
			{Key: groupTag, Value: strconv.Itoa(poolCode)}, // The pools the user sees
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
		result.UserError = "user creation failed: " + err.Error()
		return
	}
	result.UserCreated, result.Password = true, password
}

// linkUser adds the pool's group_id tag to a run user, keeping its other tags; added is false when the user had it.
func (s *Service) linkUser(ctx context.Context, userID string, poolCode int) (user tagoio.RunUser, added bool, err error) {
	user, err = s.tago.User(ctx, userID) // Its current tags: the edit replaces them all
	if err != nil {
		return tagoio.RunUser{}, false, err
	}
	value := strconv.Itoa(poolCode)
	if user.HasTag(groupTag, value) {
		return user, false, nil
	}
	tags := append(slices.Clone(user.Tags), tagoio.Tag{Key: groupTag, Value: value})
	if err := s.tago.SetUserTags(ctx, userID, tags); err != nil {
		return tagoio.RunUser{}, false, err
	}
	user.Tags = tags
	return user, true, nil
}

// LinkResult is the outcome of linking a pool to an existing run user.
type LinkResult struct {
	DeviceID      string
	DeviceCreated bool
	UserID        string
	TagAdded      bool // false when the user already had the pool's group_id tag
	GroupIDs      []string
}

// LinkPool gives the existing run user with that email access to a pool: it ensures the pool's MASTER device (see
// EnsureDevice) and adds the pool's group_id tag to the user, keeping its other tags. Calling it again is harmless.
func (s *Service) LinkPool(ctx context.Context, poolCode int, email string) (LinkResult, error) {
	users, err := s.tago.FindUsersByEmail(ctx, email)
	if err != nil {
		return LinkResult{}, fmt.Errorf("provisioning: looking up the user: %w", err)
	}
	if len(users) == 0 {
		return LinkResult{}, ErrUserNotFound
	}

	device, created, err := s.EnsureDevice(ctx, poolCode)
	if err != nil {
		return LinkResult{}, fmt.Errorf("provisioning: ensuring the pool device: %w", err)
	}
	result := LinkResult{DeviceID: device.DeviceID, DeviceCreated: created, UserID: users[0].ID}

	user, added, err := s.linkUser(ctx, users[0].ID, poolCode)
	if err != nil {
		return result, fmt.Errorf("provisioning: adding the pool's group_id tag: %w", err)
	}
	result.TagAdded = added
	for _, tag := range user.Tags {
		if tag.Key == groupTag {
			result.GroupIDs = append(result.GroupIDs, tag.Value)
		}
	}
	s.log.Info("Provisioning: pool linked to an existing user", "pool_code", poolCode, "user_id", user.ID,
		"device_created", created, "tag_added", added)
	return result, nil
}

// joinErrors returns the failures of a provisioning's steps, "" when there are none.
func joinErrors(r Result) string {
	var parts []string
	for _, e := range []string{r.DeviceError, r.UserError} {
		if e != "" {
			parts = append(parts, e)
		}
	}
	return strings.Join(parts, "; ")
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
