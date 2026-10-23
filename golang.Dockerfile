# syntax=docker/dockerfile:1
# Image of the Go service (renamed to Dockerfile at the cutover, replacing the Python one; not "Dockerfile.go": Go would
# read a .go file as source).
# Build context: the repository root.

# * Stage 1: the Svelte pages (Vite), written to internal/webui/dist
FROM node:22.21.1-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json frontend/.npmrc ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit
COPY frontend/ ./
RUN npm run build

# * Stage 2: the Go binary, with the pages embedded (build tag "embedui")
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /src/internal/webui/dist ./internal/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -tags embedui -ldflags="-s -w" -o /out/server ./cmd/server

# * Stage 3: runtime
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata su-exec && adduser -D -H -u 10001 app
ENV TZ=Europe/Madrid \
    APP_ENV=production \
    DB_PATH=/data/database.sqlite3
WORKDIR /app
COPY --from=build /out/server /app/server

# The container starts as root only to give the mounted database folder to the service user (its files may belong
# to root or to the host user), then runs the service as that user. Written inline, so a Windows checkout can never
# turn its line endings into CRLF.
COPY --chmod=0755 <<'EOF' /app/entrypoint.sh
#!/bin/sh
set -e
if [ "$(id -u)" = "0" ]; then
    chown -R app:app /data
    exec su-exec app:app /app/server "$@"
fi
exec /app/server "$@"
EOF
VOLUME /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
    CMD wget -q -O /dev/null "http://127.0.0.1:${API_PORT}/healthz" || exit 1

# Last, so a new commit does not invalidate the cached layers above.
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
LABEL org.opencontainers.image.revision=$GIT_COMMIT
ENV GIT_COMMIT=$GIT_COMMIT \
    BUILD_TIME=$BUILD_TIME
ENTRYPOINT ["/app/entrypoint.sh"]
