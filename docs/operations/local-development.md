---
title: Local development
---

Run the whole system locally: the app, the developer dashboard and the API, plus the queue worker
and the scheduler. `make dev` starts the first three with hot reload; the worker and the scheduler
are the same binary in two more shells.

| Process | Command | Purpose |
|---|---|---|
| Web + API | `make dev` | developer dashboard (Vite, `:8080/dev`), app (Vite, `:8082`), API (air, `:8081`) |
| API alone | `ims serve` (or `make dev-api`) | everything on `:8080` from the embedded builds, as in production |
| Worker | `ims queue:work` | processes River jobs (`critical`/`default`/`heavy`) |
| Scheduler | `ims schedule` | runs cron tasks (heartbeat, billing, cleanup) |

Exactly **one** scheduler and **one** (or more) worker per environment.

## Prerequisites

- Go 1.26.11+ (older 1.26 patches have standard-library CVEs), PostgreSQL 16+, GNU Make
- Node.js 20.19+ with npm (the app and the developer dashboard); 22.23+ for the docs site
- Optional: Docker (throwaway Postgres); `air` + `golangci-lint` via `make tools`

## First-time setup

```bash
cp .env.example .env          # .env is git-ignored
make key-generate             # APP_KEY into .env (optional locally, required in staging/production)
make tools                    # install air + golangci-lint
npm --prefix web/dev ci       # dashboard dependencies (Vite/React)
git config core.hooksPath .githooks   # enable pre-commit/pre-push hooks
chmod +x .githooks/pre-commit .githooks/pre-push   # macOS/Linux only

make db-up                    # Postgres via docker compose: creates ims_go_api (app) and
                              # ims_go_api_test (integration tests). With your own server:
                              #   createdb ims_go_api && createdb ims_go_api_test

make migrate                  # goose migrations AND River's tables
make seed                     # permissions, roles, tenant+branch, admin + developer users
```

Seeded accounts (both use `AUTH_DEFAULT_PASSWORD`, default `123456`):

| Account | Role | Purpose |
|---|---|---|
| `superadmin@ims.com` | Super-Admin | full API + dashboard access |
| `developer@ims.com` | hidden Developer | developer dashboard without using the super-admin account |

The default tenant/branch are `1` / `1`. `make seed` is idempotent.

Set a shared cache driver so the three processes see the same counters and scheduler heartbeat:

```dotenv
CACHE_DRIVER=database     # memory | file | redis | database
QUEUE_DRIVER=river        # river (durable, needs the worker) | sync (inline, no worker)
```

`database` cache uses the `cache` table created by the migrations.

## Run the full stack

Three shells, same working directory:

```bash
# Shell A — the app, the dashboard and the API, all with hot reload
make dev                  # = migrate + dashboard (:8080) + app (:8082) + air API (:8081)
# Open http://localhost:8082 for the app and http://localhost:8080/dev/ for the
# dashboard. Both Vite servers proxy /api, /storage and /docs to the API on :8081.
# The app starts only once `npm --prefix web/app ci` has run. `make dev-api`
# still runs the API alone on :8080 with the embedded production build.

# Shell B — queue worker
make queue                # = go run ./cmd/app queue:work
#   go run ./cmd/app queue:work --queues=critical
#   go run ./cmd/app queue:work --once

# Shell C — scheduler daemon
make schedule             # = go run ./cmd/app schedule
```

After `make build-all` the same roles are `bin/ims serve`, `bin/ims queue:work`, `bin/ims schedule`.
(`make build` alone embeds whatever is already in `web/dev/dist` and `web/app/dist`; `build-all`
builds both first, so use it for a release.)

`make dev` puts Vite on the canonical `:8080` so the dashboard, API docs and proxied API all share
one origin and HMR works in the browser. Override the ports with `IMS_WEB_PORT` (default `8080`),
`IMS_APP_PORT` (default `8082`) and `IMS_API_PORT` (default `8081`).

For the standalone SPA dev server on `:5173`, run `make dev-api` (API on `:8080`) in one shell and
`make dev-web` in another; `IMS_API_ORIGIN` defaults to `http://127.0.0.1:8080`. Do not combine
`make dev-web` with `make dev`, because `make dev` already uses `:8080` for Vite.

## Verify

```bash
curl http://localhost:8080/up
curl http://localhost:8080/api/health             # DB, cache, queue, scheduler and backup checks
curl http://localhost:8080/api/v1/system/info
# Dashboard: http://localhost:8080/dev/  (developer@ims.com or superadmin@ims.com)
# API docs UI (enabled outside production): http://localhost:8080/docs

# log in and use the token
curl -X POST http://localhost:8080/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{"email":"superadmin@ims.com","password":"123456","device":"cli"}'
curl http://localhost:8080/api/v1/user -H "Authorization: Bearer <id|secret>"

make health                                       # or: go run ./cmd/app health:check
make config                                       # secrets masked
go run ./cmd/app queue:stats                      # QUEUE / TOTAL / ACTIVE / FAILED
go run ./cmd/app schedule:list
go run ./cmd/app schedule:run scheduler.heartbeat
go run ./cmd/app backup:list
```

`DEV_DASHBOARD_ENABLED` defaults to true outside production; set it to `false` to make `/dev` and
`/dev/api/*` return 404. The hidden `Developer` account holds the developer-dashboard
permissions (`dev.view`, `dev.manage`) and `user.impersonate` — it cannot administer tenants,
users or billing data through the API, but it can act as a tenant user (`X-Acting-User`), and every
such request is written to the system audit log. Still rotate its password outside local use.

To watch a job end-to-end, trigger a background user export (`POST /api/v1/user/export-background`),
watch Shell B process it, then check `queue:stats` and the job status.

## Scheduled tasks

| ID | Spec | Description |
|---|---|---|
| `scheduler.heartbeat` | `* * * * *` | heartbeat for the health check |
| `dataprocessing.cleanup-completed` | `0 2 * * *` | prune completed import/export jobs |
| `billing.expire-subscriptions` | `0 */6 * * *` | mark overdue subscriptions expired |
| `billing.send-renewal-reminders` | `0 8 * * *` | 7/3/1-day renewal reminders |
| `backup.run` | `30 1 * * *` | database backup (when `BACKUP_ENABLED=true`) |
| `backup.clean` | `30 2 * * *` | backup retention cleanup (when enabled) |
| `notifications.purge-expired` | `30 3 * * *` | delete notifications expired for 7+ days |
| `activity.prune` | `45 3 * * *` | activity log older than `ACTIVITY_RETENTION_DAYS` |
| `devdashboard.prune` | `0 3 * * *` | dashboard telemetry and audit rows past their retention |
| `cache.purge-expired` | `15 * * * *` | expired rows in the database cache (`CACHE_DRIVER=database`) |

Times use `SCHEDULE_TIMEZONE` (falls back to `APP_TIMEZONE`).

## Everyday commands

```bash
make help
make run                  # API once (no hot reload)
make test                 # unit tests
make check                # format-check + vet + lint + tests
make web-typecheck        # dashboard TypeScript
make build-all            # SPA + Go binary (use for release)
make migrate-status       # goose + River migration state
make generate             # sqlc + OpenAPI contract + generated regions
make openapi-check        # spec validity + route drift + region freshness
make gen-check            # fail on generated-code drift
go run ./cmd/app permission:sync
go run ./cmd/app settings:sync
go run ./cmd/app feature-flags:sync
go run ./cmd/app cache:clear
go run ./cmd/app backup:run
```

Most of these are also one click away in the dashboard's **Commands** page
(`http://localhost:8080/dev/commands`, or Ctrl+K → the command name), with their output and history.

## Feature knobs (`.env`)

| Need | Set |
|---|---|
| Durable jobs | `QUEUE_DRIVER=river` + run the worker |
| Inline jobs, no worker | `QUEUE_DRIVER=sync` |
| Shared cache / counters / heartbeat | `CACHE_DRIVER=database` (or `redis` + `REDIS_*`) |
| Email | `MAIL_DRIVER=log` (default) or `smtp` + `MAIL_HOST`/`MAIL_PORT`/`MAIL_USERNAME`/`MAIL_PASSWORD` |
| Uploads | `FILES_DISK=public` (any disk: `private`, `public`, `s3` or a `STORAGE_DISKS` name such as an R2/MinIO bucket); `APP_STORAGE_DIR`, `STORAGE_*` |
| Metrics / docs | `METRICS_ENABLED=true` (`/metrics`), `DOCS_ENABLED=true` (`/docs`) |
| Traces | `docker compose --profile otel up -d jaeger` (or the Jaeger binary) + `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318` (not `localhost`: on Windows it resolves to `::1`, where the Jaeger binary does not listen); Jaeger UI on `:16686` |
| Developer dashboard | `DEV_DASHBOARD_ENABLED=true` (default outside production); `/dev` |
| Billing gateways | `BKASH_*`, `NAGAD_*`, `BANK_TRANSFER_*` |
| Database backups | `BACKUP_ENABLED=true`, `BACKUP_DIR`, `BACKUP_DESTINATIONS=local,s3` + `S3_*` (see `backups.md`) |

Use a specific file with `ims --env-file /path/to/.env serve` (flag → `APP_ENV_FILE` → `./.env`;
real environment variables always override file values).

## Troubleshooting

- `queue:work` exits immediately: it requires `QUEUE_DRIVER=river`. With `sync`, jobs run inline and
  no worker exists.
- Health shows a stale-scheduler warning: `CACHE_DRIVER=memory` is per-process — set `database`/`redis`.
- `migrate` fails: it creates the schema, not the database. Create the database first, or repoint
  `DATABASE_DSN`.
- `make dev` cannot find Vite: run `npm --prefix web/dev ci` once (Node.js 20.19+; with an older
  Node the install silently skips Vite's native bundler and the build fails to load it).
- `/dev` returns 404: set `DEV_DASHBOARD_ENABLED=true`; the dashboard is off in production by default.
- Dashboard login fails: run `make seed` (it creates `developer@ims.com` and the hidden role).
- Integration tests: set `DATABASE_TEST_DSN` (they skip, never fail, when it is unset).
- New API routes answer 404/405 after an edit, and air logs `bind: Only one usage of each socket
  address` (Windows): an earlier build is still running and holds `:8081`. It is left over when air
  is stopped without its process tree (a closed terminal, a killed `node`); air itself replaces
  its builds cleanly. `make dev` now stops a leftover `tmp\ims.exe` of this project before it
  starts; when air runs on its own, stop the old process
  (`Get-Process ims | Where-Object Path -like "*	mp\ims.exe" | Stop-Process`). The worker and
  scheduler never hot-reload: restart `make queue` / `make schedule` after changing jobs or tasks.
- Every new Redis connection shows a failed `hello` span in the trace: the server is older than
  Redis 6. go-redis first asks for the RESP3 protocol (`HELLO 3`), Redis 5 refuses, and it falls
  back to RESP2. Harmless; it goes away on Redis 6 or later.
- `backup:run` fails: install PostgreSQL client tools and point `BACKUP_PG_DUMP_PATH` at a
  `pg_dump` version matching or exceeding the server; see [`backups.md`](backups.md).

Production is a separate runbook: see [`deploy.md`](deploy.md).
