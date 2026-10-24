// Package dto holds the JSON contract between the Go server and the Svelte pages: the page view models and the
// JSON API bodies. It is the source of truth of the frontend types: `go generate ./internal/dto` writes them to
// frontend/src/lib/contract.gen.ts (tygo). Field names are snake_case.
package dto

//go:generate go tool tygo generate

// ErrorCode is a stable error code; the pages translate it, they never show Message.
type ErrorCode string

const (
	ErrorCodeInvalidRequest     ErrorCode = "invalid_request"
	ErrorCodeInvalidCredentials ErrorCode = "invalid_credentials"
	ErrorCodeUnauthenticated    ErrorCode = "unauthenticated"
	ErrorCodeNotFound           ErrorCode = "not_found"
	ErrorCodeConflict           ErrorCode = "conflict"
	ErrorCodeRateLimited        ErrorCode = "rate_limited"
	ErrorCodeServiceUnavailable ErrorCode = "service_unavailable"
	ErrorCodeInternalError      ErrorCode = "internal_error"
)

// ErrorCodes lists every error code (a test checks each one has a translation).
//
//tygo:ignore
var ErrorCodes = []ErrorCode{
	ErrorCodeInvalidRequest, ErrorCodeInvalidCredentials, ErrorCodeUnauthenticated, ErrorCodeNotFound,
	ErrorCodeConflict, ErrorCodeRateLimited, ErrorCodeServiceUnavailable, ErrorCodeInternalError,
}

// APIError is the body of every JSON error answer.
type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"` // For developers (logs, curl); never shown to the user
}

// PageBootstrap is the JSON embedded in every page shell (frontend/src/lib/bootstrap.ts).
type PageBootstrap struct {
	Page any `json:"page"` // The page's view model, or null when it could not be built
}
