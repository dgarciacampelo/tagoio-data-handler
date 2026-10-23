// This file holds the account-level calls (Account-Token): device lookup and creation, device tokens, the bucket
// data amount and run users.

package tagoio

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Tag is a TagoIO key/value tag of a device or user.
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Device is a device of the account.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tags []Tag  `json:"tags"`
}

// TagValue returns the value of a tag ("" when missing).
func (d Device) TagValue(key string) string {
	for _, tag := range d.Tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

const devicePageSize = 1000

// ListDevices returns the account devices, following the pages; nameFilter, when set, asks TagoIO for the devices
// with that exact name only.
func (c *Client) ListDevices(ctx context.Context, nameFilter string) ([]Device, error) {
	var all []Device
	for page := 1; ; page++ {
		query := url.Values{
			"page":    {strconv.Itoa(page)},
			"amount":  {strconv.Itoa(devicePageSize)},
			"orderBy": {"name,asc"},
		}
		if nameFilter != "" {
			query.Set("filter[name]", nameFilter)
		}
		var devices []Device
		if err := c.do(ctx, http.MethodGet, "/device", query, c.account(), nil, &devices); err != nil {
			return nil, err
		}
		all = append(all, devices...)
		if len(devices) < devicePageSize {
			return all, nil
		}
	}
}

// DeviceToken returns the newest token of a device ("" when it has none).
func (c *Client) DeviceToken(ctx context.Context, deviceID string) (string, error) {
	query := url.Values{"amount": {"1"}, "orderBy": {"created_at,desc"}}
	var tokens []struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodGet, "/device/token/"+url.PathEscape(deviceID), query, c.account(), nil, &tokens); err != nil {
		return "", err
	}
	if len(tokens) == 0 {
		return "", nil
	}
	return tokens[0].Token, nil
}

// NewDevice describes a device to create.
type NewDevice struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
	Visible     bool   `json:"visible"`
	Type        string `json:"type"` // "mutable": records can be edited and deleted
	Tags        []Tag  `json:"tags"`
}

// CreateDevice creates a device and returns its ID and token.
func (c *Client) CreateDevice(ctx context.Context, d NewDevice) (id, token string, err error) {
	var created struct {
		DeviceID string `json:"device_id"`
		Token    string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/device", nil, c.account(), d, &created); err != nil {
		return "", "", err
	}
	if c.dryRun {
		return "dry-run-device", "dry-run-token", nil
	}
	if created.DeviceID == "" || created.Token == "" {
		return "", "", fmt.Errorf("tagoio: device %q created without ID or token", d.Name)
	}
	return created.DeviceID, created.Token, nil
}

// DataAmount returns the number of records in a device bucket.
func (c *Client) DataAmount(ctx context.Context, deviceID string) (int, error) {
	var amount any
	if err := c.do(ctx, http.MethodGet, "/device/"+url.PathEscape(deviceID)+"/data_amount", nil, c.account(), nil, &amount); err != nil {
		return 0, err
	}
	return ValueInt(amount)
}

// NewUser describes a run (dashboard) user to create.
type NewUser struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Language string `json:"language"`
	Timezone string `json:"timezone"`
	Phone    string `json:"phone"`
	Company  string `json:"company"`
	Active   bool   `json:"active"`
	Tags     []Tag  `json:"tags"`
}

// CreateUser creates a run user.
func (c *Client) CreateUser(ctx context.Context, u NewUser) error {
	return c.do(ctx, http.MethodPost, "/run/users", nil, c.account(), u, nil)
}
