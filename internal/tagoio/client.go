// Package tagoio is a small client of the TagoIO REST API, limited to the calls this service needs: device lookup
// and creation, device tokens, data insert/read/delete in a device bucket, the bucket data amount and run users.
//
// Every TagoIO response is an envelope, {"status": true, "result": ...} or {"status": false, "message": ...},
// sometimes with an HTTP error status and sometimes with 200; the client turns every failure into an *APIError.
// In dry-run mode the writes are logged and not sent (the reads still go out), so the service can run against the
// production account without changing it.
package tagoio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	requestTimeout  = 15 * time.Second
	maxResponseSize = 32 << 20 // A full data page (10 000 records) stays well below it
	getAttempts     = 3        // GETs are retried on network errors (1 s, then 2 s apart)
)

// Client calls the TagoIO REST API.
type Client struct {
	baseURL      string
	accountToken string
	http         *http.Client
	log          *slog.Logger
	dryRun       bool
}

// NewClient creates a client for the API at baseURL (e.g. https://api.tago.io).
func NewClient(baseURL, accountToken string, dryRun bool, log *slog.Logger) *Client {
	return &Client{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		accountToken: accountToken,
		http:         &http.Client{Timeout: requestTimeout},
		log:          log,
		dryRun:       dryRun,
	}
}

// APIError is a request TagoIO answered with "status": false or an HTTP error status.
type APIError struct {
	StatusCode int    // HTTP status (200 when TagoIO reported the failure in the envelope only)
	Message    string // TagoIO's message, or the start of the body when it was not an envelope
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tagoio: HTTP %d: %s", e.StatusCode, e.Message)
}

// IsStatus reports whether err is an *APIError with one of the given HTTP statuses.
func IsStatus(err error, statuses ...int) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, status := range statuses {
		if apiErr.StatusCode == status {
			return true
		}
	}
	return false
}

// HasMessage reports whether err is an *APIError carrying the given TagoIO message.
func HasMessage(err error, message string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Message == message
}

// credential is the authentication header of a request: the account token, or a device token.
type credential struct {
	header string
	token  string
}

func (c *Client) account() credential {
	return credential{header: "Account-Token", token: c.accountToken}
}

func device(token string) credential {
	return credential{header: "Device-Token", token: token}
}

type envelope struct {
	Status  bool            `json:"status"`
	Result  json.RawMessage `json:"result"`
	Message string          `json:"message"`
}

// do sends a request and decodes the envelope's result into result (when not nil).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, cred credential, body, result any) error {
	if c.dryRun && method != http.MethodGet {
		attrs := []any{"method", method, "path", path, "query", query.Encode()}
		if path == "/data" { // Only bucket data is logged: user and device creations carry passwords and tokens
			attrs = append(attrs, "body", body)
		}
		c.log.Info("TagoIO: dry run, request not sent", attrs...)
		return nil
	}

	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("tagoio: encoding %s %s: %w", method, path, err)
		}
	}

	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	attempts := 1
	if method == http.MethodGet {
		attempts = getAttempts
	}
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		var raw []byte
		var status int
		raw, status, err = c.send(ctx, method, endpoint, cred, payload)
		if err == nil {
			return decodeEnvelope(raw, status, result)
		}
		if !isRetryable(err) || attempt == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return fmt.Errorf("tagoio: %s %s: %w", method, path, err)
}

func (c *Client) send(ctx context.Context, method, endpoint string, cred credential, payload []byte) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set(cred.header, cred.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

// decodeEnvelope maps a TagoIO response to its result or an *APIError.
func decodeEnvelope(raw []byte, status int, result any) error {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{StatusCode: status, Message: truncate(string(raw), 200)}
	}
	if !env.Status || status >= 400 {
		return &APIError{StatusCode: status, Message: env.Message}
	}
	if result == nil || len(env.Result) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(env.Result))
	decoder.UseNumber() // Keep numbers as written (e.g. "10.0"), as the Python service saw them
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("tagoio: decoding result: %w", err)
	}
	return nil
}

// isRetryable reports whether a request failed on the network (not with an answer from TagoIO).
func isRetryable(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
