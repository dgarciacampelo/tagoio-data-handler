// This file holds the routes of the service.

package main

import (
	"net/http"
)

func (app *application) routes() http.Handler {
	h := app.handlers
	service := func(f http.HandlerFunc) http.Handler { return h.RequireService(f) }
	adminAPI := func(f http.HandlerFunc) http.Handler { return h.RequireAdminAPI(f) }
	adminPage := func(f http.HandlerFunc) http.Handler { return h.RequireAdmin(f) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("GET /assets/", app.ui.AssetsHandler())

	// region Machine API (HTTP Basic): the CSMS (neos-ocpp) and the notification service
	mux.Handle("GET /api/stream", service(h.Stream))
	mux.Handle("POST /v1/charge-point-update/{pool_code}/{station_name}/{connector_id}", service(h.ChargePointUpdate))
	mux.Handle("POST /v1/charging-session-update/{pool_code}/{station_name}/{connector_id}", service(h.ChargingSessionUpdate))
	mux.Handle("GET /api/pools/{pool_code}", service(h.PoolSetup))
	mux.Handle("POST /api/provision-cpo", service(h.ProvisionCPO))
	mux.Handle("POST /v1/trigger-task/pool-variable-cleanup", service(h.CleanupAllPools))
	mux.Handle("POST /v1/trigger-task/backup-to-telegram", service(h.BackupNow))
	mux.Handle("DELETE /v1/trigger-task/single-variable/{pool_code}/{variable}", service(h.DeletePoolVariable))
	// endregion

	// region Public station page (drivers): the page itself is served by the fallback, /{pool_code}/{station_name}
	mux.HandleFunc("GET /api/station/{pool_code}/{station_name}", h.StationLiveAPI)
	mux.HandleFunc("POST /api/charge-request", h.ChargeRequest)
	mux.HandleFunc("POST /api/stop-request", h.StopRequest)
	// endregion

	// region Admin pages and APIs
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /api/login", h.LoginAPI)
	mux.HandleFunc("POST /api/logout", h.LogoutAPI)
	mux.Handle("GET /emsp-dashboard", adminPage(h.EmspPage))
	mux.Handle("GET /emsp-dashboard/audits", adminPage(h.AuditsPage))
	mux.Handle("GET /emsp-dashboard/audits/{pool_code}", adminPage(h.AuditsPage))
	mux.Handle("GET /emsp-dashboard/poster/{pool_code}/{station_name}", adminPage(h.PosterPage))
	mux.Handle("GET /api/admin/station-link", adminAPI(h.StationLinkAPI))
	mux.Handle("GET /api/admin/qr/{pool_code}/{station_name}", adminAPI(h.QRImage))
	mux.Handle("DELETE /api/admin/stations/{pool_code}/{station_name}", adminAPI(h.DeleteStationAPI))
	mux.Handle("GET /api/admin/audits/{session_id}/export", adminAPI(h.AuditExport))
	// endregion

	mux.HandleFunc("/", h.Fallback)

	// Rejects cross-site browser requests that change state (Sec-Fetch-Site / Origin); requests without those
	// headers, such as the CSMS's, pass.
	return securityHeaders(http.NewCrossOriginProtection().Handler(mux))
}

// securityHeaders adds the headers every answer carries: no MIME sniffing, no framing (clickjacking), and no
// referrer sent to other sites.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
