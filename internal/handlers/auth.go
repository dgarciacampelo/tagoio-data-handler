// This file holds the authentication: HTTP Basic for the machine endpoints (the CSMS and the notification
// service), and a signed session cookie for the admin pages, set by the login page.
//
// The admin session cookie is "<expiry unix seconds>.<HMAC-SHA256 of the expiry, the user and a purpose label>",
// keyed with DASHBOARD_SECRET_TOKEN; there is no server-side session to store. Changing the admin user or the
// secret ends every session.

package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

const (
	sessionCookie   = "admin_session"
	sessionLifetime = 7 * 24 * time.Hour
	loginPath       = "/login"
	adminLandingURL = "/emsp-dashboard"
)

// region Machine endpoints (HTTP Basic)

// RequireService protects a machine endpoint with the service credentials (APP_DEFAULT_USER/TOKEN).
func (a *App) RequireService(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, secret, ok := r.BasicAuth()
		if !ok || !credentialsMatch(a.Config.ServiceAPI, user, secret) {
			w.Header().Set("WWW-Authenticate", `Basic realm="tagoio-handler", charset="UTF-8"`)
			writeAPIError(w, http.StatusUnauthorized, dto.ErrorCodeUnauthenticated, "Not authenticated")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// credentialsMatch compares credentials in constant time (both parts are always compared).
func credentialsMatch(want config.Credentials, user, secret string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(want.User))
	secretOK := subtle.ConstantTimeCompare([]byte(secret), []byte(want.Secret))
	return userOK&secretOK == 1
}

// endregion

// region Admin session

// sessionSignature signs an expiry for the admin user.
func (a *App) sessionSignature(expiry string) string {
	mac := hmac.New(sha256.New, a.Config.CookieSecret)
	mac.Write([]byte("admin-session|" + a.Config.Admin.User + "|" + expiry))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// isAdmin reports whether the request carries a valid, unexpired admin session.
func (a *App) isAdmin(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	expiry, signature, ok := strings.Cut(cookie.Value, ".")
	if !ok || !hmac.Equal([]byte(signature), []byte(a.sessionSignature(expiry))) {
		return false
	}
	seconds, err := strconv.ParseInt(expiry, 10, 64)
	return err == nil && time.Now().Before(time.Unix(seconds, 0))
}

func (a *App) setSession(w http.ResponseWriter, r *http.Request) {
	expiry := strconv.FormatInt(time.Now().Add(sessionLifetime).Unix(), 10)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: expiry + "." + a.sessionSignature(expiry), Path: "/",
		MaxAge: int(sessionLifetime.Seconds()), HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

// isHTTPS reports whether the client reached the service over HTTPS (directly or through the reverse proxy).
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// RequireAdmin protects an admin page: without a session it redirects to the login page, which comes back.
func (a *App) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			http.Redirect(w, r, loginPath+"?redirect="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminAPI protects an admin JSON API: without a session it answers 401 (the page sends the user to the
// login page).
func (a *App) RequireAdminAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			writeAPIError(w, http.StatusUnauthorized, dto.ErrorCodeUnauthenticated, "Admin session required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// endregion

// region Login API

// LoginAPI checks the admin credentials and starts a session (POST /api/login).
func (a *App) LoginAPI(w http.ResponseWriter, r *http.Request) {
	if !a.allowCall(w, r, a.loginLimit) {
		return
	}
	var body dto.LoginRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, "Invalid login body")
		return
	}
	if !credentialsMatch(a.Config.Admin, strings.TrimSpace(body.User), body.Password) {
		a.Log.Warn("Admin login failed", "client", clientAddress(r))
		writeAPIError(w, http.StatusUnauthorized, dto.ErrorCodeInvalidCredentials, "Invalid credentials")
		return
	}
	a.setSession(w, r)
	a.Log.Info("Admin logged in", "client", clientAddress(r))
	writeJSON(w, http.StatusOK, dto.LoginResult{Redirect: SafeRedirectPath(body.Redirect)})
}

// LogoutAPI ends the admin session (POST /api/logout).
func (a *App) LogoutAPI(w http.ResponseWriter, r *http.Request) {
	clearSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// SafeRedirectPath returns target when it is a local path of this site, else the admin landing page: it rejects
// absolute URLs, scheme-relative and backslash paths, control characters and the login page itself.
func SafeRedirectPath(target string) string {
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") ||
		strings.ContainsAny(target, "\\\r\n\t") {
		return adminLandingURL
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Path == loginPath {
		return adminLandingURL
	}
	return target
}

// endregion
