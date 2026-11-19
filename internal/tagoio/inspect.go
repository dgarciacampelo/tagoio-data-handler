// This file holds the read-only account calls of the TagoIO inspection (package inspect): the devices with their
// descriptive fields, and the run users. Neither asks TagoIO for tokens or passwords.

package tagoio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// LooseString decodes a JSON string, number or null (as ""), for the descriptive fields whose type the TagoIO
// documentation doesn't pin down (dates may come as text or as numbers, and as null when never set).
type LooseString string

// UnmarshalJSON implements json.Unmarshaler.
func (s *LooseString) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var v any
	if err := decoder.Decode(&v); err != nil {
		return err
	}
	switch v := v.(type) {
	case nil:
		*s = ""
	case string:
		*s = LooseString(v)
	case json.Number:
		*s = LooseString(v.String())
	default:
		*s = LooseString(fmt.Sprint(v))
	}
	return nil
}

// DeviceDetails is a device of the account with the fields the inspection shows.
type DeviceDetails struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Tags      []Tag       `json:"tags"`
	Active    bool        `json:"active"`
	Type      LooseString `json:"type"`
	CreatedAt LooseString `json:"created_at"`
	UpdatedAt LooseString `json:"updated_at"`
	LastInput LooseString `json:"last_input"`
}

// TagValue returns the value of a tag ("" when missing).
func (d DeviceDetails) TagValue(key string) string {
	return tagValue(d.Tags, key)
}

var deviceDetailFields = []string{"id", "name", "tags", "active", "type", "created_at", "updated_at", "last_input"}

// ListDeviceDetails returns the account devices with their descriptive fields, following the pages; nameFilter,
// when set, asks TagoIO for the devices with that name only.
func (c *Client) ListDeviceDetails(ctx context.Context, nameFilter string) ([]DeviceDetails, error) {
	var all []DeviceDetails
	for page := 1; ; page++ {
		query := url.Values{
			"page":    {strconv.Itoa(page)},
			"amount":  {strconv.Itoa(devicePageSize)},
			"orderBy": {"name,asc"},
			"fields":  deviceDetailFields,
		}
		if nameFilter != "" {
			query.Set("filter[name]", nameFilter)
		}
		var devices []DeviceDetails
		if err := c.do(ctx, http.MethodGet, "/device", query, c.account(), nil, &devices); err != nil {
			return nil, err
		}
		all = append(all, devices...)
		if len(devices) < devicePageSize {
			return all, nil
		}
	}
}

// RunUser is a run (dashboard) user of the account, without its password or phone.
type RunUser struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Email     string      `json:"email"`
	Company   string      `json:"company"`
	Tags      []Tag       `json:"tags"`
	Active    bool        `json:"active"`
	CreatedAt LooseString `json:"created_at"`
	LastLogin LooseString `json:"last_login"`
}

// TagValues returns every value of a tag key: a user may carry the same key more than once (e.g. group_id).
func (u RunUser) TagValues(key string) []string {
	var values []string
	for _, tag := range u.Tags {
		if tag.Key == key {
			values = append(values, tag.Value)
		}
	}
	return values
}

// UserFilter selects run users: by email, and by tags (every tag must match). An empty filter selects them all.
type UserFilter struct {
	Email string
	Tags  []Tag
}

const (
	userPageSize = 100
	maxUserPages = 50 // 5 000 users: far above the account's, it only bounds a TagoIO that ignores the paging
)

var runUserFields = []string{"id", "name", "email", "company", "tags", "active", "created_at", "last_login"}

// ListRunUsers returns the run users that match the filter, following the pages until one brings no new user
// (TagoIO may cap the page size below the one asked for).
func (c *Client) ListRunUsers(ctx context.Context, f UserFilter) ([]RunUser, error) {
	var all []RunUser
	seen := map[string]bool{}
	for page := 1; page <= maxUserPages; page++ {
		query := url.Values{
			"page":    {strconv.Itoa(page)},
			"amount":  {strconv.Itoa(userPageSize)},
			"orderBy": {"name,asc"},
			"fields":  runUserFields,
		}
		if f.Email != "" {
			query.Set("filter[email]", f.Email)
		}
		for i, tag := range f.Tags {
			query.Set(fmt.Sprintf("filter[tags][%d][key]", i), tag.Key)
			query.Set(fmt.Sprintf("filter[tags][%d][value]", i), tag.Value)
		}
		var users []RunUser
		if err := c.do(ctx, http.MethodGet, "/run/users", query, c.account(), nil, &users); err != nil {
			return nil, err
		}
		added := 0
		for _, u := range users {
			if !seen[u.ID] {
				seen[u.ID] = true
				all = append(all, u)
				added++
			}
		}
		if added == 0 {
			return all, nil
		}
	}
	return all, nil
}

func tagValue(tags []Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}
