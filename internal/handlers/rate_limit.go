// This file holds the in-memory rate limits of the public endpoints (admin login, payment requests and the
// station page polling), per client address.

package handlers

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

// allowCall counts a call of the client in limiter, answering 429 (with Retry-After) when its calls are spent.
func (a *App) allowCall(w http.ResponseWriter, r *http.Request, limiter *windowLimiter) bool {
	retryAfter, ok := limiter.allow(clientAddress(r), time.Now())
	if ok {
		return true
	}
	a.Log.Warn("Calls rate limited", "path", r.URL.Path)
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
	writeAPIError(w, http.StatusTooManyRequests, dto.ErrorCodeRateLimited, "Too many requests. Try again later.")
	return false
}

// clientAddress is the address of the client: the last X-Forwarded-For hop when the reverse proxy set it (a
// client can only add hops before it), else the peer.
func clientAddress(r *http.Request) string {
	if forwarded := r.Header.Values("X-Forwarded-For"); len(forwarded) > 0 {
		hops := strings.Split(forwarded[len(forwarded)-1], ",")
		if last := strings.TrimSpace(hops[len(hops)-1]); last != "" {
			return last
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// windowLimiter allows `limit` calls per key in each fixed window. A nil limiter allows everything.
type windowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	windows map[string]*limitWindow
}

type limitWindow struct {
	start time.Time
	count int
}

func newWindowLimiter(limit int, window time.Duration) *windowLimiter {
	return &windowLimiter{limit: limit, window: window, windows: map[string]*limitWindow{}}
}

// allow counts a call of key at now; when the window's calls are spent, it says how long until the next window.
func (l *windowLimiter) allow(key string, now time.Time) (time.Duration, bool) {
	if l == nil {
		return 0, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.windows) >= 10_000 { // Forget ended windows before the map grows further
		for k, w := range l.windows {
			if now.Sub(w.start) >= l.window {
				delete(l.windows, k)
			}
		}
	}
	w := l.windows[key]
	if w == nil || now.Sub(w.start) >= l.window {
		l.windows[key] = &limitWindow{start: now, count: 1}
		return 0, true
	}
	if w.count >= l.limit {
		return w.start.Add(l.window).Sub(now), false
	}
	w.count++
	return 0, true
}
