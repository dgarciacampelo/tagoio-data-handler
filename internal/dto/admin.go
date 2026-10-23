// This file holds the contract of the login page.

package dto

// LoginRequest is the body of POST /api/login.
type LoginRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
	Redirect string `json:"redirect"` // Page to return to (checked by the server)
}

// LoginResult is the answer of a successful login.
type LoginResult struct {
	Redirect string `json:"redirect"`
}

// LoginView is the view model of the login page.
type LoginView struct {
	Redirect string `json:"redirect"`
}
