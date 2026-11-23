# syntax=docker/dockerfile:1
#
# The same single binary the systemd deploy ships, in a small image. Docker is
# optional: nothing in the app needs it. One image runs every role, chosen by
# the command:
#
#   docker run ims-go-admin-starter serve          # HTTP API + dashboard (default)
#   docker run ims-go-admin-starter queue:work     # River workers
#   docker run ims-go-admin-starter schedule       # cron scheduler
#   docker run ims-go-admin-starter migrate        # apply migrations (a release step)
#
# compose.yaml wires them together with PostgreSQL.

# ── 1. Developer dashboard (React SPA, embedded into the binary) ─────────────
FROM node:24-alpine AS web
WORKDIR /src/web/dev
COPY web/dev/package.json web/dev/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/dev/ ./
RUN npm run build

# ── 2. Go binary (CGO-free, version stamped from VERSION) ────────────────────
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dev/dist ./web/dev/dist
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/version.Version=$(cat VERSION)" \
      -o /out/ims ./cmd/app

# ── 3. Runtime ───────────────────────────────────────────────────────────────
FROM alpine:3.22
# ca-certificates: S3, mail and webhooks over TLS · tzdata: APP_TIMEZONE and
# cron schedules · postgresql17-client: pg_dump for `backup:run`.
RUN apk add --no-cache ca-certificates tzdata postgresql17-client \
 && addgroup -S ims && adduser -S -G ims -h /app ims \
 && mkdir -p /app/var /app/backups && chown -R ims:ims /app
WORKDIR /app
COPY --from=build /out/ims /usr/local/bin/ims
USER ims

ENV APP_LISTEN_ADDR=:8080 \
    APP_DATA_DIR=/app/var \
    APP_STORAGE_DIR=/app/var/storage \
    APP_CACHE_DIR=/app/var/cache \
    BACKUP_DIR=/app/backups
# Uploads, the file cache and backups outlive the container.
VOLUME ["/app/var", "/app/backups"]
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/up >/dev/null || exit 1

ENTRYPOINT ["ims"]
CMD ["serve"]
