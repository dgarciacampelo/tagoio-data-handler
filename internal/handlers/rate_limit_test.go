package handlers

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
)

func TestClientAddress(t *testing.T) {
	tests := map[string]struct {
		forwarded []string
		remote    string
		want      string
	}{
		"no proxy":                      {nil, "203.0.113.7:51234", "203.0.113.7"},
		"proxy appends the client":      {[]string{"203.0.113.7"}, "127.0.0.1:40000", "203.0.113.7"},
		"spoofed hops before the proxy": {[]string{"1.1.1.1, 2.2.2.2, 203.0.113.7"}, "127.0.0.1:40000", "203.0.113.7"},
		"two headers: the last one":     {[]string{"1.1.1.1", "203.0.113.7"}, "127.0.0.1:40000", "203.0.113.7"},
		"empty last hop":                {[]string{"203.0.113.7, "}, "127.0.0.1:40000", "127.0.0.1"},
		"IPv6 peer":                     {nil, "[2001:db8::1]:443", "2001:db8::1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/station/261001/ST1", nil)
			r.RemoteAddr = tt.remote
			for _, value := range tt.forwarded {
				r.Header.Add("X-Forwarded-For", value)
			}
			if got := clientAddress(r); got != tt.want {
				t.Errorf("clientAddress = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestWindowLimiter(t *testing.T) {
	l := newWindowLimiter(3, time.Minute)
	start := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	for i := range 3 {
		if _, ok := l.allow("a", start.Add(time.Duration(i)*time.Second)); !ok {
			t.Fatalf("call %d refused", i+1)
		}
	}
	retryAfter, ok := l.allow("a", start.Add(10*time.Second))
	if ok || retryAfter != 50*time.Second {
		t.Fatalf("4th call: allowed %v, retry after %v; want refused, 50s", ok, retryAfter)
	}
	if _, ok := l.allow("b", start.Add(10*time.Second)); !ok {
		t.Error("another client was refused")
	}
	if _, ok := l.allow("a", start.Add(time.Minute)); !ok {
		t.Error("refused in the next window")
	}

	var nilLimiter *windowLimiter
	if _, ok := nilLimiter.allow("a", start); !ok {
		t.Error("a nil limiter refused a call")
	}
}

func TestWindowLimiterStaysBoundedWithManyClients(t *testing.T) {
	// Entries are only pruned once their window ended, so clients that keep changing address within a window
	// (spoofed X-Forwarded-For hops when the proxy passes them on, an IPv6 /64) grow the map without limit, and
	// each call then scans it under the limiter's single lock.
	l := newWindowLimiter(10, time.Minute)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for i := range 12_000 { // Past 10 000 entries every call also scans the whole map: keep the test short
		l.allow("2001:db8::"+strconv.FormatInt(int64(i), 16), now)
	}
	testutil.KnownBug(t, len(l.windows) <= 10_000,
		"the rate limiter keeps one entry per client address for the whole window, without an upper bound ("+
			strconv.Itoa(len(l.windows))+" entries)")
}
