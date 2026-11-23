// Package panelauth decides who may use the embedded management panel of a pool, and signs the panel sessions.
//
// The panel runs inside a TagoIO dashboard (a Custom Widget), which gives it the token of the person who has the
// dashboard open. The page sends that token once; this package asks TagoIO whom it belongs to:
//   - a run user of this service's TagoIO Run: allowed on the pools named by the user's group_id tags, the same
//     rule as the access policy of the TagoIO dashboards (a device is visible to a user who carries its group_id);
//   - this service's own account (the admin console): allowed on every pool.
//
// TagoIO's answer for a run user has no tags, so they are read with the account token. The person's token is
// never stored nor logged; the page then uses a short session signed here, bound to one pool.
package panelauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

const (
	// SessionLifetime is how long a panel session lasts; the page asks for a new one with the TagoIO token when it
	// ends, so a group_id removed from a user stops working within this time.
	SessionLifetime = time.Hour
	identityTTL     = 5 * time.Minute // How long a token's identity is reused (page reloads, several panel tabs)
	maxCachedTokens = 1000
	groupTag        = "group_id"
)

var (
	// ErrInvalidToken is returned for a token TagoIO doesn't accept, or of another account.
	ErrInvalidToken = errors.New("panelauth: invalid TagoIO token")
	// ErrForbidden is returned when the user may not manage the pool.
	ErrForbidden = errors.New("panelauth: pool not allowed for this user")
)

// Tago is the part of the TagoIO client the package uses.
type Tago interface {
	RunUserID(ctx context.Context, runURL, userToken string) (string, error)
	AccountID(ctx context.Context, token string) (string, error)
	OwnAccountID(ctx context.Context) (string, error)
	User(ctx context.Context, id string) (tagoio.RunUser, error)
}

// Identity is whom a TagoIO token belongs to.
type Identity struct {
	UserID   string   // Run user ID, or the account ID for an admin
	Admin    bool     // The service's own account: every pool
	GroupIDs []string // Pools of a run user (its group_id tags)
}

// Allows reports whether the identity may manage a pool.
func (id Identity) Allows(poolCode int) bool {
	return id.Admin || slices.Contains(id.GroupIDs, strconv.Itoa(poolCode))
}

// Claims are the contents of a panel session.
type Claims struct {
	UserID   string `json:"u"`
	Admin    bool   `json:"a,omitempty"`
	PoolCode int    `json:"p"`
	Expiry   int64  `json:"e"` // Unix seconds
}

// Service authenticates the panel users.
type Service struct {
	tago   Tago
	runURL string
	secret []byte
	log    *slog.Logger
	now    func() time.Time

	mu         sync.Mutex
	identities map[[32]byte]cachedIdentity // By SHA-256 of the token: the token itself is not kept
	ownAccount string
}

type cachedIdentity struct {
	identity Identity
	until    time.Time
}

// New creates the service. secret signs the sessions; runURL is the TagoIO Run of the dashboards.
func New(tago Tago, runURL string, secret []byte, log *slog.Logger) *Service {
	return &Service{
		tago: tago, runURL: runURL, secret: secret, log: log, now: time.Now,
		identities: map[[32]byte]cachedIdentity{},
	}
}

// Identify returns whom a TagoIO token belongs to.
func (s *Service) Identify(ctx context.Context, token string) (Identity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Identity{}, ErrInvalidToken
	}
	key := sha256.Sum256([]byte(token))
	now := s.now()
	s.mu.Lock()
	if c, ok := s.identities[key]; ok && now.Before(c.until) {
		s.mu.Unlock()
		return c.identity, nil
	}
	s.mu.Unlock()

	identity, err := s.identify(ctx, token)
	if err != nil {
		return Identity{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.identities) >= maxCachedTokens {
		for k, c := range s.identities {
			if !now.Before(c.until) {
				delete(s.identities, k)
			}
		}
		if len(s.identities) >= maxCachedTokens {
			clear(s.identities)
		}
	}
	s.identities[key] = cachedIdentity{identity: identity, until: now.Add(identityTTL)}
	return identity, nil
}

// identify asks TagoIO: first as a run user of the Run, then as an account, which must be this service's own.
func (s *Service) identify(ctx context.Context, token string) (Identity, error) {
	userID, err := s.tago.RunUserID(ctx, s.runURL, token)
	if err == nil && userID != "" {
		user, err := s.tago.User(ctx, userID)
		if err != nil {
			return Identity{}, fmt.Errorf("panelauth: reading run user %s: %w", userID, err)
		}
		if !user.Active {
			return Identity{}, ErrInvalidToken
		}
		return Identity{UserID: userID, GroupIDs: user.TagValues(groupTag)}, nil
	}
	if err != nil && !isRejection(err) {
		return Identity{}, fmt.Errorf("panelauth: identifying run user: %w", err)
	}

	accountID, err := s.tago.AccountID(ctx, token)
	if err != nil {
		if isRejection(err) {
			return Identity{}, ErrInvalidToken
		}
		return Identity{}, fmt.Errorf("panelauth: identifying account: %w", err)
	}
	own, err := s.ownAccountID(ctx)
	if err != nil {
		return Identity{}, err
	}
	if accountID == "" || accountID != own {
		s.log.Warn("Panel: token of another TagoIO account rejected", "account_id", accountID)
		return Identity{}, ErrInvalidToken
	}
	return Identity{UserID: accountID, Admin: true}, nil
}

// isRejection reports whether TagoIO answered that the token is not valid for the call (rather than failing).
func isRejection(err error) bool {
	return tagoio.IsStatus(err, 400, 401, 403, 404)
}

func (s *Service) ownAccountID(ctx context.Context) (string, error) {
	s.mu.Lock()
	own := s.ownAccount
	s.mu.Unlock()
	if own != "" {
		return own, nil
	}
	own, err := s.tago.OwnAccountID(ctx)
	if err != nil || own == "" {
		return "", fmt.Errorf("panelauth: reading the service's account: %w", err)
	}
	s.mu.Lock()
	s.ownAccount = own
	s.mu.Unlock()
	return own, nil
}

// region Sessions

// Issue signs a session of an identity for a pool. The caller checked that the identity allows the pool.
func (s *Service) Issue(identity Identity, poolCode int) (string, Claims) {
	claims := Claims{UserID: identity.UserID, Admin: identity.Admin, PoolCode: poolCode,
		Expiry: s.now().Add(SessionLifetime).Unix()}
	payload, _ := json.Marshal(claims) // A struct of strings and numbers: it can't fail
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + s.sign(encoded), claims
}

// Verify checks a session for a pool: its signature, its expiry and its pool.
func (s *Service) Verify(session string, poolCode int) (Claims, bool) {
	claims, ok := s.Parse(session)
	if !ok || claims.PoolCode != poolCode {
		return Claims{}, false
	}
	return claims, true
}

// Parse checks a session's signature and expiry, and returns its claims (the caller checks the pool).
func (s *Service) Parse(session string) (Claims, bool) {
	encoded, signature, ok := strings.Cut(strings.TrimSpace(session), ".")
	if !ok || !hmac.Equal([]byte(signature), []byte(s.sign(encoded))) {
		return Claims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Claims{}, false
	}
	var claims Claims
	if json.Unmarshal(payload, &claims) != nil || !s.now().Before(time.Unix(claims.Expiry, 0)) {
		return Claims{}, false
	}
	return claims, true
}

func (s *Service) sign(encoded string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("panel-session|" + encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// endregion
