---
title: Maintenance mode
description: Take the API down for an upgrade or migration and bring it back — `ims down` / `ims up`, the dashboard toggle, what stays open, the bypass secret, background work and running it across many servers.
layers: [api, app, dashboard]
---

One switch takes the API down and brings it back, like Laravel's `artisan down` / `up`. While
down, API clients get a clear `503` with your message and a retry time; developers keep working
(the dashboard, a bypass secret); health checks and the dashboard show that the app is down.

```bash
ims down --message="Upgrading the database" --retry=120 --with-secret
ims maintenance:status
ims up
```

Or open the developer dashboard: **Inspector → System → Maintenance mode** has the toggle (needs
`dev.manage`), and the **Commands** panel and the Ctrl+K palette have `down` and `up`.

## What clients see

Every request outside the open paths answers:

```http
HTTP/1.1 503 Service Unavailable
Retry-After: 120
Content-Type: application/problem+json

{"status":503,"error_code":"MAINTENANCE_MODE","detail":"Upgrading the database","retry_after":120, ...}
```

`detail` is your `--message` (default "The service is down for maintenance. Please try again
shortly."); it is shown even in production, unlike other 5xx details. Front ends should treat
`MAINTENANCE_MODE` as "show a maintenance screen and retry after `retry_after` seconds", not as an
error.

## What stays open

| Path | Why |
| --- | --- |
| `/up`, `/api/health`, `/metrics` | probes and scrapes keep working; `/up` stays 200 so load balancers neither restart nor drop the servers |
| `/dev`, `/dev/*` (and `/dev/api/*`) | the developers doing the maintenance, and the toggle that ends it |
| `/docs`, `/docs/openapi.yaml` | static documentation |

Everything else — `/api/v1/*`, `/mcp`, `/oauth/*` — answers 503. The list is
`maintenance.OpenPaths` in `internal/platform/maintenance`; a product passes its own list to
`maintenance.Guard` in `internal/app/routes.go` to open more.

`GET /api/health` keeps its status code and adds a block:

```json
"maintenance": {"down": true, "since": "2026-11-22T02:00:00Z", "message": "Upgrading the database", "retry_after": 120}
```

> The dashboard is open, but **signing in** to it goes through `POST /api/v1/login`, which is
> closed. Sign in before you take the API down (dashboard sessions last
> `AUTH_TOKEN_EXPIRATION`), or end maintenance with `ims up` on the server.

## Letting a client through

`ims down --with-secret` prints a generated secret once (`--secret=...` sets your own, at least
8 characters). A request carrying it passes:

```bash
curl -H "X-Maintenance-Bypass: 3f1c..." https://api.example.com/api/v1/me
```

It is a header, not Laravel's cookie or URL: API clients send headers, and a secret in a URL ends
up in logs. Only its SHA-256 hash is stored, it is compared in constant time, and the header is
redacted in dashboard telemetry. A browser client on another origin needs `X-Maintenance-Bypass`
in `CORS_ALLOWED_HEADERS` (it is in the default list). Every new `down` replaces the secret; `up`
clears it. Staff tokens get no special pass: nothing is authenticated before the check.

## Background work

- **Scheduler:** scheduled tasks are skipped while down (logged as "skipped (maintenance
  mode)"). A task that must still run sets `EvenInMaintenance: true` in `internal/app/schedule.go`:
  the scheduler heartbeat does (so health stays green), and so do `backup.run` and
  `backup.clean` (a backup before a risky change still runs). `ims schedule:run <task>` is an
  explicit request and always runs.
- **Queue:** workers keep running. Queued work (mails, exports) was already accepted and
  finishing it is harmless. If a migration needs a quiet queue, pause the queues in the
  dashboard's **Queue** panel first and resume them after `ims up`.

## Many servers

The state is one row in the `maintenance` table, so every API process, worker and scheduler sees
the same switch, wherever it runs. Each process keeps an in-memory copy refreshed every
`MAINTENANCE_CHECK_INTERVAL` (default `5s`): a request costs no query, and a toggle reaches every
process within that interval (the process that toggled sees it at once). If the database cannot
be read, a process keeps its last known state.

```bash
MAINTENANCE_CHECK_INTERVAL=5s
```

`ims serve` logs a warning at start when the app is down, and the dashboard's **About** command
shows the state.

## Who did it

Every `down` and `up` is a `maintenance.down` / `maintenance.up` row in the system activity log
(no tenant; readable with `activity.view.all`): who (or the CLI), the message and retry, and on
`up` how long it was down. Dashboard toggles are also in the dashboard **Audit log**. The service
reports toggles through the `maintenance.Audit` interface, which `activity.Recorder` implements
(see [activity log](activity-log.md#maintenance-mode)).

## A typical upgrade

```bash
ims down --message="Upgrading to 0.2.0" --retry=300 --with-secret
# optional: pause queues in the dashboard if the migration needs them quiet
ims migrate
# smoke-test with the bypass header
curl -H "X-Maintenance-Bypass: <secret>" https://api.example.com/api/v1/system/info
ims up
```

## In the app

While maintenance mode is on, the first refused call sends the app to its maintenance screen (`/maintenance`) instead of failing request by request. **Retry** returns to the app once the API answers again.
