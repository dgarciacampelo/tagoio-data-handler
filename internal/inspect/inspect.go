// Package inspect reads the TagoIO account for the operators, without the account token ever leaving the service:
// the pool devices, the run users, and a per-pool consistency report (is there a device, is it registered here,
// which users see the pool, what configuration its bucket holds). It only reads: it never writes to TagoIO, never
// registers a device it finds, and never returns a device token, a user password or the account token.
package inspect

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/dgarciacampelo/tagoio-handler/internal/bucket"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// ConfigVariables are the configuration variables of a pool bucket the report shows, as the provisioning writes
// them.
var ConfigVariables = []string{
	"operator_info",
	"rate_costs",
	"max_installation_power",
	"max_grid_power_consumption",
	"load_balancing_mode",
	"withholding_amount",
}

// Stations lists the connectors the service knows per pool.
type Stations interface {
	PoolConnectors(poolCode int) []bucket.Connector
}

// Service reads the TagoIO account.
type Service struct {
	tago     *tagoio.Client
	devices  *devices.Registry
	stations Stations
	log      *slog.Logger
}

// New creates the inspection service.
func New(tago *tagoio.Client, registry *devices.Registry, stations Stations, log *slog.Logger) *Service {
	return &Service{tago: tago, devices: registry, stations: stations, log: log}
}

// region Devices

// Device is a device of the account as the inspection shows it.
type Device struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	PoolCode  int          `json:"pool_code,omitempty"` // From the name; 0 for a device that is not a pool's
	Tags      []tagoio.Tag `json:"tags"`
	Active    bool         `json:"active"`
	Type      string       `json:"type,omitempty"`
	CreatedAt string       `json:"created_at,omitempty"`
	UpdatedAt string       `json:"updated_at,omitempty"`
	LastInput string       `json:"last_input,omitempty"`
	// Registered reports whether the service has this device in its registry (and store), with the same ID.
	Registered bool `json:"registered"`
}

// DevicesReport lists the devices of the account.
type DevicesReport struct {
	Devices []Device `json:"devices"`
	// RegisteredWithoutDevice are the pools the service has registered whose device ID is not in the account.
	RegisteredWithoutDevice []RegisteredDevice `json:"registered_without_device"`
}

// RegisteredDevice is a device of the service's registry (without its token).
type RegisteredDevice struct {
	PoolCode int    `json:"pool_code"`
	DeviceID string `json:"device_id"`
}

// Devices lists every device of the account.
func (s *Service) Devices(ctx context.Context) (DevicesReport, error) {
	details, err := s.tago.ListDeviceDetails(ctx, "")
	if err != nil {
		return DevicesReport{}, fmt.Errorf("inspect: listing devices: %w", err)
	}
	registered := s.registeredByID()

	report := DevicesReport{Devices: make([]Device, 0, len(details)), RegisteredWithoutDevice: []RegisteredDevice{}}
	inAccount := make(map[string]bool, len(details))
	for _, d := range details {
		inAccount[d.ID] = true
		report.Devices = append(report.Devices, s.device(d, registered))
	}
	for id, poolCode := range registered {
		if !inAccount[id] {
			report.RegisteredWithoutDevice = append(report.RegisteredWithoutDevice, RegisteredDevice{PoolCode: poolCode, DeviceID: id})
		}
	}
	slices.SortFunc(report.RegisteredWithoutDevice, func(a, b RegisteredDevice) int { return cmp.Compare(a.PoolCode, b.PoolCode) })
	return report, nil
}

func (s *Service) device(d tagoio.DeviceDetails, registered map[string]int) Device {
	poolCode, _ := s.devices.PoolCodeFromName(d.Name)
	_, isRegistered := registered[d.ID]
	return Device{
		ID: d.ID, Name: d.Name, PoolCode: poolCode, Tags: nonNilTags(d.Tags), Active: d.Active, Type: string(d.Type),
		CreatedAt: string(d.CreatedAt), UpdatedAt: string(d.UpdatedAt), LastInput: string(d.LastInput),
		Registered: isRegistered,
	}
}

// registeredByID returns the registry's devices, by device ID.
func (s *Service) registeredByID() map[string]int {
	byID := map[string]int{}
	for _, d := range s.devices.Devices() {
		byID[d.DeviceID] = d.PoolCode
	}
	return byID
}

// endregion

// region Users

// User is a run user as the inspection shows it (no password, no phone).
type User struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Email     string       `json:"email"`
	Company   string       `json:"company,omitempty"`
	Tags      []tagoio.Tag `json:"tags"`
	GroupIDs  []string     `json:"group_ids"` // Every group_id tag: the pools the user sees
	Active    bool         `json:"active"`
	CreatedAt string       `json:"created_at,omitempty"`
	LastLogin string       `json:"last_login,omitempty"`
}

// UserQuery selects run users by email and/or group_id tag; at least one is required.
type UserQuery struct {
	Email   string
	GroupID string
}

// ErrEmptyQuery is returned for a user query without email nor group_id.
var ErrEmptyQuery = fmt.Errorf("inspect: an email or a group_id is required")

// Users returns the run users matching the query. TagoIO is asked for the email in lower case (how it compares
// emails isn't documented), and the filter is applied here again, the email case-insensitively, so the answer is
// right even if TagoIO ignored or widened it.
func (s *Service) Users(ctx context.Context, q UserQuery) ([]User, error) {
	q.Email, q.GroupID = strings.ToLower(strings.TrimSpace(q.Email)), strings.TrimSpace(q.GroupID)
	if q.Email == "" && q.GroupID == "" {
		return nil, ErrEmptyQuery
	}
	filter := tagoio.UserFilter{Email: q.Email}
	if q.GroupID != "" {
		filter.Tags = []tagoio.Tag{{Key: "group_id", Value: q.GroupID}}
	}
	found, err := s.tago.ListRunUsers(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("inspect: listing users: %w", err)
	}

	users := []User{}
	for _, u := range found {
		if q.Email != "" && !strings.EqualFold(strings.TrimSpace(u.Email), q.Email) {
			continue
		}
		if q.GroupID != "" && !slices.Contains(u.TagValues("group_id"), q.GroupID) {
			continue
		}
		users = append(users, user(u))
	}
	return users, nil
}

func user(u tagoio.RunUser) User {
	groupIDs := u.TagValues("group_id")
	if groupIDs == nil {
		groupIDs = []string{}
	}
	return User{
		ID: u.ID, Name: u.Name, Email: u.Email, Company: u.Company, Tags: nonNilTags(u.Tags), GroupIDs: groupIDs,
		Active: u.Active, CreatedAt: string(u.CreatedAt), LastLogin: string(u.LastLogin),
	}
}

// endregion

// region Pool report

// Variable is the latest record of a configuration variable.
type Variable struct {
	Value    any            `json:"value"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Group    string         `json:"group,omitempty"`
	Time     string         `json:"time,omitempty"`
}

// PoolReport is the state of a pool across TagoIO and the service.
type PoolReport struct {
	PoolCode   int    `json:"pool_code"`
	DeviceName string `json:"device_name"` // The name its MASTER device must have
	// Devices are the account devices of the pool: by name, or carrying its group_id tag.
	Devices []Device `json:"devices"`
	// Registered is the device the service's registry holds for the pool (nil when none).
	Registered *RegisteredDevice `json:"registered"`
	// Stations are the stations the service knows in the pool (from the CSMS updates).
	Stations []string `json:"stations"`
	// DataAmount is the number of records in the bucket of the pool's device (nil when unknown).
	DataAmount *int `json:"data_amount"`
	// Configuration holds the latest record of each configuration variable (null when the bucket has none).
	Configuration map[string]*Variable `json:"configuration"`
	// Users are the run users carrying the pool's group_id tag.
	Users []User `json:"users"`
	// OperatorEmailUsers are the run users whose email is the operator email in operator_info (a CPO that signed
	// up twice has its user in another pool).
	OperatorEmailUsers []User `json:"operator_email_users"`
	// Findings are the inconsistencies and notable facts, in plain words.
	Findings []string `json:"findings"`
	// Errors are the reads that failed; the rest of the report is still filled in.
	Errors []string `json:"errors"`
}

// Pool returns the report of a pool. Only the device listing must succeed; every other read that fails is listed
// in Errors.
func (s *Service) Pool(ctx context.Context, poolCode int) (PoolReport, error) {
	report := PoolReport{
		PoolCode: poolCode, DeviceName: s.devices.DeviceName(poolCode), Devices: []Device{}, Stations: []string{},
		Configuration: map[string]*Variable{}, Users: []User{}, OperatorEmailUsers: []User{},
		Findings: []string{}, Errors: []string{},
	}
	code := strconv.Itoa(poolCode)

	details, err := s.tago.ListDeviceDetails(ctx, "")
	if err != nil {
		return PoolReport{}, fmt.Errorf("inspect: listing devices: %w", err)
	}
	registered := s.registeredByID()
	var master *Device
	for _, d := range details {
		namePool, _ := s.devices.PoolCodeFromName(d.Name)
		if namePool != poolCode && d.TagValue("group_id") != code {
			continue
		}
		device := s.device(d, registered)
		report.Devices = append(report.Devices, device)
		if d.Name == report.DeviceName && master == nil {
			master = &device
		}
	}
	for id, registeredPool := range registered {
		if registeredPool == poolCode {
			report.Registered = &RegisteredDevice{PoolCode: poolCode, DeviceID: id}
		}
	}
	report.Stations = s.poolStations(poolCode)

	// The bucket is read with the registered device's token, or, for a device the service hasn't registered, with
	// the token TagoIO gives for it (read only, never returned).
	var deviceID, token string
	if master != nil {
		deviceID = master.ID
		token, err = s.deviceToken(ctx, master.ID)
		if err != nil {
			report.Errors = append(report.Errors, "device token: "+err.Error())
		}
	}

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	failed := func(what string, err error) {
		mu.Lock()
		report.Errors = append(report.Errors, what+": "+err.Error())
		mu.Unlock()
	}
	if deviceID != "" {
		g.Go(func() error {
			amount, err := s.tago.DataAmount(gctx, deviceID)
			if err != nil {
				failed("data amount", err)
				return nil
			}
			mu.Lock()
			report.DataAmount = &amount
			mu.Unlock()
			return nil
		})
	}
	for _, variable := range ConfigVariables {
		report.Configuration[variable] = nil
		if token == "" {
			continue
		}
		g.Go(func() error {
			record, err := s.tago.LastValue(gctx, token, variable)
			if err != nil {
				failed(variable, err)
				return nil
			}
			if record != nil {
				mu.Lock()
				report.Configuration[variable] = &Variable{Value: record.Value, Metadata: record.Metadata, Group: record.Group, Time: record.Time}
				mu.Unlock()
			}
			return nil
		})
	}
	g.Go(func() error {
		users, err := s.Users(gctx, UserQuery{GroupID: code})
		if err != nil {
			failed("users", err)
			return nil
		}
		mu.Lock()
		report.Users = users
		mu.Unlock()
		return nil
	})
	_ = g.Wait() // Every goroutine reports its failure in Errors and returns nil

	if email := operatorEmail(report.Configuration["operator_info"]); email != "" {
		users, err := s.Users(ctx, UserQuery{Email: email})
		if err != nil {
			failed("operator email users", err)
		} else {
			report.OperatorEmailUsers = users
		}
	}

	slices.Sort(report.Errors)
	report.Findings = findings(report, master)
	return report, nil
}

// deviceToken returns the token of a device: the registry's when it has the device, else TagoIO's.
func (s *Service) deviceToken(ctx context.Context, deviceID string) (string, error) {
	for _, d := range s.devices.Devices() {
		if d.DeviceID == deviceID && d.Token != "" {
			return d.Token, nil
		}
	}
	token, err := s.tago.DeviceToken(ctx, deviceID)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("device %s has no token", deviceID)
	}
	return token, nil
}

func (s *Service) poolStations(poolCode int) []string {
	stations := []string{}
	if s.stations == nil {
		return stations
	}
	for _, c := range s.stations.PoolConnectors(poolCode) {
		if !slices.Contains(stations, c.StationName) {
			stations = append(stations, c.StationName)
		}
	}
	slices.Sort(stations)
	return stations
}

// operatorEmail returns the operator email ("correo") of an operator_info record.
func operatorEmail(v *Variable) string {
	if v == nil {
		return ""
	}
	email, _ := v.Metadata["correo"].(string)
	return strings.TrimSpace(email)
}

// findings describes the inconsistencies of a pool report.
func findings(r PoolReport, master *Device) []string {
	out := []string{}
	code := strconv.Itoa(r.PoolCode)

	switch {
	case master == nil:
		out = append(out, fmt.Sprintf("No TagoIO device named %s.", r.DeviceName))
	case !master.Active:
		out = append(out, fmt.Sprintf("Device %s is inactive.", master.Name))
	}
	masters := 0
	for _, d := range r.Devices {
		if d.Name == r.DeviceName {
			masters++
		}
		if d.Name != r.DeviceName {
			out = append(out, fmt.Sprintf("Device %s (%s) also belongs to the pool (by name or group_id tag).", d.Name, d.ID))
		}
		if gid := tagValueOf(d.Tags, "group_id"); d.Name == r.DeviceName && gid != code {
			out = append(out, fmt.Sprintf("Device %s has group_id tag %q, not %s.", d.Name, gid, code))
		}
	}
	if masters > 1 {
		out = append(out, fmt.Sprintf("%d devices are named %s.", masters, r.DeviceName))
	}

	switch {
	case r.Registered == nil && master != nil:
		out = append(out, "The service has not registered the device yet (it does on the first CSMS request or account rescan).")
	case r.Registered == nil:
		out = append(out, "The service has no device registered for the pool.")
	case master != nil && r.Registered.DeviceID != master.ID:
		out = append(out, fmt.Sprintf("The service has device %s registered, but TagoIO's %s is %s.", r.Registered.DeviceID, r.DeviceName, master.ID))
	}

	if master != nil {
		for _, variable := range ConfigVariables {
			if r.Configuration[variable] == nil && !hasError(r.Errors, variable) {
				out = append(out, fmt.Sprintf("The bucket has no %s.", variable))
			}
		}
		if info := r.Configuration["operator_info"]; info != nil {
			if web, _ := info.Metadata["web"].(string); strings.TrimSpace(web) == "" {
				out = append(out, "operator_info has no web.")
			}
		}
	}

	if len(r.Users) == 0 && !hasError(r.Errors, "users") {
		out = append(out, fmt.Sprintf("No run user carries group_id %s: nobody sees the pool in the dashboards.", code))
	}
	for _, u := range r.OperatorEmailUsers {
		switch {
		case len(u.GroupIDs) == 0:
			out = append(out, fmt.Sprintf("The operator email's user %s carries no group_id tag.", u.Email))
		case !slices.Contains(u.GroupIDs, code):
			out = append(out, fmt.Sprintf("The operator email's user %s carries group_id %s, not %s.", u.Email, strings.Join(u.GroupIDs, ", "), code))
		}
	}
	if len(r.Stations) == 0 {
		out = append(out, "The service knows no station of the pool.")
	}
	return out
}

func hasError(errs []string, prefix string) bool {
	for _, e := range errs {
		if strings.HasPrefix(e, prefix+":") {
			return true
		}
	}
	return false
}

func tagValueOf(tags []tagoio.Tag, key string) string {
	for _, t := range tags {
		if t.Key == key {
			return t.Value
		}
	}
	return ""
}

func nonNilTags(tags []tagoio.Tag) []tagoio.Tag {
	if tags == nil {
		return []tagoio.Tag{}
	}
	return tags
}

// endregion
