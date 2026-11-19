// This file holds the parts of the fake TagoIO the inspection reads: the run users, the descriptive fields of the
// devices and the bucket data amount.

package testutil

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// FakeUser is a run user of the fake account.
type FakeUser struct {
	ID        string
	Name      string
	Email     string
	Company   string
	Tags      []tagoio.Tag
	Active    bool
	CreatedAt string
}

// DeviceDetails are the descriptive fields of a fake device; a device without them is active, with no dates.
type DeviceDetails struct {
	Active    bool
	CreatedAt string
	LastInput string
}

type accountExtras struct {
	users   []FakeUser
	details map[string]DeviceDetails // Device ID -> details
}

// AddUser adds a run user to the account.
func (f *FakeTagoIO) AddUser(u FakeUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = append(f.users, u)
}

// SetDeviceDetails sets the descriptive fields of a device.
func (f *FakeTagoIO) SetDeviceDetails(id string, d DeviceDetails) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.details == nil {
		f.details = map[string]DeviceDetails{}
	}
	f.details[id] = d
}

// deviceJSON is a device as GET /device answers it; the caller holds the lock.
func (f *FakeTagoIO) deviceJSON(d tagoio.Device) map[string]any {
	details, ok := f.details[d.ID]
	if !ok {
		details = DeviceDetails{Active: true}
	}
	out := map[string]any{"id": d.ID, "name": d.Name, "tags": d.Tags, "active": details.Active, "type": "mutable"}
	if details.CreatedAt != "" {
		out["created_at"] = details.CreatedAt
	}
	out["last_input"] = nil
	if details.LastInput != "" {
		out["last_input"] = details.LastInput
	}
	return out
}

// serveDataAmount answers GET /device/{id}/data_amount with the bucket's record count; the caller holds the lock.
func (f *FakeTagoIO) serveDataAmount(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/device/"), "/data_amount")
	token, ok := f.tokens[id]
	if !ok {
		writeEnvelope(w, http.StatusNotFound, false, nil, "Device not found")
		return
	}
	writeEnvelope(w, http.StatusOK, true, len(f.data[token]), "")
}

// serveUsers answers GET /run/users with filter[email] (exact) and filter[tags][i][key|value] (all must match), one
// page only; the caller holds the lock.
func (f *FakeTagoIO) serveUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	var wanted []tagoio.Tag
	for i := 0; query.Has("filter[tags][" + strconv.Itoa(i) + "][key]"); i++ {
		prefix := "filter[tags][" + strconv.Itoa(i) + "]"
		wanted = append(wanted, tagoio.Tag{Key: query.Get(prefix + "[key]"), Value: query.Get(prefix + "[value]")})
	}

	users := []map[string]any{}
	if page <= 1 {
		for _, u := range f.users {
			if email := query.Get("filter[email]"); email != "" && u.Email != email {
				continue
			}
			if !containsTags(u.Tags, wanted) {
				continue
			}
			users = append(users, map[string]any{
				"id": u.ID, "name": u.Name, "email": u.Email, "company": u.Company, "tags": u.Tags,
				"active": u.Active, "created_at": u.CreatedAt, "last_login": nil,
			})
		}
	}
	writeEnvelope(w, http.StatusOK, true, users, "")
}

func containsTags(tags, wanted []tagoio.Tag) bool {
	for _, want := range wanted {
		if !slices.Contains(tags, want) {
			return false
		}
	}
	return true
}
