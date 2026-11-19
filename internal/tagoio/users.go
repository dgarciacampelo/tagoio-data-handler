// This file holds the account-level calls on run (dashboard) users beyond their creation: lookup by email, user
// details and tag edits.

package tagoio

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// RunUser is a run (dashboard) user of the account.
type RunUser struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Active bool   `json:"active"`
	Tags   []Tag  `json:"tags"`
}

// HasTag reports whether the user has a tag with exactly that key and value.
func (u RunUser) HasTag(key, value string) bool {
	for _, tag := range u.Tags {
		if tag.Key == key && tag.Value == value {
			return true
		}
	}
	return false
}

// runUserFields are the fields asked for when listing run users (TagoIO answers only id and name by default).
var runUserFields = []string{"id", "name", "email", "active", "tags"}

// FindUsersByEmail returns the run users whose email is email, ignoring case (TagoIO keeps one user per email, so
// there is at most one).
func (c *Client) FindUsersByEmail(ctx context.Context, email string) ([]RunUser, error) {
	email = strings.TrimSpace(email)
	query := url.Values{
		"page":          {"1"},
		"amount":        {"100"},
		"filter[email]": {strings.ToLower(email)},
	}
	for i, field := range runUserFields {
		query.Set("fields["+strconv.Itoa(i)+"]", field)
	}
	var users []RunUser
	if err := c.do(ctx, http.MethodGet, "/run/users", query, c.account(), nil, &users); err != nil {
		return nil, err
	}
	matched := users[:0]
	for _, u := range users { // The filter may match loosely: keep the exact email only
		if strings.EqualFold(strings.TrimSpace(u.Email), email) {
			matched = append(matched, u)
		}
	}
	return matched, nil
}

// User returns a run user with all its tags.
func (c *Client) User(ctx context.Context, id string) (RunUser, error) {
	var u RunUser
	err := c.do(ctx, http.MethodGet, "/run/users/"+url.PathEscape(id), nil, c.account(), nil, &u)
	return u, err
}

// SetUserTags replaces the tags of a run user: tags must hold every tag the user keeps.
func (c *Client) SetUserTags(ctx context.Context, id string, tags []Tag) error {
	body := struct {
		Tags []Tag `json:"tags"`
	}{tags}
	return c.do(ctx, http.MethodPut, "/run/users/"+url.PathEscape(id), nil, c.account(), body, nil)
}
