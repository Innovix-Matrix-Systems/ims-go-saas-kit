# AGENTS.md — Working Guide for This Repository

Instructions for AI coding agents (and humans) working on the IMS Go SaaS Kit. Read this before
writing code. The human documentation lives in [`docs/`](docs/README.md) (Markdown, read on GitHub;
the published site is the API starter's): start with the [tutorial](docs/tutorial/first-module.mdx), the
[guides](docs/guides/modules.md) and the [design decisions](docs/architecture/decisions.md). Task
recipes live in
**skills**: `.agents/skills/` (canonical) and the identical copy in `.claude/skills/`
(`make skills-sync` copies, `make gen-check` fails on drift).

| Skill | Use when |
|---|---|
| `add-module` | adding a feature/CRUD module or an endpoint to one |
| `add-cli-command` | adding or changing an `ims` CLI command |
| `db-change` | migrations, sqlc queries, `internal/dbgen` regeneration |
| `dev-dashboard` | dashboard panels, `/dev/api` endpoints, dashboard commands |
| `verify-changes` | finishing any task: the gates to run before reporting done |

## What this repo is

- **Go monolith** with Laravel ergonomics: feature-first modules, explicit dependency injection,
  typed request validation, RFC 9457 problem+json errors, `{data}` envelopes, multi-tenant +
  branch isolation, RBAC, billing with feature limits, settings + feature flags (with percentage
  rollout), activity log, cache drivers, swappable storage disks (local/S3/R2/MinIO), River
  queue, cron scheduler, Cobra CLI — all in **one binary** (`cmd/app`), deployed with systemd
  (no containers required; `Dockerfile` + `compose.yaml` run the same binary, built in CI).
- **Developer dashboard** (`/dev`, React SPA embedded via `go:embed`): Telescope-style requests,
  logs, exceptions, queue, backups, commands, inspector (system health, routes, roles, config,
  packages), audit log, tenants, sessions, settings, feature flags, files, developers, playground;
  Ctrl+K command palette.
- **Known gaps** (do not assume they exist): the dashboard has no frontend test suite,
  golangci-lint is still v1.

## Golden commands

```bash
make help                  # list every task (dev, build, test, lint, generate, db, ...)
make dev                   # dashboard on :8080 (/dev) + client app on :8082 (both HMR) + API on :8081 (air)
make dev-api               # API only, hot reload via air
make build                 # bin/ims (CGO-free)
make check                 # format-check + vet + lint + unit tests
make generate              # sqlc + oapi-codegen + OpenAPI regions
make gen-check             # fails when generated code, OpenAPI regions, docs reference pages or skills drifted
make openapi-check         # spec valid + no route drift (every route must be in api/openapi.yaml)
go run ./cmd/app route:list --path=/api/v1/x --json   # live routes with permission, handler, middleware
make migrate               # goose + River migrations
make web-typecheck         # dashboard: tsc --noEmit
make web-style-check       # dashboard: token contrast and style rules
make web-build             # dashboard: build web/dev/dist (embedded by the binary)
make app-typecheck         # client app: tsc --noEmit (also app-style-check, app-openapi-check)
make app-build             # client app: build web/app/dist (embedded, served at /)
make docs-gen              # docs/reference/* from the CLI, .env.example, permission catalog, Makefile

# Integration tests (real Postgres; each package gets its own schema):
$env:DATABASE_TEST_DSN = "postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable"
go test -count=1 ./...
go test -count=1 -race -p 2 ./...   # -p 2 locally: parallel race builds can exhaust memory
```

Finish work with the `verify-changes` skill. Commit only when the user asks; then one commit per
feature with a message that says what changed and why.

<!-- rename:keep -->
## Renaming a project made from the template

`make rename MODULE=github.com/acme/shop-api [NAME=] [BINARY=] [TITLE=] [DRY=1]` (`cmd/rename`,
logic in `internal/rename`) rewrites the module path, the `ims` binary/CLI/user, the
`ims-go-saas-kit` repo and deploy name, the `IMS Go SaaS Kit`/`IMS` titles and the `IMS_` deploy
variables in every tracked file, renames the systemd units, then runs `go mod tidy`, `make
generate` and `make docs-gen`. It keeps `@ims.com` seed e-mails and Innovix credit links, refuses a
dirty tree, and runs once. When you add a file or name that carries the starter's name, check it
with `make rename MODULE=github.com/acme/x DRY=1`.
<!-- rename:end -->

## Repository map

```text
cmd/app/                      single binary entry point
cmd/rename/, cmd/skills/      dev tools: `make rename`, `make skills-sync` / `skills-check`
module.go                     root package: embeds go.mod and VERSION
VERSION                       release version (semver); release builds stamp it, local builds report <v>-dev
CHANGELOG.md                  Keep a Changelog; update it with every user-visible change
docs/                         human documentation (Markdown, read on GitHub)
web/dev/                      dashboard SPA (TanStack Router/Query, cmdk); embed.go also embeds package.json
  src/components/ui.tsx       Card, Button, Chip, Drawer, Modal, Tabs, StatCard, Icon, ...
  src/components/inputs.tsx   Select, DatePicker, TimePicker (use these, never native <select>/date)
web/app/                      client app SPA served at / (Vite + TanStack Router plugin, React Query, cmdk)
  src/api/client.ts           api() wrapper: /api/v1 paths, ApiError (problem+json), token storage, app-wide events
  src/auth/session.ts         sign-in/2FA/sign-out, session query (profile + permissions), can()
  src/components/navigation.ts every page: label key, icon, group, `g` letter, required permission
  src/i18n/                   en.ts (reference catalog) + bn.ts; useT(), Accept-Language sent with every call
  public/appearance.js        theme/accent/density before first paint (a file, so the CSP needs no inline script)
internal/
  app/        composition root: platform.go (shared deps), schedule.go (recurring tasks),
              wiring.go (HTTP deps), routes.go (middleware chain + route groups), dev_commands.go (dashboard commands)
  cli/        Cobra tree: BuildRoot's registrars list (root.go) is the one place commands are wired;
              withPlatform(state, fn) boots the app for a command (platform.go)
  config/     typed config, one file per section (app, database, cache, queue, auth, billing,
              storage, backup, mail, ops, activity, env); config.go = Load + cross-section Validate
  apperr/     application error codes + *apperr.Error
  authn/      opaque tokens, OTP, throttle, recovery (password reset / e-mail verify), bearer
              middleware, Identity, UserID(r) / UserIDFromContext(ctx);
              social/ = social login provider drivers (Google, GitHub)
  authz/      permission Catalog, Checker (Can/MustCan, super-admin bypass), Require → *Gate
  tenancy/    Scope, Filter (fails closed), Resolve/Exempt/InternalOnly middleware, From(r)
  testkit/    integration-test app + factories (User, Role, Tenant, SuperAdmin) + request builder
  platform/   technical infra:
    database/   pgx registry, TxManager (WithTx, WithTenantLock, TryHoldLock), migrate, dbtest
    cache/      Store interface + memory|file|redis|database drivers
    queue/      Engine (sync | River), job registry, admin
    scheduler/  task registry, cron runner, heartbeat, run history (schedule_runs)
    httpx/      Kit (Bind/Validate/PathID + Responder), Binder, Validator, Problem (RFC 9457),
                pagination, middleware, ClientIP(ctx)/UserAgent(ctx)
    storage/    Disk interface (Put/Get/Delete/URL/TemporaryURL/Public) + local and S3 drivers
    telemetry/  dashboard capture (requests, SQL, cache, queue, logs) + Redactor
    tracing/    OpenTelemetry: OTLP setup, HTTP/SQL/job/outbound spans, trace ids in logs; Begin/Annotate
    maintenance/ `ims down`/`up`: state row, cached Checker, Guard middleware (503 MAINTENANCE_MODE)
    pubsub/     live-update bus: Publisher (pg_notify), Hub (this process's streams), Listener (one LISTEN)
    delivery/   the one way to reach a person: templates (txt/html, locale variants), mail/sms/push
                channels, delivery.send job, deliveries log (no bodies, masked addresses)
    health/ i18n/ mail/ (smtp/log transport) sms/ (log/twilio transport) ratelimit/ metrics/
  modules/    business features (see "Module anatomy"):
    access activity announcements auth backup billing branches dashboard dataprocessing dev
    featureflags files identity notifications oauth onboarding privacy realtime settings system
    tenants webhooks
  privacy/    leaf registry for erasure/export: Contributor, Guard, Writer (modules declare, modules/privacy runs)
  scaffold/   `ims gen module` + `ims gen openapi` generators
  dbgen/      sqlc generated code (never edit)
  contract/   oapi-codegen generated code (never edit)
api/openapi.yaml              canonical API contract (served at /docs)
migrations/                   goose SQL, embedded (also sqlc's schema source)
sql/queries/                  sqlc queries
```

## Module anatomy (the pattern to follow)

Small/medium modules are flat; large modules (billing) split into subpackages:

```text
internal/modules/<name>/
├── routes.go        Route registration + each route's permission:
│                    r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)
├── handler.go       HTTP transport: h.Bind → service → h.Data/h.Error. No SQL, no rules.
│                    Large handlers split by resource (identity: users_/profile_/import_export_handler.go).
├── request.go       Request DTOs (validate tags) + response DTOs & mappers.
├── service.go       Business rules, tenancy, invariants, transactions, activity records.
├── repository.go    Repo interface + sqlc-backed impl. No HTTP concepts.
└── model.go         Domain structs + row mappers (never expose dbgen types outside).

internal/modules/billing/           Big-module layout (HTTP stays at the module root):
├── routes.go, handler.go, request.go, orchestrator.go, drivers.go
├── domain/   shared types: features.go, model.go, repository.go, gateway.go, invoice.go
└── service/  guard.go, package_service.go, subscription_service.go, payment_service.go, ...
```

A handler action, the whole shape:

```go
func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) { // JSON + validate; writes the 400/415/422 itself
		return
	}
	branch, err := h.service.Create(r.Context(), tenancy.From(r), authn.UserID(r), req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*branch))
}
```

Rules of the layering (enforced by review and `route_permissions_integration_test.go`):

1. `handler → service → repository → dbgen`. Services never import `httpx`; repositories never
   import `httpx`/`authn`.
2. Handlers embed `httpx.Kit` (built once in `wiring.go`, passed to every `NewHandler`, including
   the dashboard's); each module defines `func (h *Handler) can(perm string)` returning
   `authz.Require(h.checker, perm, h.Responder)`.
3. Permissions are declared on the route. Call `h.checker.MustCan(ctx, authn.UserID(r), perm)`
   inside a handler only when the permission depends on the payload (e.g. sending `roles`).
4. Services are callable from HTTP, CLI, jobs and scheduled tasks — no `http.Request` below the
   handler. Optional collaborators are `WithX(...)` options (`WithTx`, `WithActivity`, ...).
5. Repositories take explicit typed params (`tenancy.Filter`, `tenantID`); services own
   transactions (`TxManager.WithTx`, `WithTenantLock` for quota checks). Inside a lock, every
   query runs on the locked transaction (`q`); read anything else (like the plan limit) before
   taking it, or concurrent requests can fill the pool and deadlock.

### Adding a new module (checklist — details in the `add-module` skill)

1. Prefer `ims gen module --name=x --fields="..."` (writes migration, queries, model, repository,
   service, request, handler, routes, service test and OpenAPI regions), then adapt.
2. Migration + queries (`db-change` skill); `make sqlc`.
3. Service: tenancy from `tenancy.Scope`, stamping actor/tenant, `activity.Recorder` for changes
   tenant admins should see.
4. Handler + routes with `h.can(...)`; mount in `internal/app/routes.go`, wire in
   `internal/app/wiring.go`.
5. Permissions in `internal/authz/catalog.go` (+ `AdminTenantPermissions` group if tenant admins
   get it); `permission:sync`.
6. OpenAPI: `ims gen openapi --module=<name>` (or hand-write outside the generated regions);
   `make openapi-check`.
7. Tests: unit tests with fakes; an integration test with `testkit` including a cross-tenant
   isolation check.
8. Record non-obvious decisions in `docs/architecture/decisions.md` and the list below; update the guide in `docs/guides/`.

## Route groups (registered in `internal/app/routes.go`)

| Group | Middleware | Use for |
|---|---|---|
| Public | none | login/OTP, social login, password reset, e-mail verify, public packages, gateway callbacks, `/api/health`, `/up` |
| Authenticated (no tenancy) | `auth` | logout, the caller's own sessions, two-factor, account deletion and data export (tenantless accounts must reach them) |
| Authenticated + tenant-resolved | `auth` → `tenant.resolve` | tenant-scoped resources (users, branches, roles, files, settings, flags, activity, billing status) |
| Authenticated + tenant-exempt | `auth` → `tenant.exempt` (+ `tenant.internal`) | global management (tenants, permissions, internal users/roles/files, packages, approvals) |
| Dashboard `/dev/api` | `dev.enabled` → `auth` → `tenant.exempt` → `can:dev.view` | dashboard JSON API; mutations add `can:dev.manage` |
| MCP `/mcp`, `/oauth/*`, `/.well-known/oauth-*` | `mcp.enabled` → (for `/mcp`) MCP bearer token → scope check | the MCP connector; tools dispatch into `/api/v1` |

Global middleware (outermost first): `request-id → public-origin (non-local) → real-ip →
tracing (with an OTLP endpoint) → security-headers → cors → access-log → recover → telescope → metrics → maintenance → timeout →
throttle → locale`. The live table, with each route's permission: `ims route:list` (`--json` for
tools; `--path`, `--method`, `--middleware`) or the dashboard's Inspector → Routes.

## Cross-cutting patterns (memorize these)

**Errors** — services return `*apperr.Error`; handlers call `h.Error(w, r, err)`, which renders
RFC 9457 problem+json (`error_code`, `errors` for 422, `meta` for feature limits, `retry_after`
for 429, `trace_id` for 5xx). Never write raw JSON errors. Wrap causes with `apperr.Wrap`.

**Validation** — `h.Bind` (size limit, content type, strict fields, then `validate:` tags).
Business validation lives in services: `apperr.Validation(map[string][]string{...})`.

**Tenancy** — `tenancy.From(r)` gives the `Scope`; `scope.Filter()` is what repositories use for
single-row reads: one tenant, or every tenant only for internal/global reads. A zero `Filter`
matches **no** tenant rows (fails closed). Branch-scoped lists filter on `scope.ListBranchID()` (zero = every branch: a member who may switch and has not picked one); new rows are stamped with `scope.EffectiveBranchID()`. `X-Acting-User` (`user.impersonate`) runs the request as
another user (`Identity.ImpersonatorID` is the caller; mount `authn.DenyImpersonated` on account
changes). Public pre-sign-in routes use `tenancy.Public` (`X-Tenant-Code`). Impersonators send `X-Acting-Tenant` /
`X-Acting-Branch`.

> **Tenant isolation is explicit — there is no RLS.** Every tenant-scoped query MUST filter on
> an explicit tenant (SQL predicate `(@all_tenants OR tenant_id = @tenant_id)`). A query that
> forgets it is a security bug. Cover each module with a cross-tenant isolation test.

**Auth** — opaque tokens (`id|secret`, SHA-256 at rest, per device). `authn.UserID(r)` is
`auth()->id()`. Two-factor (`authn.TwoFactor`: `totp` or `email`, plus recovery codes) is asked
after the password through the existing `OTP_REQUIRED` → `/verify-otp` step (`second_factor.go`);
a tenant policy hands out setup-only tokens (`Identity.SetupOnly`, refused by tenancy with
`TWO_FACTOR_SETUP_REQUIRED`). Values the app must read back are encrypted with
`internal/platform/crypto` (`APP_KEY`); hash anything that only needs comparing. Super-admins bypass permission checks. CLI/jobs/scheduled tasks are trusted and do
not re-check permissions. Password reset and e-mail verification use single-use hashed tokens in
the cache (`authn.Recovery`); responses never reveal whether an e-mail exists.
Invitations reuse the same machinery: `POST /api/v1/user` with `send_invitation` creates the user
inactive and mails a link (purpose `invite`); `POST /api/v1/invitation/accept` sets the password,
activates and verifies in one guarded UPDATE (pending invitations only), and the user signs in
normally. Social login (`authn.Service.SocialLogin`) only swaps the password check for a provider check:
a driver in `internal/authn/social` returns the provider-verified e-mail, then the password
login's account checks and token apply. Add a provider as one file there (OIDC:
`newOIDCProvider`, like `google.go`; code exchange: `exchangeCode`, like `github.go`), its
`SOCIAL_<NAME>_*` settings in `config/social.go` and a case in `social.New`; never trust an e-mail
the provider has not verified (`docs/guides/social-login.md`). Every login is an `auth.login`
activity entry, reported through `authn.LoginAudit`.

**Who made a request, outside the chain** — middleware that wraps authentication (access log,
telemetry) cannot see context values added inside it. They read `httpx.RequestInfoFrom(ctx)`,
which `authn.Authenticate` (`httpx.NoteUser`) and `tenancy` (`httpx.NoteScope`) fill in.

**Activity log** — services call `s.activity.Record(ctx, activity.Entry{TenantID, Action:
"branch.created", Subject: "branch", SubjectID, Changes})` after a change succeeds. A nil recorder
records nothing; actor, IP and request id come from the context; secret-looking keys are redacted.
Read at `GET /api/v1/activity` (`activity.view`: own tenant; `activity.view.all`: every tenant
and the system log, the rows with no tenant). Entries written while acting as a user carry the
impersonator; act-as itself is logged as `impersonation.*` system rows.

**MCP tools** — an OpenAPI operation marked `x-mcp: {tool: name}` is an MCP tool; no Go code.
`internal/mcp` serves it through the `/api/v1` router in-process (identity via
`authn.Dispatched`, never the token), so every gate applies. MCP access tokens
(`Identity.IsMCP()`) work only on `/mcp`; `authn.Authenticate` refuses them elsewhere. The OAuth
server is `internal/modules/oauth` (CIMD clients or registered ones). Don't add MCP-only
endpoints: add the REST operation, then mark it.

**Pagination** — `page`/`per_page` (default 10, max 100) via `httpx.PageParams`, whitelisted
ordering via `httpx.OrderParams`, `h.Paginated(...)` with `httpx.NewPagination`. Feeds
(notifications, activity, dashboard streams) use `limit` + opaque `cursor` and `h.Cursor(...)`.

**Delivery** — anything that contacts a person calls `platform.Delivery.Send(ctx, delivery.Message{
To: delivery.Recipient{...}, Template: "name", Data: {...}, Channels: [...]})`: never a mail or SMS
driver directly. Templates are folders (`subject.txt`, `mail.txt`, optional `mail.html`, `sms.txt`,
`push.json`, `sample.json`; `mail.bn.txt` for Bangla) registered with
`platform.Delivery.Templates().Register(fsys)`; a missing data key fails at `Send`. One
`delivery.send` job per channel; the `deliveries` log keeps status and a masked address, never a
body. `authn` reaches it through `authn.Deliverer` (adapter in `internal/app/delivery.go`: delivery →
queue → tenancy → authn would be a cycle). `ims delivery:test` sends one now.

**Notifications** — register an event type with `notifications.RegisterEvent(name, Event{Channels,
Mandatory})`; a notification with `channels` (`mail`, `sms`, `push`) is delivered through the
delivery system to the users fan-out newly addresses (retry-safe), honouring per-user preferences,
verified addresses, the plan (`sms.notifications`) and `SMS_DAILY_LIMIT_PER_TENANT`.

**Live updates** — after a write the app should see at once, a module publishes a nudge:
`s.publisher.Publish(ctx, pubsub.ToUsers(RealtimeKind, ids...))` (or `pubsub.ToTenant`), wired with
`WithPublisher(...)` in `platform.go`. Any process may publish (it is `pg_notify`); `ims serve`
streams it at `GET /api/v1/realtime/stream` and the app refetches. Events carry no data, publish
after the write succeeds, never fail the write on a publish error, and add the kind to
`invalidates` in `web/app/src/components/realtime.ts` (`docs/guides/realtime.md`).

**Webhooks** — `internal/modules/webhooks` delivers activity-log entries to tenants' endpoints
(it is the `activity.Recorder` sink). Make an action deliverable with
`webhooks.RegisterEvent("invoice.paid", "…")` where it is recorded; payloads carry the redacted
changes, signed per Standard Webhooks, retried by the job itself (`retrySchedule`), with an SSRF-safe
client. `webhook.manage` + plan `api.access`.

**Tracing** — `internal/platform/tracing` (OpenTelemetry, on with `OTEL_EXPORTER_OTLP_ENDPOINT`)
already spans every request, query, job, outbound call (S3 included) and Redis command. **Every
exported service write opens a span**: a named `err` result, then
`ctx, end := tracing.Begin(ctx, "<module>.<operation>", attribute.Int64("tenant.id", ...))` and
`defer end(&err)`; `tracing.Annotate` adds ids found part way. Reads and per-request checks open
none. Ids and counts as attributes, never personal data. `TestServiceWritesOpenASpan` fails on a
write without a span (or list it in `unspannedOperations` with the reason). Metrics stay Prometheus.

**Duplicate requests** — writes behind sign-in pass `internal/platform/dedupe` (guard + optional
`Idempotency-Key`); handlers need nothing. Name a route in `dedupeOptions` (`internal/app/routes.go`)
to skip it (harmless repeats) or give it a longer window (money).

**Cache** — depend on `cache.Store`. Keys are owned by the module that caches; include the tenant
id in tenant-derived keys and delete the key in the service method that writes the data. Default
driver: `memory` locally, `database` elsewhere.

**Queue / Scheduler** — jobs are `queue.Definition{Kind, Queue, Validate, Handle}`; prefer
`EnqueueTx`. Scheduled tasks are listed in `internal/app/schedule.go`
(`{ID, Spec, Description, Timeout, EvenInMaintenance, Run}`; overlap is skipped by default, and
every task not marked `EvenInMaintenance` is skipped while the app is down); one scheduler process at a time (advisory lock).

**Settings & feature flags** — settings: typed catalog (`modules/settings/registry.go`), tenant →
global → default, groups + order editable in the dashboard. Flags: catalog in
`modules/featureflags/registry.go`, user → tenant → global → default, optional percentage rollout
(stable per-user or per-tenant buckets); gate routes with `featureflags.Require`.

**Storage** — code names a disk (`FILES_DISK=r2`); disks are declared in config
(`private`, `public`, `s3`, plus `STORAGE_DISKS=...`). Visibility, not the name, decides whether a
file gets a URL; private files go through the authorized download route.

**Billing** — `service.Guard` resolves features/limits (overrides → package); resource services
read the limit with `LimitChecker.QuotaFor` before `WithTenantLock`, then count and compare inside it. Gateway callbacks
are always confirmed with the gateway. Staff move a tenant onto a plan with `SubscriptionService.Assign`
(`POST /billing/internal/tenants/{tenant_id}/subscription`); an override set to `null` is removed.
Billing management belongs in the product's admin app, not the developer dashboard.

## Database workflow (critical rules — details in the `db-change` skill)

1. Never edit `internal/dbgen` by hand; change `sql/queries/` or `migrations/` and `make sqlc`.
2. Goose migrations are the only schema source.
3. Explicit-id seeding needs `SyncIDSequence` (tenants/branches repositories; used by the seeder).
4. Soft deletes: partial unique indexes (`WHERE deleted_at IS NULL`); reads filter `deleted_at`.
5. Map `pgtype.Timestamptz` to `time.Time`/`*time.Time` in `model.go`.
6. River owns its tables (`ims migrate`).

## Testing conventions

- Unit tests: stdlib `testing`, table-driven, fakes for repository interfaces. No mock frameworks.
- **Integration tests use `internal/testkit`** in the package's external test package
  (`package x_test`): `app := testkit.New(t)`, `app.User(t, testkit.Role(...), testkit.InTenant(...))`,
  `app.As(user).Post(path, body).Status(t, 201).Data(t, &v)`. Tests skip without
  `DATABASE_TEST_DSN`. `dbtest.Queries/Pool/Clean` remain for repository-level tests.
- MCP tools: `testkit.New(t, testkit.WithMCP())`, then `app.Agent(t, user, testkit.AllowChanges())`
  signs in through the real OAuth flow; `agent.Call(t, "create_x", args).OK(t).Data(t, &v)` or
  `.Refused(t, "INSUFFICIENT_PERMISSIONS")`. Test a tool next to its REST test, isolation included
  (`internal/modules/branches/testkit_example_test.go`).
- `internal/app/route_permissions_integration_test.go` asserts every protected route answers 403 to
  a user without permissions — new routes must be protected or allowlisted there.
- Cache drivers share the `cachetest` contract suite; gateway HTTP is tested against
  `httptest.Server` stubs; CLI commands get a smoke test through `BuildRoot`.

## Design decisions (deliberate)

Keep this list accurate; add to it when you make a non-obvious choice (also update
`docs/architecture/decisions.md`).

- Explicit query scoping (SQL parameters) instead of ORM global scopes; no Postgres RLS.
- Permission checks on routes/handlers (Laravel `can:` / controller style); services enforce
  tenancy + invariants. CLI/jobs are trusted callers.
- Tenant/audit stamping in services, not model observers. Activity is recorded explicitly.
- `tenancy.Filter{}` fails closed; all-tenant reads are explicit (`tenancy.Unscoped()`).
- Nobody changes their own roles; the Super-Admin cannot be stripped or deleted.
- Quota checks (users.max, branches.max) run under a per-tenant advisory lock.
- Billing callbacks are confirmed with the gateway; subscribe is idempotent per tenant.
- Error responses are unified RFC 9457.
- OTP and recovery tokens are cache-only.
- Backups are local-first with optional S3/R2 copies.
- Dashboard: handlers call services (`dev/services.go`), mutations need `dev.manage` on the route,
  every mutation and dashboard sign-in is audited (`dev_actions`, with IP and browser).
- A suspended tenant (`is_active = false`) is refused in `tenancy.Resolve` / `tenancy.Exempt`
  (`403 TENANT_SUSPENDED`) and at sign-in, from the per-request user lookup (`Identity.TenantActive`,
  no cache); logout stays open and platform accounts can still act inside it. Billing suspends lapsed
  tenants only with `BILLING_SUSPEND_ON_EXPIRY` (`docs/guides/tenancy.md#suspending-a-tenant`).
- Act as a user per request (`X-Acting-User`), not with login-as tokens; act-as is written to the
  system audit log (start is fail-closed, every request is logged).
- MCP tools are REST operations marked `x-mcp`, served through the `/api/v1` router (no MCP-only
  endpoints, no second authorization path). The API is its own OAuth server for the connector
  (CIMD or registered clients; no DCR). MCP tokens work only on `/mcp`, session tokens never do.
- Social login signs in existing users by the provider-verified e-mail through the normal login
  checks and token: stateless (the client runs the provider flow), no link table, no OTP step.
  Logins (password and social) are recorded in the activity log via `authn.LoginAudit`.
- Two-factor reuses the sign-in's `OTP_REQUIRED` step (one field takes an app code, a mailed code
  or a recovery code), bound to the password step by a cached marker so a code alone never signs
  in; TOTP is RFC 6238 on the standard library (no dependency, the client renders the QR).
- Webhooks come from the activity log (a `Recorder` sink), so every module that records activity can
  be a source; only registered actions are deliverable. The delivery job schedules its own retries
  (eight attempts over a day) instead of the queue's backoff, and the HTTP client checks the address
  it connects to (no private/metadata addresses, no redirects) unless `WEBHOOK_ALLOW_PRIVATE`.
- Duplicate-request protection is server-side by default (a cache lock on caller + method + path +
  body, kept a few seconds after success) with the client's `Idempotency-Key` as an optional,
  stronger layer; both fail open when the cache is down.
- One delivery system (`internal/platform/delivery`) for mail, SMS and push; mail and SMS drivers
  are transports behind it. Templates render at `Send` (to validate) and again in the job (so a
  fix applies to retries); the log never stores bodies or data (`docs/guides/delivery.md`).
- Maintenance mode is one database row every process reads through a cached `Checker`
  (`MAINTENANCE_CHECK_INTERVAL`); `/up`, `/api/health`, `/metrics`, `/dev` and `/docs` stay open,
  the bypass is a hashed secret in `X-Maintenance-Bypass`, scheduled tasks skip unless
  `EvenInMaintenance`, queue workers keep running (`docs/guides/maintenance-mode.md`).
- The dashboard is plain CSS on design tokens (`web/dev/src/styles/tokens.css`): themes, accent
  presets and density change there; `npm run check:style` enforces WCAG AA, the type scale and no
  raw colours. Pages use the kit in `components/ui.tsx` and the list in `components/navigation.ts`.
- Scheduled runs are rows in `schedule_runs` (`SCHEDULE_HISTORY_DAYS`), not cache keys, so the
  history works with any cache driver; a task that runs too often to log sets `NoHistory`
  (the heartbeat).
- Live updates are nudges (kind + audience, no data) over Postgres `LISTEN/NOTIFY`, streamed as
  SSE read with `fetch` (bearer header, never a URL token); one `LISTEN` connection per `ims serve`,
  any process publishes, polling stays as the fallback (`docs/guides/realtime.md`).
- Erasure tombstones the `users` row (personal columns blanked, `erased_at` set) instead of
  deleting it. **A table that stores a user id or personal data needs a `PrivacyContributor`
  in its module** (`privacy.go`: `Columns`, `Export`, `Erase`), registered in
  `internal/app/privacy.go` (identity last), or an exemption there with the reason;
  `internal/modules/privacy` `TestCoverage` and `ims privacy:coverage` fail otherwise. Audit rows
  are scrubbed, not deleted; exports never contain secrets
  (`docs/design/account-deletion-and-export.md`).

## When a task is ambiguous

1. Check `docs/architecture/decisions.md` and the matching guide in `docs/guides/`.
2. Prefer the established pattern in this codebase over inventing a new one — the reference
   modules are `branches` (small), `identity` (rich) and `billing` (big).
3. Record a new non-obvious decision in `docs/architecture/decisions.md` and in the list above.
