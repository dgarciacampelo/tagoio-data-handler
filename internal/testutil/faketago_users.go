// This file holds the parts of the fake TagoIO the provisioning writes: run user creation (one user per email) and
// the details and tag edits of one user. The users live with the inspection's (faketago_inspect.go).

package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// User returns a run user by ID (ok false when missing).
func (f *FakeTagoIO) User(id string) (tagoio.RunUser, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := slices.IndexFunc(f.users, func(u FakeUser) bool { return u.ID == id }); i >= 0 {
		return f.users[i].runUser(), true
	}
	return tagoio.RunUser{}, false
}

// UserByEmail returns a run user by email, ignoring case (ok false when missing).
func (f *FakeTagoIO) UserByEmail(email string) (tagoio.RunUser, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := slices.IndexFunc(f.users, func(u FakeUser) bool { return strings.EqualFold(u.Email, email) }); i >= 0 {
		return f.users[i].runUser(), true
	}
	return tagoio.RunUser{}, false
}

func (u FakeUser) runUser() tagoio.RunUser {
	return tagoio.RunUser{
		ID: u.ID, Name: u.Name, Email: u.Email, Company: u.Company, Tags: slices.Clone(u.Tags), Active: u.Active,
		CreatedAt: tagoio.LooseString(u.CreatedAt),
	}
}

// createUser answers POST /run/users; like TagoIO, it rejects an email that already has a user. The caller holds
// the lock.
func (f *FakeTagoIO) createUser(w http.ResponseWriter, r *http.Request) {
	var u tagoio.NewUser
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		writeEnvelope(w, http.StatusBadRequest, false, nil, err.Error())
		return
	}
	if slices.ContainsFunc(f.users, func(existing FakeUser) bool { return strings.EqualFold(existing.Email, u.Email) }) {
		writeEnvelope(w, http.StatusBadRequest, false, nil, "Email address already in use")
		return
	}
	f.nextID++
	f.users = append(f.users, FakeUser{
		ID: fmt.Sprintf("user-%d", f.nextID), Name: u.Name, Email: u.Email, Company: u.Company, Tags: u.Tags, Active: u.Active,
	})
	writeEnvelope(w, http.StatusOK, true, "User created", "")
}

// serveUser answers GET and PUT /run/users/{id}; an edit with tags replaces the whole tag list, as TagoIO does. The
// caller holds the lock.
func (f *FakeTagoIO) serveUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/run/users/")
	i := slices.IndexFunc(f.users, func(u FakeUser) bool { return u.ID == id })
	if i < 0 {
		writeEnvelope(w, http.StatusNotFound, false, nil, "User not found")
		return
	}
	if r.Method == http.MethodGet {
		writeEnvelope(w, http.StatusOK, true, f.users[i].runUser(), "")
		return
	}
	var edit struct {
		Tags *[]tagoio.Tag `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
		writeEnvelope(w, http.StatusBadRequest, false, nil, err.Error())
		return
	}
	if edit.Tags != nil {
		f.users[i].Tags = *edit.Tags
	}
	writeEnvelope(w, http.StatusOK, true, "User Successfully Updated", "")
}
