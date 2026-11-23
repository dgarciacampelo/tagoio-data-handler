package panelauth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

const runURL = "neos.tago.run"

// fakeTago knows run user tokens, account tokens and the users' tags.
type fakeTago struct {
	runTokens     map[string]string // token -> run user ID
	accountTokens map[string]string // token -> account ID
	users         map[string]tagoio.RunUser
	down          bool // Every call fails as if TagoIO were unreachable
	calls         int
}

var errDown = errors.New("connection refused")

func (f *fakeTago) RunUserID(_ context.Context, url, token string) (string, error) {
	f.calls++
	if f.down {
		return "", errDown
	}
	if id, ok := f.runTokens[token]; ok && url == runURL {
		return id, nil
	}
	return "", &tagoio.APIError{StatusCode: 401, Message: "Authorization denied"}
}

func (f *fakeTago) AccountID(_ context.Context, token string) (string, error) {
	f.calls++
	if id, ok := f.accountTokens[token]; ok {
		return id, nil
	}
	return "", &tagoio.APIError{StatusCode: 400, Message: "Invalid token"}
}

func (f *fakeTago) OwnAccountID(context.Context) (string, error) { return "own-account", nil }

func (f *fakeTago) User(_ context.Context, id string) (tagoio.RunUser, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return tagoio.RunUser{}, &tagoio.APIError{StatusCode: 404, Message: "not found"}
}

func groups(ids ...string) []tagoio.Tag {
	tags := []tagoio.Tag{{Key: "plan", Value: "ADVANCED"}}
	for _, id := range ids {
		tags = append(tags, tagoio.Tag{Key: "group_id", Value: id})
	}
	return tags
}

func newFake() *fakeTago {
	return &fakeTago{
		runTokens:     map[string]string{"cpo-token": "u1", "two-pools": "u2", "no-pool": "u3", "inactive": "u4"},
		accountTokens: map[string]string{"admin-token": "own-account", "other-account": "someone-else"},
		users: map[string]tagoio.RunUser{
			"u1": {ID: "u1", Active: true, Tags: groups("220001")},
			"u2": {ID: "u2", Active: true, Tags: groups("221006", "231003")},
			"u3": {ID: "u3", Active: true, Tags: groups()},
			"u4": {ID: "u4", Active: false, Tags: groups("220001")},
		},
	}
}

func newService(f *fakeTago) *Service {
	return New(f, runURL, []byte("0123456789abcdef0123456789abcdef"), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestIdentifyAppliesTheGroupIDRule(t *testing.T) {
	tests := map[string]struct {
		token   string
		allowed []int
		denied  []int
		admin   bool
	}{
		"CPO of one pool":    {token: "cpo-token", allowed: []int{220001}, denied: []int{221006}},
		"CPO of two pools":   {token: "two-pools", allowed: []int{221006, 231003}, denied: []int{220001}},
		"user without pools": {token: "no-pool", denied: []int{220001, 221006}},
		"the account itself": {token: "admin-token", allowed: []int{220001, 999999}, admin: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			identity, err := newService(newFake()).Identify(context.Background(), tt.token)
			if err != nil {
				t.Fatal(err)
			}
			if identity.Admin != tt.admin {
				t.Errorf("admin = %v; want %v", identity.Admin, tt.admin)
			}
			for _, pool := range tt.allowed {
				if !identity.Allows(pool) {
					t.Errorf("pool %d denied; want allowed", pool)
				}
			}
			for _, pool := range tt.denied {
				if identity.Allows(pool) {
					t.Errorf("pool %d allowed; want denied", pool)
				}
			}
		})
	}
}

func TestIdentifyRejects(t *testing.T) {
	for name, token := range map[string]string{
		"unknown token":          "made-up",
		"empty token":            " ",
		"another TagoIO account": "other-account",
		"inactive run user":      "inactive",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newService(newFake()).Identify(context.Background(), token)
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("err = %v; want ErrInvalidToken", err)
			}
		})
	}
}

func TestIdentifyReportsAnUnreachableTagoIOAsAFailure(t *testing.T) {
	f := newFake()
	f.down = true
	_, err := newService(f).Identify(context.Background(), "cpo-token")
	if err == nil || errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v; want a failure, not an invalid token", err)
	}
}

func TestIdentifyReusesTheIdentityForAWhile(t *testing.T) {
	f := newFake()
	s := newService(f)
	now := time.Now()
	s.now = func() time.Time { return now }
	for range 3 {
		if _, err := s.Identify(context.Background(), "cpo-token"); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls != 1 {
		t.Errorf("TagoIO calls = %d; want 1", f.calls)
	}
	now = now.Add(identityTTL + time.Second)
	_, _ = s.Identify(context.Background(), "cpo-token")
	if f.calls != 2 {
		t.Errorf("TagoIO calls after the TTL = %d; want 2", f.calls)
	}
}

func TestSessions(t *testing.T) {
	s := newService(newFake())
	now := time.Now()
	s.now = func() time.Time { return now }
	session, claims := s.Issue(Identity{UserID: "u1", GroupIDs: []string{"220001"}}, 220001)
	if claims.PoolCode != 220001 || claims.UserID != "u1" {
		t.Fatalf("claims = %+v", claims)
	}

	if got, ok := s.Verify(session, 220001); !ok || got != claims {
		t.Errorf("Verify = %+v, %v; want the claims", got, ok)
	}
	if _, ok := s.Verify(session, 221006); ok {
		t.Error("a session of pool 220001 verified for pool 221006")
	}
	_, signature, _ := strings.Cut(session, ".")
	otherPool, _ := s.Issue(Identity{UserID: "u1"}, 221006)
	otherEncoded, _, _ := strings.Cut(otherPool, ".")
	if _, ok := s.Verify(otherEncoded+"."+signature, 221006); ok {
		t.Error("claims of another pool verified with this session's signature")
	}
	other := New(newFake(), runURL, []byte("another-secret-another-secret-00"), s.log)
	if _, ok := other.Verify(session, 220001); ok {
		t.Error("a session signed with another secret verified")
	}
	now = now.Add(SessionLifetime + time.Second)
	if _, ok := s.Verify(session, 220001); ok {
		t.Error("an expired session verified")
	}
}
