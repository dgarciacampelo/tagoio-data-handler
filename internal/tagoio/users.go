// This file holds the account-level calls the provisioning makes on existing run (dashboard) users: lookup by email,
// user details and tag edits. The user type and the listing are the inspection's (inspect.go).

package tagoio

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// HasTag reports whether the user has a tag with exactly that key and value.
func (u RunUser) HasTag(key, value string) bool {
	for _, tag := range u.Tags {
		if tag.Key == key && tag.Value == value {
			return true
		}
	}
	return false
}

// FindUsersByEmail returns the run users whose email is email, ignoring case (TagoIO keeps one user per email, so
// there is at most one). The email is looked up in lower case, and only exact matches are kept.
func (c *Client) FindUsersByEmail(ctx context.Context, email string) ([]RunUser, error) {
	email = strings.TrimSpace(email)
	users, err := c.ListRunUsers(ctx, UserFilter{Email: strings.ToLower(email)})
	if err != nil {
		return nil, err
	}
	matched := users[:0]
	for _, u := range users {
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
