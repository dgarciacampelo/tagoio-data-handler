# tagoio-handler

Go-between of the TagoIO platform and the CSMS instances (neos-ocpp) of the Velo Energy charging pools, written in
Go with Svelte 5 pages.

- **TagoIO → CSMS.** The TagoIO dashboards' forms and buttons (rates, RFID cards, operator details, OCPP commands…)
  and each installation's MQTT meter trigger *external analyses*. The service receives them over TagoIO's realtime
  Server-Sent Events connection, turns them into typed events and streams them to the CSMS on `GET /api/stream`.
  It is an anti-corruption layer: the CSMS never sees TagoIO payloads.
- **CSMS → TagoIO.** The CSMS reports connector statuses and charging sessions; the service keeps the live state,
  stores the session history (for the audits), and writes the pool dashboards in each pool's TagoIO bucket.
- **Pages.** The public station page drivers open from the QR code of a station (status, payment link form, rates,
  instructions, terms), and the admin pages: eMSP dashboard with the QR posters, and the session audits with their
  XLSX export.
- **Housekeeping.** Keeps the TagoIO buckets below their record limit, purges old telemetry and backs the database
  up to Telegram.

## Layout

```
cmd/server/              Entry point: configuration, wiring, routes, graceful shutdown
internal/
  config                 Environment configuration, validated at startup (-check-config to only validate)
  tagoio                 TagoIO REST client (account and device calls, dry-run guard on writes)
  tagoio/realtime        TagoIO realtime (SSE) client of the external analyses
  analysis               Analysis handlers: TagoIO scope -> typed event + side effects
  events                 /api/stream event contract and broker
  chargepoints           Connector statuses (with the under-voltage quarantine) and charging sessions
  bucket                 A pool's TagoIO bucket by pool code (device-full recovery, validation toasts, cleanup)
  devices                Pool -> TagoIO device and token registry
  pools                  Pool configuration cache (operator, rates, pre-authorization) and CSMS pool setup
  provisioning           CPO sign-up: TagoIO user and pool device
  maintenance, scheduler Bucket data checks, telemetry purge, backups; daily jobs in Europe/Madrid
  store                  SQLite (pure Go driver) with versioned migrations
  telegram               Notices and backups
  export                 Session audit workbook (XLSX)
  handlers               HTTP handlers (machine API, station page, admin pages and APIs)
  dto                    JSON contract of the pages -> frontend/src/lib/contract.gen.ts (tygo)
  webui                  Page shell and Vite assets (embedded with the "embedui" build tag)
frontend/                Svelte 5 pages (Vite, Tailwind CSS 4, Paraglide JS), one per src/pages/<page>
```

## Contracts with other services

| Endpoint | Caller | Notes |
| --- | --- | --- |
| `GET /api/stream` | neos-ocpp | SSE: one `data: {"event": <type>, "data": {...}}` line per event, keep-alive every 15 s. Event shapes in `internal/events`, checked against the former Pydantic models. |
| `POST /v1/charge-point-update/{pool_code}/{station_name}/{connector_id}` | neos-ocpp | Connector status. |
| `POST /v1/charging-session-update/{pool_code}/{station_name}/{connector_id}` | neos-ocpp | Session start, meter values and end. |
| `GET /api/pools/{pool_code}` | neos-ocpp | Pool setup (creates the TagoIO device when missing). |
| `POST /api/provision-cpo` | neos-notification-mgmt | CPO sign-up form (snake_case or the Google Form titles). |
| `POST /v1/trigger-task/pool-variable-cleanup`, `POST /v1/trigger-task/backup-to-telegram`, `DELETE /v1/trigger-task/single-variable/{pool_code}/{variable}` | Operators (curl) | Background jobs answer 202. |

All of them use HTTP Basic with `APP_DEFAULT_USER` / `APP_DEFAULT_TOKEN`. The admin pages use a login form and a
signed session cookie (`APP_ADMIN_USER` / `APP_ADMIN_TOKEN`, `DASHBOARD_SECRET_TOKEN`).

## Development

Requirements: Go 1.27, Node 22 (see `frontend/package.json`).

```bash
cp .env.example .env            # Fill it in
npm --prefix frontend ci
npm --prefix frontend run dev   # Vite on :5173
```

```bash
APP_ENV=development API_PORT=8090 DRY_RUN=true go run ./cmd/server   # Browse http://localhost:8090
```

In development the server reads `.env` and loads the page scripts from Vite (hot reload). `DRY_RUN=true` logs the
events, the TagoIO writes and the Telegram messages instead of sending them; keep it on whenever the `.env` holds
production tokens.

Checks:

```bash
go vet ./... && go test ./...
npm --prefix frontend run check
go generate ./internal/dto      # After changing internal/dto: regenerates the TypeScript contract
```

The production image (`golang.Dockerfile`) builds the pages, embeds them in the binary and runs it as a non-root user
with the database in `/data`.

## Cutover from the Python service (by 2026-11-19)

TagoIO turns off the Socket.IO realtime connection the Python service uses on **2026-11-19**.

1. **Capture the analysis payloads** (dry run): run the Go service with the production `.env`, `DRY_RUN=true` and
   `LOG_LEVEL=info`, trigger each dashboard action once, and turn the logged scopes (`Analysis: trigger received`,
   with the variable names) into tests in `internal/analysis/testdata`. Then replace the positional fields in
   `internal/analysis/handlers.go` with the variable names. Only the OCPP request names are confirmed so far.
2. **Staging soak**: at least 24 h with `DRY_RUN=true` against a copy of `database_files/`, and neos-ocpp
   (staging) pointed at it.
3. **Cutover commit**: delete `src/`, `templates/`, `static/`, `tests/`, `requirements.txt`, `pytest.ini`,
   `ruff.toml`, `tailwind.config.js` and the Python `Dockerfile`/`docker-compose.yml`. Rename `golang.Dockerfile` to `Dockerfile` and
   `docker-compose.go.yml` (pointing it at `Dockerfile`).
4. **Deploy**: stop the Python container, `docker compose up -d
   --build`. neos-ocpp reconnects to `/api/stream` by itself. Check the 21:15 backup reaches Telegram.
5. Delete the unused `pay_start_transaction` / `pay_stop_transaction` analyses in TagoIO. The `.env` variables
   `API_VERSION`, `PAYMENTS_GW_DEVICE_TOKEN`, `TAGO_DATA_AMOUNT_*`, `TAGOIO_HANDLER_URL` and `TEST_*` are no longer
   read and can go.

Rollback until 2026-11-19: restart the previous Python image. After that date the Python service can no longer
receive analysis triggers.
