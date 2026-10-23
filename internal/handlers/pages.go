// This file holds the page plumbing: the Svelte page shell with its view model, and the fallback handler that
// serves the public station pages (/{pool_code}/{station_name}).

package handlers

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

// page renders a Svelte page with its view model.
func (a *App) page(w http.ResponseWriter, entry, title string, view any) {
	a.UI.Render(w, webui.Page{Entry: entry, Title: title, Bootstrap: dto.PageBootstrap{Page: view}})
}

// stationPath matches the public page of a station: /<pool code>/<station name>. The station page lives in the
// fallback handler because a /{pool}/{station} pattern would overlap /assets/ in the router.
var stationPath = regexp.MustCompile(`^/([0-9]{1,9})/([^/]+)$`)

// Fallback serves the station pages and answers 404 for every other unknown path. The site root goes to the
// admin dashboard.
func (a *App) Fallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/" {
		http.Redirect(w, r, adminLandingURL, http.StatusFound)
		return
	}
	match := stationPath.FindStringSubmatch(r.URL.Path)
	if match == nil {
		http.NotFound(w, r)
		return
	}
	poolCode, _ := strconv.Atoi(match[1])
	a.StationPage(w, r, poolCode, match[2])
}

// LoginPage renders the admin login page (GET /login); an admin already logged in goes on.
func (a *App) LoginPage(w http.ResponseWriter, r *http.Request) {
	redirect := SafeRedirectPath(r.URL.Query().Get("redirect"))
	if a.isAdmin(r) {
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	a.page(w, "login", "Velo Energy - Acceso", dto.LoginView{Redirect: redirect})
}
