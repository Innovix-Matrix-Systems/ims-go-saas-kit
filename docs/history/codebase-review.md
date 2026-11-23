# Codebase Review — IMS Go Admin Starter

> **Historical snapshot.** This review describes the code at commit `1f55eeb`. Its findings were
> fixed afterwards (see `CHANGELOG.md` for what changed); file paths
> and line numbers below refer to that commit, not to the current code.

**Reviewed:** commit `1f55eeb` ("Update ci.yml"), working tree clean
**Scope:** entire repository — Go backend (314 files), SQL layer (17 migrations / 18 query files / 243 named queries), OpenAPI contract, developer-dashboard SPA (50 files), CI/CD, deployment scripts, and all documentation.
**Method:** static review of every package in 9 parallel deep-dive passes, plus objective verification (build, vet, gofmt, lint, unit tests, statement coverage) and manual end-to-end verification of every Critical and High security claim against the source. **Second pass:** every Critical and High finding was independently re-verified line-by-line against the source (route → middleware → permission catalog → default role → service → SQL), with preconditions and mitigations traced; several claims were corrected, sharpened or downgraded — see §7b for the verification log.

---

## 1. Executive summary

This is an unusually well-organised codebase for its size, built around a clear and unusually
well-articulated vision. It is not a toy starter: the layering is disciplined, the error contract
is real, the contract is enforced in CI, and the infrastructure layer (migrations, transactions,
cache contract suite, queue envelope, telemetry backpressure) is genuinely production-shaped.

The problem is that **the product's headline promise — multi-tenant isolation — is not
actually enforced**, and it fails in a way that is reachable by the *default seeded role* of
every tenant. Three independent, verified chains let a normal tenant administrator delete
users in other tenants, tamper with the role assignments of any user — including stripping the
platform Super-Admin's roles entirely — and read every tenant's notifications and uploaded
files. The project's own documentation is emphatic that "a query that forgets the
filter is a security bug — not a style issue", and the `branches` module demonstrates exactly
how it should be done — but `identity`, `notifications` and `files` do not follow it, and the
`tenancy.Exempt()` middleware hands them a blanket "internal" scope to any authenticated caller.

Everything else is a smaller, more tractable set of problems: production safety is *documented*
rather than *enforced* (the install script seeds the dev-oriented `.env` template with only a
"review it before starting" note, `db:seed` creates a super-admin with the password `123456`
and no production guard, access tokens never expire by default), the public billing callback
can fail a pending payment without contacting the gateway — and for cash payments the lookup
key is a guessable timestamp, so this works cross-tenant — and the system is architecturally
single-node (the documented cache-invalidation mechanism (`cachekeys`) is dead code, the
scheduler has no cross-process coordination, and the default cache driver is per-process).
Two of the reported risks are deliberate, documented trade-offs (fail-open rate limiting,
single-scheduler topology) and are re-graded accordingly in §11.

| | Score | Note |
|---|---|---|
| **Overall** | **6.6 / 10** | Well-built foundation; the core isolation boundary is not yet trustworthy. |
| **Code quality & architecture** | **7.6 / 10** | Above average for a starter; the layering discipline is real and consistently applied. |
| **Security** | **4.5 / 10** | Strong primitives (bcrypt, `crypto/rand`, constant-time compare, no injection) undermined by three exploitable privilege-escalation chains. |
| **Multi-tenant isolation** | **4.5 / 10** | The flagship feature. `branches` is exemplary; `identity`, `notifications`, `files` are not. |
| **Scalability** | **5.5 / 10** | Fine for one node with modest load; horizontal replicas are unsafe with the default configuration. |
| **Testing** | **5.5 / 10** | 21.8 % statement coverage hand-written, ~50 % of packages at 0 %, no frontend tests, and the tests that would have caught C1–C3 do not exist. |
| **Docs & DX** | **7.5 / 10** | Exceptionally good intent; ~12 concrete statements are now factually wrong about the code. |

**Bottom line:** this is a strong *engineering foundation* with an *unfinished security model*.
The defects are not deep architectural problems — they are missing guards on three routes and
two missing columns in a schema. Fixing P0 (estimated 2–3 working days) converts this from
"unsafe to expose to tenants" to "credible multi-tenant starter". The longer-horizon work
(horizontal scaling, frontend tests, coverage) is normal evolution, not remediation.

---

## 2. The vision, decoded

Read from `AGENTS.md`, `docs/architecture.md`, `README.md` and the 13 phase specs, the project
wants to be:

> **A batteries-included, multi-tenant SaaS API starter that ships as one CGO-free binary** —
> HTTP API, migrations, queue workers, cron scheduler, admin commands and an embedded
> Telescope-style developer dashboard, deployed with systemd and no containers.

The pattern it commits to is unusually specific, and the code mostly honours it:

- **Feature-first modules** with an identical file skeleton (`routes.go` → `handler.go` →
  `request.go` → `service.go` → `repository.go` → `model.go`), with `domain/` + `service/`
  sub-packages only for the large billing module.
- **Explicit dependency injection.** No `init()` side effects anywhere in `internal/`; every
  dependency arrives through a `Platform`/`Dependencies` struct. This is verified, not assumed.
- **One error universe.** Services return `*apperr.Error` with stable machine-readable codes;
  handlers render RFC 9457 `problem+json` through a single `Responder`. No handler writes raw
  JSON errors.
- **Explicit tenancy.** `tenancy.Scope` is passed from handler → service → repository as
  explicit ids. No ORM global scopes, no ambient state, no RLS — a deliberate, documented
  decision with the trade-off spelled out.
- **Contract as a build artefact.** `api/openapi.yaml` is canonical, served live at `/docs`,
  diff-gated in CI, and cross-checked bidirectionally against the chi route table.
- **Generated code is never hand-edited.** sqlc and oapi-codegen output are diff-gated in CI;
  a module generator (`ims gen module`) scaffolds the whole pattern.
- **Everything is callable from CLI, jobs and scheduled tasks** — services have no `http.Request`
  below the handler layer.

What is *not* yet true: the isolation boundary the vision rests on. The design is right; the
guards are missing in three places.

---

## 3. Objective measurements

| Metric | Value |
|---|---|
| Go files | 314 (74 test files) |
| Production Go lines | 49,097 |
| — generated (`dbgen` + `contract`) | 13,329 (27 %) |
| — **hand-written production** | **35,768** |
| Test lines | 10,671 (30 % of hand-written production) |
| Hand-written statement coverage | **21.8 %** (2,935 / 13,465) |
| Total statement coverage incl. generated | 19.5 % |
| Database | 31 tables, 17 goose migrations, 18 query files, 243 named queries |
| Contract | `api/openapi.yaml` 5,945 lines; `internal/contract` 313 types |
| Frontend | 50 files; SPA dist ~775 KB uncompressed, 287 KB largest chunk |
| Git history | 7 commits |

### Verification results (this review)

| Check | Result |
|---|---|
| `go build ./...` | **PASS** |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (0 files) |
| `golangci-lint run ./...` | **PASS** (exit 0) |
| `go test -count=1 -p 2 ./...` | **PASS** (all packages green) |
| Integration tests | **SKIPPED** — no Postgres available on 127.0.0.1:5432; every DSN-gated test skipped, so the 21.8 % figure understates CI coverage |
| `PLAN.md` referenced by `specs/README.md` | **MISSING** from the repository |

### Coverage by area (unit tests only, no database)

| Strong | Weak / zero |
|---|---|
| `internal/app` 63.5 % (17 test files, e2e) | `internal/modules/identity` **0 %** |
| `internal/platform/i18n` 75.7 % | `internal/modules/settings` **0 %** |
| `internal/platform/httpx` 56.6 % | `internal/modules/featureflags` **0 %** |
| `internal/platform/cache/memory` 81.7 % | `internal/modules/billing/service` **0 %** |
| `internal/platform/cache/file` 72.7 % | `internal/modules/billing/domain` **0 %** |
| `internal/platform/webui` 90.9 % | `internal/modules/files` **0 %** |
| `internal/scaffold` 86.0 % | `internal/modules/tenants` **0 %** |
| `internal/platform/featureflags` 87.5 % | `internal/platform/health` **0 %**, `metrics` **0 %** |
| `internal/platform/scheduler` 69.1 % | `internal/platform/logging` **0 %**, `seeder` **0 %**, `apperr` **0 %** |
| `internal/platform/ratelimit` 58.6 % | `internal/platform/cache` (wrapper) **0 %**, `database` (wrapper) **0 %** |

Note: many of the 0 % packages are thin delegating wrappers whose behaviour *is* exercised from
`internal/app`'s e2e tests. The real gap is that **`identity`, `settings`, `featureflags`,
`billing/service` and `files` have no package-level tests of their own**, and `authn`,
`authz` and `tenancy` have very little (48.5 %, 12.2 %, 11.3 %).

---

## 4. What is genuinely excellent

These are not filler. They are things many production Go codebases do not get right.

1. **No hidden globals, no `init()` side effects.** Every dependency is threaded explicitly
   through `Platform`/`Dependencies` (`internal/app/platform.go:41`, `internal/app/wiring.go:44`).
   The composition root is readable end to end.
2. **RFC 9457 implemented properly, not approximated.** `Problem` carries
   `type/title/status/detail/instance` plus `error_code`, `timestamp`, `errors`, `meta`,
   `retry_after`; `trace_id` is emitted **only** for ≥ 500 (`internal/platform/httpx/problem.go:89-94`);
   5xx details are masked outside production in the responder, not per-call-site
   (`httpx/problem.go:147-149`), with a regression test asserting no leakage
   (`internal/app/contract_test.go:111-148`).
3. **Correct, verified RFC 9457 in the contract.** 12 shared problem responses are aliased and
   reused (`api/openapi.yaml:3898-3910`); six representative responses are validated against the
   spec schemas including `application/problem+json` content types (`internal/app/contract_test.go:63-106`).
4. **Bidirectional contract drift detection.** `RouteDrift` computes a symmetric difference and
   the test asserts both directions (`internal/app/contract.go:42-107`,
   `internal/app/contract_test.go:42-58`). It runs in the DB-less CI job.
5. **Fail-closed nullable-parameter SQL pattern.** `@all_tenants::boolean OR tenant_id = @tenant_id`
   with a nullable parameter yields *no rows*, not *all rows* — the failure mode is empty, not
   everything (`internal/dbgen/users.sql.go:225`).
6. **Fail-closed proxy trust.** `X-Forwarded-For` is honoured only when the immediate peer is in
   `HTTP_TRUSTED_PROXIES`, walked right-to-left (`internal/platform/httpx/realip.go:27-76`),
   with a four-case unit test matrix. The default empty list disables spoofing entirely.
7. **Migrations are concurrency-safe and reversible.** A dedicated advisory-lock connection
   serialises concurrent runners (`internal/platform/database/migrate.go:221-239`); goose and River
   migrate in one entry point; all 17 migrations carry `-- +goose Down`; rollback→re-apply is
   integration-tested.
8. **Transaction handling is production-grade.** Rollback runs on a context detached from
   cancellation with a 5 s bound, so a timed-out request still releases locks
   (`internal/platform/database/tx.go:44-49`).
9. **One cache contract suite for four drivers** (`internal/platform/cache/cachetest/cachetest.go:20`),
   covering TTL, TTL-preserving `Increment`, `SetNX`, `Clear` and `Remember`.
10. **Queue envelope design.** A single River kind carries `{kind,payload,tenant_id,branch_id}`;
    tenancy is restored before the handler; unknown kinds and invalid payloads are
    `river.JobCancel`-ed rather than retried forever (`internal/platform/queue/river.go:50-78`);
    transactional enqueue is proven in an integration test.
11. **Telemetry has real backpressure.** Bounded channels, non-blocking push with a drop counter,
    batched flushes off the request path (`internal/platform/telemetry/collector.go:226-257`).
12. **Path traversal is neutralised** in both storage and the SPA
    (`internal/platform/storage/local.go:29-37`, `internal/platform/webui/webui.go:41-42`), with tests.
13. **No command injection anywhere.** `pg_dump` runs via `exec.CommandContext` with a fixed argv
    and a server-generated `--file` path; the password is passed via `PGPASSWORD`, never on the
    command line (`internal/modules/backup/dump.go:68-74,103-111`).
14. **Money is integer minor units and never client-controlled.** The charged amount always comes
    from the package row (`internal/modules/billing/service/subscription_service.go:147`);
    `SubscribeRequest` carries no amount field.
15. **Gateway success is re-verified, not trusted.** bKash callbacks re-query
    `/tokenized/checkout/execute` and require `statusCode == "0000" && transactionStatus == "Completed"`.
16. **`order_by` is whitelisted everywhere**, and the SQL encodes ordering as `CASE WHEN` so a bad
    value can never reach the parser — so it cannot become a SQL injection vector.
17. **The generator is deterministic and non-destructive.** Text splice (never a YAML round-trip),
    marker-based, kin-openapi validated before write, atomic write preserving permissions,
    with idempotency and hand-written-preservation tests.
18. **systemd units are genuinely hardened**: `User/Group`, `NoNewPrivileges`, `PrivateTmp`,
    `ProtectSystem=strict`, `ProtectHome`, scoped `ReadWritePaths`.
19. **The SPA has no XSS sinks.** Zero matches for `dangerouslySetInnerHTML` / `innerHTML` / `eval`;
    all server content is rendered through React text interpolation.
20. **`ims gen module` is a genuine onboarding force multiplier** — migration, sqlc queries,
    repository, service, handler, routes, tests and the OpenAPI region in one command.

---

## 5. Critical findings

All three chains below were verified by reading the source end to end. Each is reachable by the
**default tenant Administrator role** that `CreateDefaultRolesForTenant` provisions for every
tenant (`internal/modules/access/service.go:264-295`).

### C1 — Tenant Admin can delete users in any tenant (cross-tenant destructive)

**Chain (all verified):**

| Step | Location | Fact |
|---|---|---|
| 1 | `internal/tenancy/middleware.go:123-134` | `Exempt()` sets `Scope{Internal: true}` for **any authenticated caller** — it does not check that the caller is tenantless or a super-admin. |
| 2 | `internal/tenancy/context.go:62-64` | `AllTenants()` returns `s.Internal \|\| s.Acting == ActingGlobal` → **true** for any caller on an exempt route. |
| 3 | `internal/modules/identity/handler.go:590-607` | `DELETE /api/v1/internal/user/{id}` is gated **only** by `authz.PermUserDelete`. |
| 4 | `internal/authz/catalog.go:125` | `PermUserDelete` has **no `IsSystem: true`**. |
| 5 | `internal/authz/catalog.go:197-216` | `AdminTenantPermissions()` includes every non-system permission in groups `role`/`branch`/`user`/`setting` → `user.delete` is included. |
| 6 | `internal/modules/access/service.go:283-294` | `CreateDefaultRolesForTenant` grants exactly that set to every tenant's Admin role. |
| 7 | `internal/modules/identity/service.go:69` | `if !scope.AllTenants() && user.TenantIDValue() != scope.EffectiveTenantID()` → the tenant check is **skipped**. |
| 8 | `internal/modules/identity/service.go:268` → `sql/queries/users.sql` `SoftDeleteUser` | The soft-delete query filters on `id` only — **no `tenant_id`**. |

**Impact.** Any tenant's default Admin can soft-delete users in any other tenant, and the
service additionally calls `s.auth.RevokeAll` (`identity/service.go:271`), killing their sessions.
Only Super-Admins are protected (`identity/service.go:261-267`).

**Fix.** Two layers, both needed:
- Mark `user.delete`, `user.role.assign`, `notification.*` and `file.*` as `IsSystem: true` and
  add dedicated `user.internal.*` variants — the pattern **already exists** in the catalogue
  (`user.internal.create`, `user.internal.update`, `user.internal.export`). This is the primary
  fix and is sufficient on its own.
- Defence in depth: add an internal-identity gate to the exempt group (see C4) so a scope of
  "internal" can no longer be self-asserted — applied per route family, since the exempt group
  also carries shared profile handlers and the `files` module.

---

### C2 — Tenant Admin can rewrite the roles of users in any tenant

**Chain:** `POST /api/v1/internal/user/assign-role` (`internal/modules/identity/handler.go:610-632`)
is gated only by `PermUserRoleAssign` — also non-system (`internal/authz/catalog.go:126`) and
therefore also in `AdminTenantPermissions()`.

`Service.AssignRoles` (`internal/modules/identity/service.go:279-299`):
- `s.Get(ctx, scope, targetID)` — the tenant check is skipped (C1 step 7);
- **an empty `roleIDs` list skips validation entirely** (`validateRoles` returns `nil` for
  `len == 0`, `service.go:463-466`), then `ClearRoles` runs unconditionally — so the attacker
  can strip **all** roles from any user, including the platform Super-Admin (which has no
  `IsSuperAdmin` guard on this path);
- with a non-empty list, `validateRoles(ctx, target.TenantIDValue(), roleIDs)` validates against
  the **target's** tenant, so the attacker can grant any role of the target's tenant to the
  target — privilege escalation *within the victim tenant*, executed by an outsider;
- with a tenantless (internal) target, `scope.Internal` routes to `validateSystemRoles`
  (`service.go:284-287`), so the attacker can grant **system roles** (including Super-Admin) to
  any internal user — though not to themselves (their own tenant is non-zero, so the system-role
  path never applies to a self-attack, and `RoleExistsForTenant` rejects the Super-Admin role
  for a tenant).

**Impact (corrected on second pass).** This is *not* a self-service route to Super-Admin —
the report's first-pass wording ("full cross-tenant account-takeover primitive") overstated it.
What is confirmed: (a) strip every role from any user in any tenant, including the Super-Admin
account — a platform-wide denial of service with no audit trail beyond `updated_by`; (b) grant
any of a victim tenant's roles to any of that tenant's users; (c) grant system roles to internal
(tenantless) users. **Fix.** Require `scope.Internal` to be *proven* (see C4), require a
dedicated system permission on the internal route, reject empty `roleIDs`, and add an
`IsSuperAdmin` guard mirroring `Delete`/`ChangePassword`.

---

### C3 — Tenant Admin can self-grant cross-tenant read permissions and read every tenant's data

**Chain:**

| Step | Location | Fact |
|---|---|---|
| 1 | `internal/modules/access/routes.go:12` | `POST /api/v1/role/assign-permission` requires only `PermRoleUpdate` — non-system, group `role` → **in `AdminTenantPermissions()`**. |
| 2 | `internal/modules/access/service.go:234-240` | `replacePermissions(..., stripSystem: true)` strips only `IsSystem` permissions. `notification.view.all` (`authz/catalog.go:168`) and `file.view.all` (`authz/catalog.go:136`) are **not** system → grantable. |
| 3 | `internal/modules/notifications/routes.go:21` | `GET /api/v1/internal/notification` sits in the exempt group → `Scope{Internal:true}` → `AllTenants() == true`. |
| 4 | `internal/modules/notifications/handler.go:127` | Requires `PermNotificationViewAll` — now granted. |
| 5 | `internal/modules/notifications/service.go:138-148` → `sql/queries/notifications.sql:30-39` | `ListNotifications` is `SELECT * FROM notifications WHERE (...)` — **no `tenant_id` filter at all**. |
| 6 | `sql/queries/files.sql:23` + `migrations/00011_create_files.sql:2-23` | The `files` table has **no `tenant_id` column**, and `ListFiles` filters only by type/disk. `GET /api/v1/files` is exempt and gated by `file.view.all`. |

**Impact.** A tenant Admin escalates to reading **every notification and every uploaded file of
every tenant on the system**, including customer email content and uploaded documents. There are
also `PATCH /internal/notification/{id}` and `DELETE /internal/notification/{id}` with the same
shape (`notifications/routes.go:24-25`) — cross-tenant modification and deletion.

---

### C4 — The tenant-exempt route group has no server-side concept of "internal user"

This is the root cause behind C1–C3 and the single highest-leverage fix. `AGENTS.md` describes
the exempt group as *"global/system management (tenants CRUD, permissions, internal users/roles,
packages/admin billing)"* — but the code has no way to distinguish a system operator from a
tenant administrator. `Scope.Internal` is a **request flag, not a proven identity**.

The codebase already knows the right answer elsewhere: `authz.PermUserTenantSwitch` is
`IsSystem: true`, `tenant.*` and `package.*` and `subscription.*` and `payment.*` permissions are
all `IsSystem: true` (`internal/authz/catalog.go:105-109,145-164`). The `user`, `notification`,
`file`, `branch`, `role` and `setting` groups are not — and those groups are exactly the ones
mounted on exempt routes.

**Recommended fix (one coherent change):**

1. **Primary:** in `authz/catalog.go`, mark every permission that gates a tenant-exempt route as
   `IsSystem: true`, or introduce `*.internal.*` variants (the existing convention). This alone
   closes C1–C3 without touching middleware semantics, because `AdminTenantPermissions()` and
   `replacePermissions(stripSystem: true)` both exclude system permissions.
2. **Defence in depth:** add an explicit internal-identity gate for the routes that are truly
   system-only. A blanket "reject tenant-scoped callers in `Exempt()`" needs a route-by-route
   check first — the exempt group deliberately carries shared handlers such as
   `/internal/user/profile/update` (the same handler as the resolved route) and the whole `files`
   module, so the correct rule is per route family, not global.
3. Add `tenant_id` to `files` and to the `notifications` list/get/update/delete queries.
4. Add an integration test that exercises **every** `/api/v1/internal/*` route with a
   tenant-scoped token and asserts 403. That test alone would have prevented C1–C3.

---

## 6. High-severity findings

### Security & production safety

| ID | Finding | Evidence | Impact / fix |
|---|---|---|---|
| H1 | **`APP_ENV` is the single production-hardening switch and is never validated** | `internal/config/config.go:305-307` — `IsProduction()` is a case-insensitive exact match (`EqualFold`) that does **not** trim, while `IsLocal()` trims, lowercases and accepts three aliases; `Validate()` never checks `APP_ENV` (only `Load` reads it, `:552`) | A value like `APP_ENV=production ` (trailing space) or `APP_ENV=prod` silently leaves the dev dashboard on (`:1031`), `/docs` on (`:1000`), 5xx details unmasked and text logs. Case is handled; whitespace and synonyms are not. **Fix:** validate against an allowlist and normalise (trim/lowercase) once at load. |
| H2 | **Production provisioning seeds the dev-oriented `.env` template with no enforced posture** | `deploy/scripts/install.sh:41-51` — copies `.env.example` to `/etc/ims/ims-go-admin-starter/.env` **only if absent**, with an explicit "review it before starting" note and "Edit … (DATABASE_DSN, APP_URL, keys, etc.)" as next step 1; nothing validates or rejects insecure values, and the template sets `APP_ENV=local`, `APP_DEBUG=true`, `AUTH_DEFAULT_PASSWORD=123456`, `AUTH_TOKEN_EXPIRATION=0`, `CACHE_DRIVER=memory`, `sslmode=disable` | An operator who skips the review step starts a "production" service with the dashboard, docs, per-process cache, non-expiring tokens and a known admin password — and the app boots happily. **Fix:** ship `deploy/.env.production`; refuse to start (or fail `config:show --strict`) while `APP_ENV=local` in a systemd context. |
| H3 | **`db:seed` has no production guard and creates publicly-known credentials** | `internal/cli/seed.go:59-93`, `internal/seeder/seeder.go:26-31,217,252` — hashes `cfg.Auth.DefaultPassword` (default `123456`) for `superadmin@ims.com` and `developer@ims.com` | The README's documented deploy step (`README.md:128`) creates a super-admin with a guessable password. **Fix:** refuse in production unless `--force` **and** a changed password; otherwise generate a random one and print it once. |
| H4 | **Access tokens never expire by default** | `internal/config/config.go:774` (`AUTH_TOKEN_EXPIRATION` defaults to `0`); `internal/authn/authenticator.go:66-72` only writes `expires_at` when > 0 | A stolen token is valid forever. **Fix:** non-zero default; reject `0` in production. |
| H5 | **Login enumerates accounts by status code, message and timing** | `internal/authn/service.go:55,61,135-146` — unknown email → 422 "The selected email is invalid."; wrong password → 401 "The provided password is incorrect."; `verifyBeforeLogin` runs *before* the password check; the unknown-user path skips bcrypt entirely | Reliable account enumeration and targeted password spraying. **Fix:** one uniform `INVALID_CREDENTIALS` response + compare against a dummy bcrypt hash. |
| H6 | **Login throttle is keyed `email\|IP`, so rotating IPs resets the budget** | `internal/authn/service.go:161-163`; the repo's own test asserts this behaviour (`internal/authn/service_test.go:165-167`); decay window is 1 minute | Per-account lockout is bypassable by IP rotation — password spraying against one account is limited only by the global per-IP limiter (which a distributed source pool also evades). **Fix:** two independent counters (per-account and per-IP); lock on either. |
| H7 | **`PATCH /user/{id}` performs role assignment under `user.update` only** | `internal/modules/identity/handler.go:216-224` → `internal/modules/identity/service.go:240-247` (clears + reassigns roles); no `id != actorID` guard | Any account holding only `user.update` can promote itself to tenant Admin. `Delete` *does* have a self-check (`service.go:258`), so the omission is inconsistent. **Fix:** strip `roles` from the update path, or require `PermUserRoleAssign` + forbid self-targeting. |
| H8 | **Public billing callback can mark a *pending* payment failed without contacting the gateway** | `internal/modules/billing/drivers.go:97-104` — bKash `HandleCallback` short-circuits on the caller-supplied `status` query parameter (anything ≠ `success` returns `IsSuccessful:false` with no gateway verification); `service/subscription_service.go:272-274` then writes `payments.status='failed'`; `gateway_ref` is echoed to the payer in the subscribe response | **Verified mitigations:** `HandleGatewayCallback` returns completed/refunded payments unchanged (`:249-251`), so only *pending* payments are mutable, and a later genuine success callback can still recover a wrongly-failed payment. **Confirmed impact:** the payer can fail their own in-flight bKash payment; for cash (H9) the ref is guessable, so *any* unauthenticated caller can fail *any* pending manual payment. **Fix:** never derive failure from the callback body; re-query the gateway. |
| H9 | **Manual-method callbacks are unauthenticated *and* keyed on a guessable reference** | `internal/modules/billing/drivers.go:238-240` — `CashDriver.HandleCallback` returns a zero-value result (documented "manual methods have no callbacks"), which `HandleGatewayCallback` treats as failure; the cash reference is `CASH-<unix-seconds>` (`drivers.go:215`) and `gateway_ref` has no unique index (`migrations/00009:56`), so `GetPaymentByGatewayRef … ORDER BY id DESC LIMIT 1` resolves whichever payment was created in that second | An unauthenticated attacker who iterates recent timestamps can mark **other tenants'** pending cash/bank payments failed (the method must match the payment's, `subscription_service.go:246-248`). Completed payments are protected. **Fix:** reject callbacks for non-gateway methods before touching payment state; make manual references high-entropy and unique. |
| H10 | **Gateway HTTP errors are indistinguishable from business rejections** | `internal/modules/billing/drivers.go:199-207` — `post` ignores `resp.StatusCode` | A bKash 5xx with a JSON body → `IsSuccessful=false` → the payment is **permanently marked failed although money was captured**. **Fix:** error on non-2xx; map 5xx to retryable. |
| H11 | **`POST /billing/subscribe` has no idempotency → double charge** | `internal/modules/billing/service/subscription_service.go:117-172` — each call creates a new pending subscription *and* a new checkout | A retry or double click leaves multiple pending subscriptions and multiple live bKash `paymentID`s, each of which can be paid. **Fix:** accept `Idempotency-Key`; reject a second in-flight checkout for the same tenant+package. |
| H12 | **Feature-limit enforcement is TOCTOU** | `internal/modules/identity/service.go:102-110`, `internal/modules/branches/service.go:96-104` — `COUNT` then `INSERT`, no lock, no transaction | Concurrent creates exceed `users.max` / `branches.max` — the quota that billing sells. **Fix:** conditional insert or serializable transaction with a tenant row lock. |
| H13 | **The seeded Developer role holds all 67 catalog permissions — by design** | `internal/seeder/seeder.go:28-31` documents the intent in-code ("holds the hidden Developer role (every catalog permission)"), and `:57-63` + `access/service.go:107-128` implement it | Not a bug — a deliberate design decision with a large blast radius: the "dashboard access" role is a platform super-user (all tenant *and* system permissions), and the dev API runs tenant-exempt, so a leaked developer token is total compromise. **Fix (design review):** grant only `dev.view`/`dev.manage` plus read-only admin views, or split dashboard access from data administration. |
| H14 | **`dev.view` alone can download a full `pg_dump` and read in-flight email bodies** | `internal/modules/dev/routes.go:59`; `internal/modules/backup/service.go:161-185`; `internal/platform/queue/admin.go:29-45` exposes raw job args, which for `mail.send` contain the recipient and body | A read-only developer role can exfiltrate the whole database and customer emails. **Fix:** require `dev.manage` (or a dedicated `backup.download`) for archive downloads; redact mail payloads. |
| H15 | **Telemetry persists raw SQL argument values when capture is on** | `internal/platform/telemetry/redact.go:131-141` — `Args` redacts only *map keys* (`Value`, `:71-92`); pgx supplies positional scalars, which pass through unredacted; `app/platform.go:88-96` gates capture on `cfg.DevDashboard.Enabled` (default `!IsProduction()`, `config.go:1031`) | Password hashes (e.g. `sql/queries/users.sql:63-71` passes the bcrypt hash positionally) and other credential-like values land in `dev_queries.args` — in every database backup, readable by any `dev.view` holder. Gated off in a correctly-set production, but enabled by default in dev and in any `APP_ENV` typo scenario (H1). **Fix:** redact by statement/column, or skip args for auth tables. |
| H16 | **File uploads have no private option at all** | `internal/config/config.go:467-471` — `FILES_DISK` accepts **only `public` or `s3`**; `private` is rejected with "local disks are not publicly addressable"; `:883` defaults to `public`; `internal/app/routes.go:51` serves `/storage/*` unauthenticated | Every uploaded file is permanently readable by anyone with the URL (public disk) or via S3. For a multi-tenant SaaS this is a design gap, not a misconfiguration: **tenant documents cannot be stored privately through this module**, and the `file.view`/`file.delete` permission checks govern only the metadata API. **Fix:** add `private` to `FILES_DISK` and serve downloads through an authorized handler (or S3 presigned URLs). |
| H17 | **SVG uploads are servable inline from the app origin → stored XSS on direct navigation** | `internal/modules/files/service.go:37` allows `svg`; `:276-283` only requires a sniffed `image/*` (mimetype detects `image/svg+xml`); `internal/platform/storage/static.go` sets `nosniff` + immutable caching but no `Content-Disposition: attachment` and no CSP exists anywhere (`httpx/middleware.go:180-192` omits CSP deliberately for the docs UI); the SPA keeps its bearer token in `localStorage` (`web/dev/src/api/client.ts:20-29`) | **Precondition:** the victim must navigate directly to the file URL (embedded `<img>` use is safe — scripts don't run there). If they do, the SVG executes same-origin and can read `localStorage`. The `immutable` cache header (UUID paths) keeps the payload cached indefinitely. **Fix:** drop `svg` from the allow-list and/or serve user uploads with `Content-Disposition: attachment` + a sandboxing CSP. |

### Availability, scalability & data integrity

| ID | Finding | Evidence | Impact / fix |
|---|---|---|---|
| H18 | **`/api/health` returns HTTP 200 even when the DB, cache or queue checks fail** | `internal/platform/health/handler.go:49-51` — no `WriteHeader`; `HasFailures` exists (`health/health.go:38-45`) but is never consulted | systemd/nginx/k8s readiness probes pass on a broken instance; traffic keeps flowing to it. **Fix:** return 503 when `results.HasFailures()`. |
| H19 | **`cachekeys` — the documented primary invalidation mechanism — is dead code** | `internal/platform/cachekeys/cachekeys.go:64`; **verified: zero references to `platform/cachekeys` anywhere in the repo**; `docs/architecture.md:110` calls it "the primary invalidation mechanism" | The *replacement* mechanism (ad-hoc keyed `Delete` calls, e.g. `authz/checker.go:113-141`, `tenancy/middleware.go:142-150`) is wired at every mutation site found, so nothing is broken today. The real cost is maintainability and drift risk: every new cache-owning module must re-invent key naming and remember every invalidation path, and nothing enforces namespace-level invalidation. **Re-graded Medium (maintainability/doc integrity). Fix:** wire `cachekeys` into the caching modules, or delete it and correct the docs. |
| H20 | **The memory cache driver is unbounded and is the default in every environment** | `internal/platform/cache/memory/memory.go:30` (map with no cap or LRU); entries with `ttl<=0` never expire (`:92-94`); `internal/config/config.go:674` | Unbounded RSS under key churn, plus per-replica divergence of permission/tenancy/rate-limit caches. **Fix:** LRU + a cap; default to `redis`/`database` outside local development. |
| H21 | **The `file` cache driver is unsafe for multi-process use** | `internal/platform/cache/file/file.go:78-101` (`SetNX` = read-then-write under a process-local mutex), `:145-179` (`Increment` = read-modify-write), `:128-142` (`Clear` deletes every `.json` in the directory); the process-local limitation is stated in the package's own header comment | With two processes sharing the directory, fixed-window counters lose updates → the rate limiter and login throttle under-count. Documented, but nothing prevents selecting it for a multi-process deployment. **Re-graded Medium (documented foot-gun). Fix:** `O_EXCL` lockfile for `SetNX`/`Increment`, prefix-aware `Clear`, and a config warning when `file` is combined with queue/scheduler modes. |
| H22 | **Uploads are buffered entirely in RAM** | `internal/platform/httpx/multipart.go:15` — `ParseMultipartForm(maxBytes)` uses the *total* limit as the in-memory threshold, so Go never spills to disk; the files module allows 100 MB (`internal/modules/files/service.go:27`) | Any user with `file.create` can pin ~100 MB of heap per concurrent request. **Fix:** small `maxMemory` (e.g. 8 MB) + `MaxBytesReader` at the total limit; stream to disk. |
| H23 | **Server `WriteTimeout` (30 s) < app request timeout (60 s) → the timeout 503 is unreachable** | `internal/config/config.go:580` vs `:589`; applied at `internal/app/lifecycle.go:19`; `httpx.Timeout` writes the 503 only *after* the handler returns (`httpx/middleware.go:166-171`) | Synchronous exports (`identity/handler.go:733-738`), backup downloads and long imports are killed mid-body with a broken connection instead of a clean `problem+json`. **Fix:** `WriteTimeout=0` or ≥ `RequestTimeout`; use `http.ResponseController.SetWriteDeadline`. |
| H24 | **The rate limiter fails open on cache errors and is global, IP-keyed, applied before auth** | `internal/platform/ratelimit/cache.go:26-30` — returns `true, 0` on any store error; **deliberate and documented in-code** (`:18-19` "fail open and rely on the cache driver's own observability"); `internal/app/routes.go:46` applies it before authentication to every route | An availability-over-strictness trade-off, not an accident — but the consequence stands: a cache outage disables rate limiting, and shared-NAT tenants / health probes share one pre-auth per-IP budget. **Re-graded Medium (design trade-off). Fix:** exempt health/metrics; add a user-keyed tier after auth; consider fail-closed for auth-adjacent routes only. |
| H25 | **The scheduler's overlap guard is process-local; no cross-process coordination** | `internal/platform/scheduler/runner.go:26,40,89-104` — an in-process `running` map; no advisory lock (the migrations path shows the pattern, `database/migrate.go:221-239`); `registry.go` documents "one scheduler process per deployment" | Within the documented single-scheduler topology this is correct, so it is a **horizontal-scaling constraint, not a bug** — but nothing *enforces* the constraint, and a second `ims schedule` process (HA attempt, operator error) silently double-executes `billing.expire-subscriptions`, `send-renewal-reminders` and `backup.run`. **Re-graded Medium. Fix:** take the migration-style advisory lock in `Runner.Start` (cheap, makes the constraint self-enforcing). |
| H26 | **Notification fan-out scales linearly in recipients with a fixed 5-minute job timeout** | `internal/modules/notifications/jobs.go:39,53` — `Timeout: 5 * time.Minute` for fan-out/decrement jobs; the fan-out materialises target user IDs and performs per-recipient cache operations (`service.go:300-318,454-467`); `dispatchDecrement` chunks enqueues (`:363-381`) | **First-pass wording ("cannot complete for large tenants") was an unmeasured extrapolation and is withdrawn.** Confirmed facts: work grows linearly with recipients under a fixed timeout, so there is a recipient count beyond which the job times out and retries (burning worker capacity); no scale test exists to locate it. **Fix:** compute counters in SQL, make the timeout configurable, and add a large-tenant scale test. Re-graded Medium. |
| H27 | **`payments.gateway_ref` has no index, yet the public callback resolves by it** | `migrations/00009_create_billing_tables.sql:68-70`; `sql/queries/billing.sql:139-140` | A seq scan per callback on a public route. **Fix:** `CREATE INDEX payments_gateway_ref_idx ON payments (gateway_ref) WHERE gateway_ref IS NOT NULL`. |
| H28 | **No `statement_timeout` on the pool** | `internal/platform/database/registry.go:73-78` sets only `MaxConns`/`MinConns`/`ConnectTimeout`; no `RuntimeParams` anywhere except `search_path` (`backup/dump.go:112`) | **Mitigation verified:** HTTP requests carry the `Timeout` middleware deadline into `pgx` via context (`httpx/middleware.go:163-165`), scheduler tasks and River jobs have their own timeouts, so cancellation does propagate on those paths. The gap is code without a deadline — CLI commands and any context-less call — where a runaway query pins one of the 20 pool connections until the client gives up. **Fix:** set `statement_timeout` + `idle_in_transaction_session_timeout` in `RuntimeParams` as a backstop. |
| H29 | **The deployment writes to a read-only path** | Units set `ProtectSystem=strict` with `WorkingDirectory=/opt/ims/ims-go-admin-starter/current` (`deploy/systemd/ims-go-admin-starter.service:11,22,24`), but the shipped env keeps `APP_DATA_DIR=./var`, `APP_STORAGE_DIR=./var/storage`, `APP_CACHE_DIR=./var/cache` (`.env.example:12-14`), resolved relative to the CWD (`config.go:558-560`); `install.sh` never repoints them, although `docs/runbooks/deploy.md:13` says writable state lives in `/var/lib/ims/ims-go-admin-starter` | Uploads, `BACKUP_DIR` (`config.go:936-939`) and the `file` cache driver fail with EROFS in production. **Fix:** `StateDirectory=` in the units; point the paths at it. |
| H30 | **No vulnerability, SAST, secret or dependency scanning in CI** | `.github/workflows/ci.yml` runs gofmt/vet/build/generated-code-drift/test(-race)/lint + a web job (typecheck/build/drift) + an integration job — no `govulncheck`, no `dependency-review-action`, no CodeQL/gitleaks, no coverage floor, no `concurrency` group; the linter is pinned to the EOL v1 line (`golangci-lint-action@v6` + `v1.64.8`, built via `install-mode: goinstall`) | Dependency and secret risk is unmonitored. (The first pass speculated the v1 linter might fail to build under Go 1.26 — withdrawn as unverified; it compiled and ran clean locally on Go 1.26.1.) **Fix:** add the scanners, move to golangci-lint v2, add `concurrency`. |
| H31 | **`release.sh` health-checks the wrong endpoint and never auto-rolls back** | `deploy/scripts/release.sh:31` — `HEALTH_URL` defaults to `http://127.0.0.1:8080/up` (the liveness probe, hard-coded `{"status":"ok"}`); the release flow is migrate → switch symlink → restart → `health_check` → prune (`:218-234`); `health_check` ends in `die` on failure (`:108`), and `do_rollback` is reachable **only** via the explicit `--rollback` flag (`main()`, `:244-249`) | A DB-broken release passes the health gate and stays live until a human intervenes; rollback is manual. (The first pass also claimed `npm ci` runs as root — plausible but unverified in this pass, so it is dropped.) **Fix:** default `HEALTH_URL` to `/api/health?fresh=1`; auto-revert `current` when the post-switch check fails. |

---

## 7. Medium findings

**Authorization & tenancy model**

- **M1** — Authorization lives in **handlers, not services**, contradicting `docs/developer-guide.md:57-59` ("in the *service* (so CLI/jobs are covered too)"). Every `MustCan` call is in a handler (`identity/handler.go:133`); no service calls it. Any CLI command or job that calls these services bypasses authorization entirely. `branches.Service.checker` is injected and **never used** (`internal/modules/branches/service.go:34,41`).
- **M2** — `GetUser` / `UpdateUser` / `SoftDeleteUser` / `UpdateUserPassword` have **no `tenant_id` filter** (`sql/queries/users.sql:1,73,86,91`); isolation is a post-fetch Go check (`identity/service.go:69`) — the exact pattern `AGENTS.md` forbids ("explicit scoping in SQL is the *only* isolation boundary"). The row is still fetched cross-tenant.
- **M3** — Users are created pre-verified (`EmailVerifiedAt: time.Now()` unconditionally, `identity/service.go:161`), so `AUTH_CHECK_EMAIL_VERIFIED` is a no-op for API-created users.
- **M4** — Imported users share the default password `123456` (`internal/modules/identity/exchange.go:451-453`).
- **M5** — Self-service password change does **not** revoke existing tokens (`identity/service.go:395-410`); the admin flow does (`:370`). A hijacked session survives a password reset.
- **M6** — `PATCH /user/{id}` and `PATCH /internal/user/{id}` are TOCTOU-guarded (fetch-then-update with no lock).
- **M7** — `sql/queries/roles.sql:33,42,57,60,65,68,71,76,99,105` — role mutations and `model_has_roles` writes filter on ids only, guarded by prior Go reads.

**Billing correctness**

- **M8** — No state-machine guard on status transitions: `ActivateSubscription` and `UpdatePaymentStatus` filter on `id` only (`sql/queries/billing.sql:90-99,150-158`), so a stale callback can reactivate a cancelled subscription.
- **M9** — Subscription activation + payment completion are three separate, non-atomic statements (`subscription_service.go:175-185,276-281`) → a mid-flight failure leaves money recorded with no active plan.
- **M10** — `ReplaceFeatures` deletes all rows then upserts them one at a time, outside a transaction (`service/package_service.go:62-66,79-83`) → a mid-loop failure leaves partial limits and no error surfaced.
- **M11** — `recordPayment` writes a completed payment row *before* `Activate` runs (`billing/handler.go:380-393`) → same non-atomic window.
- **M12** — Settings/flag cache read errors hard-fail with 500 instead of degrading to the database (`settings/service.go:527-546`, `featureflags/service.go:270-289`) — contradicts the documented `Remember` degradation (`AGENTS.md:210`).
- **M13** — The billing guard is uncached and resolves the plan twice per check (`service/guard.go:74-92,130-153`) → ~6 extra round trips on every user/branch write.
- **M14** — Cancel revokes access immediately, contradicting both the code comment and `docs/architecture.md:150-151` (the query filters `status IN ('active','trial')`, `sql/queries/billing.sql:50-55`).
- **M15** — `storage.mb` is catalogued as a limit but **never enforced** (`domain/features.go:22,43-50`) — a package can sell a quota that nothing checks.
- **M16** — `crypto/rand` failure is ignored when generating invoice numbers (`domain/invoice.go:12-13`) → identical numbers within a second → unique-index violation on `migrations/00009:68`.
- **M17** — `gateway_ref` is neither unique nor collision-free; `CASH-<unix>` collides within a second and the lookup takes the newest row (`sql/queries/billing.sql:139-140`).
- **M18** — bKash token caching has no single-flight and no invalidation (`drivers.go:145-170`) → a cold-cache stampede; a revoked token keeps failing for up to 55 minutes.

**HTTP contract & transport**

- **M19** — Role and permission list endpoints return a bare `{data:[…]}` with no `links`/`meta` and are unpaginated (`internal/modules/access/handler.go:89,313,368`) — inconsistent with every other list.
- **M20** — 429 responses omit `retry_after` from the problem body (`internal/platform/ratelimit/ratelimit.go:118-119`) — contradicts the documented extension set (`docs/architecture.md:164`).
- **M21** — The notification cursor is unsigned base64 JSON (`internal/modules/notifications/cursor.go:18-51`) → a caller can hand-craft any `(created_at, id)` position. Confidentiality is preserved by the visibility predicate, so this is feed-integrity only. **Fix:** HMAC it.
- **M22** — Offset pagination permits `page` up to 1,000,000 (`internal/platform/httpx/filters.go:15,35`) → an offset of 100 M → a full index scan from a single request.
- **M23** — `Recover` returns without writing when the response already started (`httpx/middleware.go:113-115`) → the client receives a truncated body under a 200 with no error signal. **Fix:** `http.ErrAbortHandler`.
- **M24** — `X-Request-ID` is trusted verbatim with no length/charset validation (`httpx/middleware.go:41-46`) → log and `trace_id` spoofing.
- **M25** — Every list endpoint pays a second `COUNT(*)` query (O(n) on large tables).
- **M26** — `i18n.Middleware` allocates a fresh go-i18n `Localizer` per request (`internal/platform/i18n/locales.go:47`) → cache one per locale.
- **M27** — The 503 from `httpx.Timeout` and the 429 from the rate limiter are rendered *outside* the `Locale` middleware (`internal/app/routes.go:46-47`) → English-only errors.
- **M28** — `pagination links` echo the client-supplied `Host` header (`httpx/response.go:205-224`).
- **M29** — `settings` int coercion truncates `1.9` → `1` and accepts out-of-range floats (`internal/modules/settings/registry.go:130-131`).

**Data layer & schema**

- **M30** — `notifications` is never purged: `expires_at` only hides rows from the feed; no scheduler task exists → four notification tables grow unboundedly.
- **M31** — The database cache driver never purges expired rows; `CacheDeleteExpired` (`sql/queries/cache.sql:25`) is referenced nowhere → the `cache` table grows monotonically.
- **M32** — `users.branch_id` / `active_branch_id` use `ON DELETE SET NULL` (`migrations/00003:5-6`) → deleting a branch silently strips user assignments. `branches.manager_id` has no FK.
- **M33** — No `(tenant_id, deleted_at)` composite indexes on `branches` (`00002:18`), `users` (`00003:27`), `roles` (`00005:15`) → soft-deleted rows stay in the btree and are filtered post-scan.
- **M34** — `ILIKE '%…%'` search with no trigram/GIN index → seq scan + sort on the hot list endpoints (`users.sql:14-18`, `branches.sql:12-15`, `notifications.sql:32`, `settings_groups.sql:46`, `dataprocessing.sql:27-31`).
- **M35** — The feed index is `(tenant_id, created_at)` but the feed orders `(created_at DESC, id DESC)` (`notifications.sql:168`) → filesort on ties.
- **M36** — Missing indexes: `subscriptions(tenant_id,status)`, `dev_requests(tenant_id)`, `dev_requests(user_id)`, `personal_access_tokens(expires_at)` (read by the prune scan), `files(created_at)`.
- **M37** — `migrations/00013:32` drops `settings.tenant_id`, destroying the FK to `tenants`; tenant-scoped settings/flags orphan on tenant delete with no cascade.
- **M38** — Missing audit columns on `settings`, `data_processing_jobs`, `personal_access_tokens`, `backup_runs` and all six `dev_*` tables.
- **M39** — Free-text status columns with no CHECK/enum: `subscriptions.status`, `payments.status`, `payment_approval_requests.status`, `notifications.priority`, `data_processing_jobs.status`. Only `backup_runs` uses CHECK constraints — the best-modelled table in the schema.
- **M40** — `payments.currency` is `text` with no scale column (`migrations/00009:55`); `users.is_active` is `smallint` while every other boolean is `boolean` (`migrations/00003:16`) → runtime casts.
- **M41** — No `CREATE INDEX CONCURRENTLY` anywhere; `ALTER TABLE … DROP COLUMN tenant_id` (`00013:32`) takes ACCESS EXCLUSIVE with no `lock_timeout` guard.
- **M42** — `migrations/00012` Down is a no-op — the permission DELETE is irreversible.
- **M43** — `ListUserIDsByRoleIDs` accepts **any global system role** for any tenant (`sql/queries/roles.sql:45-49`) → a ROLE-targeted notification fans out across all tenants. `ListUserIDsByBranch` is unbounded.

**Kernel, platform & operations**

- **M44** — `NewPlatform` leaks the telemetry collector's goroutines on every error path: the collector is created at `internal/app/platform.go:91-99` but `collector.Close()` appears only in the success cleanup (`:382`); the 17 duplicated `cacheCleanup(); dbRegistry.Close()` blocks (`:106-321`) never close it.
- **M45** — Every CLI mode except migrate/seed builds the *entire* platform; `queue:work` creates a **second** River engine on top of the one `NewPlatform` already created (`internal/cli/queue.go:50-73` vs `internal/app/platform.go:114`); `cache:clear` creates a second cache store (`internal/cli/cache.go:36-44`).
- **M46** — 16 near-identical `cfg.Validate() → NewPlatform → cleanup` blocks in the CLI, while `health:check`, `schedule:list` and `openapi:check` skip `Validate()` entirely.
- **M47** — River `prepare` never calls `def.Validate` (the sync engine does, `internal/platform/queue/sync.go:29-33`) → malformed payloads are persisted and rejected asynchronously.
- **M48** — Job envelopes store only `EffectiveTenantID`/`EffectiveBranchID` (`queue/river.go:262-266`); under `ActingGlobal` the tenant is `0` ("no filter") → a job enqueued under global impersonation executes **unscoped across tenants**.
- **M49** — `/metrics` and `?fresh=1` on `/api/health` are unauthenticated; `?fresh=1` re-runs every check including a `GROUP BY` over `river_job` (`internal/platform/queue/health.go:22-45`) → DB amplification against a struggling instance.
- **M50** — Telemetry copies request and response bodies on **every** request regardless of sampling (`internal/platform/telemetry/http.go:33-34` runs before the sample check) → ~128 KiB of copies per request at the defaults.
- **M51** — `POST /api/v1/role/assign-permission` is *tenant-resolved*, not exempt — it is the mechanism that turns a tenant Admin into a cross-tenant reader (C3).
- **M52** — SMTP has no I/O deadlines after dial (`internal/platform/mail/smtp.go:41-52`); S3 uploads set no `Content-Type` (`s3.go:48-60`); `localDisk.Put` ignores the `size` argument (`local.go:39`).
- **M53** — The dev module has **no service layer** — handlers talk straight to repositories and contain business logic (`internal/modules/dev/queue.go:159-180`, `telemetry_handlers.go:177-206`), breaking the documented `handler → service → repository` rule; dev authorization is a per-handler helper (`dev/base.go:75-86`) rather than route-level middleware, so a new mutating route that forgets `requireManage` is silently unprotected.
- **M54** — The dev audit trail is best-effort: insert errors are swallowed and the (possibly cancelled) request context is used (`internal/modules/dev/actions.go:36-44`).

**Generators, contract & deployment**

- **M55** — The scaffold generator does not validate `--name`, `--table` or `--package` (only field names, `internal/scaffold/scaffold.go:73-81`) → `--name=../../x` writes files outside the repo; `--table` injects arbitrary SQL into the generated migration (`internal/scaffold/templates.go:3-21`).
- **M56** — The generated `PATCH` overwrites every column (`templates.go:57-66`) → omitting an optional field nulls it.
- **M57** — `ims gen openapi --all` silently drops endpoints when a module fails to parse (`internal/cli/openapi_gen.go:195-198`) and then overwrites the region.
- **M58** — The generated service imports `authn` and calls `authn.IdentityFromContext` (`templates.go:292,398-407`), contradicting `docs/developer-guide.md:36` ("Must NOT … import `httpx` or `authn`") and breaking "services are callable from CLI/jobs".
- **M59** — `/dev/api/*` (~60 endpoints) is excluded from route-drift checking (`internal/app/contract.go:59`).
- **M60** — `internal/contract` (210 KB, 313 types) is generated but imported **nowhere** → no compile-time link between the spec and the handlers.
- **M61** — The spec documents an `X-Tenant-Code` header (`api/openapi.yaml:25,3892`) that no code reads.
- **M62** — `install.sh` creates `$BASE_DIR`/`$BASE_DIR/releases` owned by the service user (`deploy/scripts/install.sh:28`) → a compromised service process can replace the binary or repoint `current` and persist across restarts.
- **M63** — `.env.example` is missing six keys the config reads: `AUTH_CHECK_IS_ACTIVE`, `AUTH_CHECK_EMAIL_VERIFIED`, `AUTH_CHECK_PHONE_VERIFIED`, `DEFAULT_TENANT_EMAIL`, `DEFAULT_TENANT_PHONE`, `BKASH_CALLBACK_URL`.

**Frontend**

- **M64** — The SPA has **zero tests** — no vitest/jest/playwright, no test script in `package.json`; CI runs only typecheck/build/drift. The untested surface is exactly the API client, the token lifecycle, query keys and the command palette.
- **M65** — Sign-out never calls `POST /api/v1/logout` (`web/dev/src/api/client.ts:58-61`) → the server-side token stays valid after the user believes they signed out.
- **M66** — RFC 9457 bodies are flattened to one sentence (`client.ts:102-106`) → `errors`, `meta`, `retry_after` and `error_code` are discarded; `ApiError.code` is never rendered.
- **M67** — `retry: 1` retries 401/403/404/429 alike (`web/dev/src/query.ts:6`) → doubles requests on permission errors and hammers a rate-limited API.
- **M68** — No CSP on the SPA shell, and the bearer token lives in `localStorage` (`client.ts:3,18-33`; `httpx/middleware.go:180-192`; `webui/webui.go:81-96`).
- **M69** — `DataTable` sorts client-side over a cursor-paginated server list (`components/DataTable.tsx:22-30`) → implies a global order that is false.
- **M70** — Modals, confirm dialogs and drawers have no `role="dialog"`/`aria-modal`, no focus trap, no Escape handling and no focus restoration (`components/ui.tsx:344-370,411-445`).
- **M71** — Live polling re-fetches every accumulated page (`_app.logs.index.tsx:31`, `_app.requests.index.tsx:56`, `_app.queue.tsx:88`, `_app.sessions.tsx:74`) → N sequential requests every 5–10 s with unbounded page accumulation.
- **M72** — An unsandboxed `<iframe src={blobUrl}>` fallback for non-previewable files (`_app.files.tsx:828-832`) — latent, not currently reachable.
- **M73** — Large screens mix fetching, mutation and presentation (`settings.tsx` 1,036 lines, `files.tsx` 833, `flags.tsx` 610, `backups.tsx` 602, `sessions.tsx` 507) → per-screen hooks would halve them.
- **M74** — `web/dev/.tanstack/tmp/*` (5 files) are tracked build junk; three different dev-port defaults exist (`package.json:6` → 5173, `scripts/dev.mjs:9-11` → 8080/8081, `vite.config.ts:7` → 8080).

**Import / export**

- **M75** — Export builds the whole workbook in memory and copies it into a second buffer, paging with `OFFSET` (`internal/modules/identity/exchange.go:329-390`; `sql/queries/users.sql:150-160`) → memory blow-up on large tenants.
- **M76** — Import uses `csv.ReadAll` / `GetRows` with excelize's default 16 GB unzip limit (`exchange.go:560-581`) → a crafted 10 MB xlsx can OOM a worker.
- **M77** — No CSV/XLSX formula-injection guard for cells starting with `=`, `+`, `-`, `@`.

**Cross-cutting (second pass)**

- **M78** — The CORS default `Access-Control-Allow-Headers` value contains a stray `n,` token between `X-Acting-Branch` and `X-Branch-Id` (`internal/config/config.go`, CORS defaults) — a harmless-looking typo that ships a bogus header name to browsers.
- **M79** — **N+1 role lookups on the user list**: `identity/handler.go:89-91` calls `checker.RoleRefs` per listed user, each an uncached query (`authz/checker.go:92-105`) → a 100-row page costs 101 extra round trips; `per_page` becomes a DB-load multiplier on the heaviest list. **Fix:** batched `RoleRefsForUsers(ids []int64)` once per page.
- **M80** — **No optimistic concurrency on updates**: no version column, no `WHERE updated_at = @expected` predicates anywhere (`users.sql:73-84`, `roles.sql:33-42` are blind `UPDATE … WHERE id = @id`) → concurrent edits silently last-write-win. Related but distinct from the TOCTOU items (M6, H12): this is lost-update protection for normal concurrent editing, which none of the modules have. **Fix:** `version int` column (or `updated_at` predicate) + 409/422 on mismatch; pair with conditional inserts for quotas (H12).

---

## 7b. Second-pass verification log

Every Critical and High claim was re-verified line-by-line against the source in a second pass
(new sub-agent reviews were attempted but failed on an upstream quota error, so all
re-verification below was performed directly). Results:

| Claim | Verdict | Notes |
|---|---|---|
| C1 (cross-tenant user delete) | **Confirmed** | Full chain re-read: `tenancy/middleware.go:123-134` → `context.go:62-64` → `identity/handler.go:590-607` → `catalog.go:125,197-216` → `access/service.go:283-294` → `identity/service.go:69,268` → `users.sql` `GetUser`/`SoftDeleteUser` (filter on `id` only — read directly). |
| C2 (cross-tenant role tampering) | **Confirmed, impact corrected** | Not self-escalation to Super-Admin (`validateSystemRoles` requires a tenantless target; `RoleExistsForTenant` rejects system roles for a tenant). **Worse** in one dimension: an empty `roleIDs` skips validation and `ClearRoles` still runs — a tenant Admin can strip the Super-Admin's roles. §5 rewritten. |
| C3 (self-grant → cross-tenant reads) | **Confirmed** | `access/routes.go:12` + `catalog.go:100` (`PermRoleUpdate`, non-system) + `access/service.go:234-240` (strips only IsSystem) + `notifications/handler.go:125-150` (filter has **no tenant field**) + `notifications.sql:30-39` (no `tenant_id`) + `files/handler.go:31-47` (same) + `migrations/00011` (no `tenant_id` column) — all read directly. `scopedGet` (`notifications/service.go:126-135`) skips the tenant check when `AllTenants()`. |
| C4 (exempt-group root cause) | **Confirmed; fix refined** | A blanket caller check inside `Exempt()` needs a route-by-route review first (shared profile handlers; the whole `files` module lives in the exempt group). §5 fix rewritten. |
| H1 (`APP_ENV` validation) | **Partially correct — corrected** | `IsProduction()` uses `strings.EqualFold` — case-insensitive, but no trim (verified `config.go:304-307`). Case is handled; whitespace/synonyms are not. Row corrected. |
| H2 (install.sh seeds dev env) | **Partially correct — softened** | Template is copied only when absent, with an explicit "review it before starting" step (verified full script). The enforcement gap is real; the "installs dev .env into production" framing was overstated. |
| H3 (seed has no prod guard) | **Confirmed** | `internal/cli/seed.go` `db:seed` body and `seeder.Run` contain no `IsProduction` check (read directly). |
| H4 (tokens never expire) | **Confirmed** | `config.go:774` default 0; `Validate` only rejects negatives (`:464-466`). |
| H5 (login enumeration) | **Confirmed** | `authn/service.go:135-146` (422 "The selected email is invalid."), `:59-61` (401 "The provided password is incorrect."), `:55` (`verifyBeforeLogin` before bcrypt). |
| H6 (throttle keyed email\|IP) | **Confirmed** | `loginThrottleKey` (`:161-163`); note the global per-IP limiter still bounds a single source. |
| H7 (`user.update` implies role assignment) | **Confirmed** | `request.go:20` (`Roles []int64`), handler passes it through, `service.go` `if input.Roles != nil { ClearRoles; AssignRoles }` — read directly. |
| H8/H9 (public callback) | **Confirmed, sharpened** | Mitigation verified: completed/refunded payments are returned unchanged (`subscription_service.go:249-251`). Sharpened: cash refs (`CASH-<unix>`) are guessable → unauthenticated cross-tenant tampering with *pending* manual payments. |
| H10 (gateway 5xx = rejection) | **Confirmed** | `drivers.go:199-207` — status code checked only to build an error message for unparseable bodies. |
| H11 (no subscribe idempotency) | **Confirmed** | `Subscribe` (`:117-172`) has no pending-check; every call creates subscription + charge. |
| H12 (limit TOCTOU) | **Confirmed** | `identity/service.go:102-110` — `Count` then insert, no transaction/lock. |
| H13 (Developer role = all perms) | **Confirmed, by design** | Documented in-code at `seeder.go:28-31`; re-graded as a design concern. |
| H14 (dev.view can download dumps) | **Confirmed** | `/dev/api` group requires only `dev.view`; `GET /backups/{id}/download` is a GET, so `requireManage` (mutations only) does not apply. |
| H15 (telemetry SQL args) | **Confirmed, gating added** | Capture is gated on `DevDashboard.Enabled` (`platform.go:88-96`, default `!IsProduction()`); `redact.Args` passes positional scalars through. "Credential-equivalent" softened to "credential-like". |
| H16 (public files) | **Confirmed and strengthened** | `config.go:467-471` — `FILES_DISK` accepts only `public|s3`; **no private option exists**. Rewritten. |
| H17 (SVG XSS) | **Confirmed, precondition added** | `static.go` sets `nosniff` but no `Content-Disposition`/CSP; requires direct navigation; `immutable` caching persists the payload. |
| H18 (health always 200) | **Confirmed** | `health/handler.go:30-52` — never writes a failure status; `HasFailures` unused. |
| H19 (cachekeys dead) | **Confirmed, re-graded Medium** | Ad-hoc invalidation is wired at every mutation site found; the cost is maintainability, not a live staleness bug. |
| H20 (memory driver unbounded) | **Confirmed** | `memory.go:86-99` — map insert, no cap/eviction; `ttl<=0` never expires. |
| H21 (file driver multi-process) | **Confirmed, re-graded Medium** | Limitation is documented in the package header; it is a foot-gun, not a hidden bug. |
| H22 (multipart in RAM) | **Confirmed** | `multipart.go` — `ParseMultipartForm(maxBytes)` with the total cap as the memory threshold. |
| H23 (WriteTimeout < RequestTimeout) | **Confirmed** | `config.go:580,589` (30s vs 60s); `Timeout` writes the 503 only after the handler returns (`middleware.go:166-171`). |
| H24 (rate limiter fail-open) | **Confirmed, re-graded Medium** | Deliberate and documented in-code (`cache.go:18-19`). |
| H25 (scheduler no coordination) | **Confirmed, re-graded Medium** | Single-scheduler topology is documented; nothing enforces it. |
| H26 (fan-out ceiling) | **Withdrawn as stated, re-graded Medium** | "Cannot complete" was an unmeasured extrapolation; the linear-work + fixed-timeout facts stand. |
| H27 (`gateway_ref` unindexed) | **Confirmed** | Only one occurrence in migrations (`00009:56`); no index anywhere. |
| H28 (no statement_timeout) | **Confirmed, nuance added** | HTTP/scheduler/River paths carry deadlines via context; CLI paths do not. |
| H29 (deploy writes read-only path) | **Confirmed** | Units harden with `ProtectSystem=strict`; `install.sh` (read in full) never repoints `APP_*_DIR` away from `./var`. |
| H30 (CI scanners missing) | **Confirmed; speculation withdrawn** | The golangci-lint build-failure speculation is withdrawn (unverified; clean locally). |
| H31 (release.sh) | **Confirmed** | `HEALTH_URL` defaults to `/up` (`:31`); `health_check` → `die` (`:108`); `do_rollback` only via `--rollback` flag (`main()`); the `npm ci`-as-root detail was unverified and is dropped. |
| Exec-summary "account-takeover" (C2) | **Corrected** | See C2. |
| Metrics in §3 (67 perms, 7 tasks, 31 tables, 243 queries, coverage) | **Confirmed** | Recounted: `rg -c` on catalog entries (67), `scheduler.Task{` (7), `CREATE TABLE` (31), `-- name:` (243). |

**Net effect of the second pass:** 3 findings strengthened (H8/H9 sharpened, H16 strengthened),
8 re-graded or corrected (C2 impact, H1, H2, H19, H21, H24, H25, H26), 2 speculative details
withdrawn, 1 new Low finding (M78). **No Critical finding was invalidated.** Scores are
unchanged; the remediation priorities in §10 are unaffected.

## 7c. Final full-codebase check

A third pass re-ran every objective gate and swept the corners the earlier passes covered
least, plus repo-wide pattern sweeps for common vulnerability classes.

**Objective gates (all re-run at commit `1f55eeb`):**

| Gate | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l .` | 0 files |
| `go test -race -count=1 -p 2 ./...` | **PASS — zero race detections, all packages green** |
| `golangci-lint run ./...` | PASS (previous run; unchanged tree) |

**Pattern sweeps (repo-wide, non-test code):**

| Pattern | Result |
|---|---|
| `math/rand` in security-relevant roles | Clean — single use is `collector.go:107`, sampling only (`rand.Float64() < SampleRate`); all IDs/secrets use `crypto/rand` |
| `exec.Command` misuse | Clean — 3 uses, all `CommandContext`/fixed argv (`backup/dump.go:40,68`, `cli/gen.go:230` runs the pinned sqlc module); no shell interpolation |
| SQL built by string concat (`Sprintf` into queries) | Zero matches — all SQL is sqlc-generated or `CASE WHEN`-parameterised |
| `TODO`/`FIXME`/`HACK`/`XXX` markers | Zero matches in `internal/` and `cmd/` |
| File-permission hygiene | See L2 below |

**Least-reviewed corners (read directly):** `cmd/app/main.go` (12-line shim, clean),
`internal/version` (ldflags target only), `internal/platform/logging` (JSON in
production/staging, text otherwise), `internal/platform/metrics` (label cardinality
method/route/status only; `statusRecorder` implements `Unwrap()`/`WroteHeader()` — which
confirms the Timeout/telemetry double-write is specific to `captureWriter`), `internal/platform/docs`
(unpinned CDN, documented), `tools/tools.go` (build-tagged, never in the app graph),
`internal/platform/storage/local.go` (traversal-safe; see L2), `api/embed.go`/`web/dev` embed
wiring (clean).

**New findings from this pass (all Low):**

- **L1** — `logging.New` lowercases but does not trim the env string (`internal/platform/logging/logging.go:20-25`) — same class as H1: `APP_ENV="production "` selects text logs. Fixes itself with the H1 normalisation.
- **L2** — Private-disk writes create files world-readable: `os.MkdirAll(dir, 0o755)` + `os.Create(target)` (`internal/platform/storage/local.go:44-47`) yields `0644` files. Matters only for `DATA_PROCESSING_DISK=private` content on multi-user hosts. **Fix:** `os.OpenFile(target, …, 0o600)` (and consider `0o700` dirs).
- **L3** — The docs UI loads Scalar from a CDN with no `integrity` (SRI) attribute (`internal/platform/docs/docs.go`, `scalarHTML`) — a supply-chain hardening gap for an already-CDN-dependent page (documented trade-off: CSP is omitted for this page). **Fix:** pin a version + SRI hash, or self-host the asset.

**Verdict of the final check:** the tree is clean under every gate including the race detector;
the earlier findings stand unchanged; the three new Lows are polish, not risk drivers. Overall
score remains **6.6 / 10**.

## 7d. CRUD flow gaps — consolidated view

The module skeleton (`routes → handler → request → service → repository → dbgen`) is sound and
consistently applied — the architecture is right. The gaps are in how the flow is *applied*.
Each gap below already exists as findings in this report; this table consolidates them into a
single fix-list so the flow work can be tracked as one effort. None require a redesign.

| # | Flow gap | Findings | Fix direction | Priority |
|---|---|---|---|---|
| 1 | Authorization enforced in handlers, not services — CLI/jobs/schedulers bypass it | M1 | Move `MustCan` into service methods (or a service-level guard); keep handler checks as defence in depth | P1 |
| 2 | Tenant filtering not in SQL for `users`/`roles`/`model_has_roles` (post-fetch Go checks instead) | C1, C2, M2, M7 | Add `tenant_id` to the queries; delete the post-fetch checks; keep the internal-route gate (C4) | P0 |
| 3 | Write-path transactions inconsistent — billing multi-statement state changes run outside `WithTx` | M8, M9, M10, M11 | One `WithTx` per use case; state-transition predicates (`AND status IN (...)`) | P1 |
| 4 | No idempotency on state-changing writes | H8, H9, H11 | `Idempotency-Key` on subscribe/pay; authoritative callback verification only | P1 |
| 5 | Generated `PATCH` overwrites every column (full replacement semantics) | M56 | `sqlc.narg` + `COALESCE` partial updates (or document PUT semantics) | P2 |
| 6 | N+1 lookups on hot paths (role refs per user row; billing plan resolved twice per check) | M79, M13 | Batch `RoleRefsForUsers`; memoize the plan per request/tenant | P2 |
| 7 | Offset pagination + `COUNT(*)` as the default list strategy | M22, M25 | Make keyset/cursor the default for large tables (infra already exists in the notification feed) | P2 |
| 8 | No optimistic concurrency / lost-update protection | M80, H12, M6 | Version columns or `updated_at` predicates; conditional inserts for quotas | P1 |
| 9 | Repeated per-request lookups (`IsSuperAdmin` uncached per `Can` call) | §9.1, M13 | Cache the super-admin probe with the permission set | P2 |

Items 1–4 are the ones that change correctness or security posture; 5–9 are efficiency and
data-integrity hardening. After 1–4 plus the C1–C3 fixes, the CRUD flow is at the level the
documentation already claims.

---

## 8. Documentation accuracy — verified drift

The documentation is unusually good in intent and unusually wrong in detail. Every item below was
verified against the code.

| Document | Claim | Reality |
|---|---|---|
| `specs/README.md:5,10,36` + every phase spec | The roadmap lives in a root `PLAN.md` | **`PLAN.md` does not exist in the repository** (verified: `Test-Path` false, absent from `git ls-files`) |
| `docs/architecture.md:110`, `AGENTS.md:209` | `cachekeys` is "the primary invalidation mechanism" | Imported **nowhere** in the repo (verified) |
| `docs/architecture.md:120-122` | Lists 4 scheduled tasks | 7 exist — `backup.run`, `backup.clean` and `devdashboard.prune` are missing (`internal/app/platform.go:284,303,327`) |
| `AGENTS.md:166`, `docs/architecture.md:34-37` | Middleware chain is `RequestID → AccessLog → Recover → Timeout → RateLimit → Locale` | The real chain adds `RealIP`, `SecurityHeaders`, `CORS`, telemetry and metrics (`internal/app/routes.go:26-47`) |
| `docs/developer-guide.md:57-59` | Authorization is enforced "in the **service** (so CLI/jobs are covered too)" | Every `MustCan` call is in a handler |
| `docs/developer-guide.md:36` | The generated service "Must NOT … import `httpx` or `authn`" | The generated service imports `authn` (`internal/scaffold/templates.go:292,398-407`) |
| `AGENTS.md:25` | `make dev` serves the UI on `:5173` | It serves `:8080` (`web/dev/scripts/dev.mjs:9-10`); `:5173` is `make dev-web` |
| `README.md:179` | `.env.example` documents "every key with working defaults" | Six keys are missing (M63) |
| `.github/copilot-instructions.md:9` | "Large modules (billing) split services into `service/` and HTTP into `api/`" | There is no `api/` package — billing's HTTP lives at the module root |
| `internal/cli/openapi.go:16-18`, `docs/runbooks/deploy.md:77` | `openapi:check` "needs a DB" | The same check runs DB-less in CI (`internal/app/contract_test.go:15-30`) |
| `api/openapi.yaml:25,3892` | Public tenant resolution accepts `X-Tenant-Code` | No code reads it; `GetByCode` is used only by a test |
| `docs/architecture.md:150-151` | Cancelling keeps access until `expires_at` | Cancel revokes access immediately (`sql/queries/billing.sql:50-55`) |
| `Makefile:28-33` | `clean` removes artifacts | On Windows it uses `rmdir /s /q` (directories) for `coverage.out`/`coverage.html` (files) → those are never deleted |

---

## 9. Scalability assessment

### 9.1 Per-request database cost

An authenticated, tenant-resolved request pays **4–6 round trips before business logic**:

1. `GetToken` — `internal/authn/authenticator.go:99`
2. `FindByID` for the user — `internal/authn/authenticator.go:116`
3. `IsSuperAdmin` inside `Can` — `internal/authz/checker.go:38-48` — **an uncached DB query on every single `Can`/`MustCan` call**, and handlers call it several times per request
4. `IsSuperAdmin` inside `Resolve` — `internal/tenancy/middleware.go:44`
5. `branchBelongsToTenant` — 0–1 (cached 5 min)
6. Token `last_used_at` touch — 0–1 (at most once per minute)

Then the business path: the user list adds **one role query per row** (`identity/handler.go:89-91`
→ `authz/checker.go:92-105`), so a 100-row page costs 101 extra queries; the billing guard
resolves the plan twice per check (`billing/service/guard.go:74-92,130-153`).

**This is the single most actionable scalability finding:** caching the super-admin probe
alongside the already-cached permission set removes one query per authorization check — likely
2–4 queries per request.

### 9.2 Horizontal scaling is unsafe with the default configuration

Four independent mechanisms break the moment you run two API replicas:

| Mechanism | Consequence with N replicas |
|---|---|
| `CACHE_DRIVER=memory` default (H20) | Permission, tenancy and rate-limit caches are per-process. The effective rate limit becomes **N × configured**. The scheduler heartbeat is invisible to the API's health check. Permission staleness up to the 10-minute TTL. |
| Scheduler has no leader election (H25) | Every replica runs `billing.expire-subscriptions`, `send-renewal-reminders` and `backup.run` → duplicate notifications, duplicate backups. |
| Rate limiter fails open on cache error (H24) | A cache outage disables rate limiting entirely, across all replicas. |
| Job envelopes drop the impersonation scope (M48) | A job enqueued under global impersonation carries tenant `0` and executes **unscoped across tenants**. |

The project's own topology is single-host (`docs/architecture.md §11.19`: three systemd units on
one machine), so this is a *consistent* design — but it means "scale out" is not currently a
supported operation, and the documentation should say so explicitly.

### 9.3 Vertical ceilings

| Ceiling | Where | Consequence |
|---|---|---|
| ~100 MB heap per concurrent upload | `httpx/multipart.go:15`, `files/service.go:27` | 10 concurrent uploads ≈ 1 GB RSS |
| Offset up to 100 M | `httpx/filters.go:15,35` | One request forces a full index scan |
| `COUNT(*)` per list | every list endpoint | O(n) per page request |
| `ILIKE '%…%'` without trigram index | `users.sql:14-18` and 4 more | Seq scan + sort on the hottest lists |
| Fan-out materialises all user ids + 2 cache ops each | `notifications/service.go:300-318,454-467` | Never completes for a large tenant |
| Export = full workbook × 2 in memory | `identity/exchange.go:329-390` | OOM on a large tenant |
| Import = whole file, 16 GB unzip ceiling | `identity/exchange.go:560-581` | Worker OOM from a crafted file |
| No `statement_timeout` | `database/registry.go:73-78` | One runaway query pins a pool connection |
| `dev_queries` etc. get one INSERT per captured entry | `dev/telemetry_sink.go:31-57,65-78` | Thousands of single-row inserts/s on the pool being observed |
| Unbounded growth: `cache`, `notifications`×4, `dev_*` | M30, M31, M36 | Disk exhaustion over months |
| `GROUP BY` over `river_job` every 30 s | `queue/health.go:22-45` | Expensive once job volume grows |

### 9.4 What does scale well

- **River** — horizontal worker scaling, per-queue `MaxWorkers` (`queue/river.go:123-126`),
  transactional enqueue, per-channel retry/timeout, built-in job cleaner bounding `river_job`.
- **pgx pools** — lazy, bounded, health-checked, closed on shutdown.
- **Telemetry** — bounded channels, non-blocking push with a drop counter, batched flush off the
  request path. This is the right design and it is implemented.
- **The notification feed** — keyset cursor pagination on `(created_at, id)`, not `OFFSET`.
- **Schema conventions** — partial unique indexes with `WHERE deleted_at IS NULL` applied
  consistently to every soft-deletable table; uniform `tenant_id` + `created_by`/`updated_by` +
  `ON DELETE CASCADE`.
- **CI economics** — the contract drift check and generated-code diff gates are cheap and catch
  real regressions, so they scale as the team grows.
- **Onboarding** — `ims gen module` makes the pattern reproducible, which is how this kind of
  consistency is usually lost.

### 9.5 Scalability score: **5.5 / 10**

Comfortable for one node serving hundreds to low-thousands of users. Not safe for horizontal API
replicas with the shipped configuration, and not sufficient for tenants in the 10 k+ user range
for fan-out, export or import.

---

## 10. Prioritised remediation roadmap

### P0 — Blockers (≈2–3 days; unblocks any external exposure)

1. **Fix the tenant-exempt boundary.** Mark every permission gating an exempt route as
   `IsSystem: true` (or add `*.internal.*` variants); optionally add an internal-identity gate
   to `Exempt()`-mounted route families, decided per route (see C4). *(fixes C1, C2, C3)*
2. **Add `tenant_id` to `files`; add the filter to the `notifications` list/get/update/delete
   queries.** *(fixes the second half of C3)*
3. **Add an integration test that hits every `/api/v1/internal/*` route with a tenant-scoped
   token and asserts 403.** This is the regression test the project currently lacks, and it would
   have caught C1–C3.
4. **Production guards.** Refuse `db:seed` in production unless forced *and* the password has
   been changed; ship `deploy/.env.production`; validate `APP_ENV` against an allowlist.
   *(H1, H2, H3)*
5. **Non-zero `AUTH_TOKEN_EXPIRATION` default**, rejected as `0` in production. *(H4)*
6. **Uniform login response** + a dummy bcrypt comparison; throttle on account **and** IP
   independently. *(H5, H6)*

### P1 — Correctness & hardening (≈1–2 weeks)

7. Move authorization into services (or add a service-level guard interface) so CLI, jobs and
   schedulers are covered. *(M1)*
8. SQL-level `tenant_id` filters on `users`, `roles`, `model_has_roles`, `role_has_permissions` —
   delete the post-fetch Go checks. *(M2)*
9. Strip `roles` from `PATCH /user/{id}`; require `user.role.assign`; forbid self-targeting. *(H7)*
10. Billing: `Idempotency-Key` on subscribe; authoritative callback verification only; status
    transition predicates (`AND status IN (...)`); wrap activation + payment in one transaction;
    index `payments.gateway_ref`. *(H8–H11, M8–M11, H27)*
11. Atomic feature-limit enforcement. *(H12)*
12. `/api/health` returns 503 on failures. *(H18)*
13. Wire `cachekeys` in (or delete it and fix the docs); default the cache driver to
    `redis`/`database` outside local development; add an LRU cap to the memory driver. *(H19, H20)*
14. Scheduler advisory lock, mirroring the migration lock. *(H25)*
15. Fix `HTTP_WRITE_TIMEOUT` vs `HTTP_REQUEST_TIMEOUT` ordering. *(H23)*
16. Multipart `maxMemory` 8 MB + stream to disk; default the files disk to private; drop `svg`
    from the allow-list. *(H16, H17, H22)*

### P2 — Production maturity (≈1 month)

17. Cache the super-admin probe with the permission set; batch role refs per page; cache the
    billing plan per tenant. *(§9.1)*
18. Redact telemetry SQL args by statement/column; gate body capture on the sample rate. *(H15, M50)*
19. `statement_timeout` + `idle_in_transaction_session_timeout` on the pool. *(H28)*
20. Retention tasks: expired cache rows, expired notifications, telemetry. *(M30, M31)*
21. Index work: trigram for search, `(tenant_id, deleted_at)`, `(tenant_id, created_at DESC, id DESC)`
    on the feed, `subscriptions(tenant_id,status)`, `dev_requests(tenant_id,user_id)`. *(M33–M36)*
22. Frontend: CSP on the SPA shell; call `/api/v1/logout` on sign-out; parse the full RFC 9457
    problem; stop retrying 4xx; modal focus management; stop client-side sorting of server lists.
    *(M64–M72)*
23. Frontend tests: vitest for the API client + token lifecycle, Playwright for login and one
    panel; add `make web-check` to CI. *(M64)*
24. CI: `govulncheck`, `dependency-review`, CodeQL/gitleaks, `concurrency`, golangci-lint v2,
    a coverage floor. *(H30)*
25. Deploy: `StateDirectory=`, auto-rollback on a failed `/api/health?fresh=1` check, build as
    an unprivileged user, root-owned release tree. *(H29, H31, M62)*
26. Scaffold: validate name/table/package; PATCH semantics; no `authn` import in the generated
    service; fail hard when a module cannot be analysed. *(M55–M58)*
27. Contract: include `/dev/api/*` in drift checking; either use or remove `internal/contract`;
    delete the `X-Tenant-Code` parameter. *(M59–M61)*
28. Docs: fix the 13 drift items in §8; decide whether `PLAN.md` exists.
29. CLI: one `withPlatform` helper; split `NewPlatform` into capability providers; close the
    telemetry collector on error paths. *(M44–M46)*

### P3 — Polish & long-term

30. `sqlc.narg` + `COALESCE` for partial updates; keyset pagination + streaming for exports;
   CSV formula-injection guard. *(M56, M75–M77)*
31. `users.is_active` → `boolean`; CHECK constraints on status columns; `payments.currency` scale.
   *(M39, M40)*
32. Broaden `.golangci.yml` (`gosec`, `bodyclose`, `errorlint`, `unparam`, `gocritic`).
33. Rate limiter: user-keyed tier after auth; exempt health/metrics. *(M24)*
34. HMAC-sign the notification cursor; validate `X-Request-ID`. *(M21, M24)*
35. `Makefile` Windows file deletion; remove tracked `.tanstack/tmp` junk. *(M74)*

---

## 11. Verdict

**What this codebase is.**

A strong, opinionated, unusually disciplined Go monolith whose *engineering* is well above the
norm for a "starter" — layered, tested at the seams, contract-enforced, generated-code-gated,
and honestly documented in its intent. The primitives are right: `crypto/rand` tokens, bcrypt at
cost 12, constant-time comparison, RFC 9457 done properly, fail-closed proxy trust, fail-closed
nullable-parameter SQL, advisory-locked migrations, transactions that release locks on timeout,
a cache contract suite, a queue envelope that restores tenancy, telemetry with real backpressure,
and no command injection anywhere.

**Where it falls short.**

The *security model* is unfinished. The vision says "explicit scoping in SQL is the only isolation
boundary"; the `branches` module implements that faithfully and beautifully; `identity`,
`notifications` and `files` do not. And the mechanism that would have caught the difference — a
test asserting that a tenant-scoped token gets 403 on every `/internal/*` route — does not exist.
Three verified chains take a default tenant Admin from "administrator of one tenant" to
"administrator of every tenant on the system". Everything else on the High list is a
production-hardening gap; these three are a design hole. A second verification pass
(§7b) confirmed all three chains end-to-end and corrected several High-severity
characterisations — the criticals survived falsification; some Highs were deliberate
trade-offs in disguise.

**How good is it? 6.6 / 10.**

Roughly: 8.5 for the architecture, 7.5 for the code quality, 4.5 for security, 4.5 for tenancy
isolation, 5.5 for scalability, 5.5 for testing, 7.5 for documentation and DX.

**How scalable? 5.5 / 10.** One node, hundreds to low-thousands of users: comfortable.
Horizontal replicas with the shipped configuration: unsafe (per-process cache, no
cross-process scheduler coordination, fail-open rate limiter). Tenants in the 10 k+ range:
fan-out, export and import all need rework (linear-work jobs under fixed timeouts, full
in-memory workbooks, whole-file imports).

**The encouraging part.** None of the Critical findings require a redesign. They require three
route guards (well, one catalog change plus one gate), one schema column, one SQL filter, one
test, and one config default. The architecture that holds them up is already correct — which is
exactly why the fixes are cheap.

**My recommendation.** Do P0 before this touches anything external. It is roughly three days of
work and it converts the project from "a good-looking starter" to "a starter whose isolation
boundary you can rely on". Then P1 over the following two weeks, and treat the P2 items as the
roadmap to genuinely production-grade — paying particular attention to the cache driver default,
the scheduler lock, and the telemetry redaction, because those are the three that will bite at
scale rather than at launch.

---

*Review performed against commit `1f55eeb`. Every Critical and High finding was re-verified
line-by-line against the source in a second pass — the full log is §7b; claims that could not
be verified were corrected, re-graded, or withdrawn there rather than left standing.*
