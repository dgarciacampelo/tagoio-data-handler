// This file holds the calls made with a token a person holds, not the service's: the token TagoIO hands to the
// Custom Widgets of its dashboards, for the user who has the dashboard open. They only identify who that user is.
//
// In TagoIO Run the token is the run user's own; in the admin console it is the account's (a full credential, so
// it is sent to TagoIO and nowhere else, and never logged).

package tagoio

import (
	"context"
	"net/http"
	"net/url"
)

// bearer authenticates a request with a person's token, as TagoIO's own pages send it.
func bearer(token string) credential {
	return credential{header: "Authorization", token: token}
}

// RunUserID returns the ID of the run user a token belongs to, in the Run of runURL (e.g. "neos.tago.run"). TagoIO
// answers without the user's tags: read them with User, with the account token.
func (c *Client) RunUserID(ctx context.Context, runURL, userToken string) (string, error) {
	var info struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, "/run/"+url.PathEscape(runURL)+"/info", nil, bearer(userToken), nil, &info)
	return info.ID, err
}

// AccountID returns the ID of the account a token belongs to.
func (c *Client) AccountID(ctx context.Context, token string) (string, error) {
	var info struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, "/account", nil, bearer(token), nil, &info)
	return info.ID, err
}

// OwnAccountID returns the ID of the service's own account.
func (c *Client) OwnAccountID(ctx context.Context) (string, error) {
	var info struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, "/account", nil, c.account(), nil, &info)
	return info.ID, err
}
