---
title: Design decisions
description: The deliberate trade-offs behind the starter — what was chosen, what was rejected, and why.
---

A starter is mostly decisions. These are the ones worth knowing before you change something,
numbered so code comments and reviews can refer to them. Each says **what** was decided and
**why**; when a decision changes, edit it here (and in `AGENTS.md`) in the same pull request.



1. Explicit query scoping instead of global scopes; explicit tenancy params everywhere.
2. Tenant/audit stamping in services, not model observers.
3. `ActingGlobal` impersonation is a read-only, all-tenant view; writes need acting headers.
4. Bigserial sequences are synced after explicit-id seeding (`SyncIDSequence`).
5. Subscribe validates and charges before deactivating previous subscriptions; gateway failure
   cancels the fresh pending subscription.
6. E-mail is not changeable through user update.
7. `order_by`/`order_direction` are whitelisted per endpoint.
8. One error universe: RFC 9457 everywhere.
9. OTP codes are cache-only; delivery goes through the queue (`mail.send`).
10. Local scheduler heartbeat visibility requires a shared cache driver (memory is per-process).
11. Notification fan-out counters are idempotent (`INSERT ... ON CONFLICT ... RETURNING`); reads
    are user-scoped (never branch-scoped); mark-all-read is scoped + idempotent; unread counters
    recompute from the database on cache miss. Import/export job rows report real progress and an
    honest FAILED status; downloads require COMPLETED + file.
12. One `files` table with polymorphic attachments
    (`owner_type`/`owner_id`/`collection`) with a Go collection registry, single-file collections
    enforced by service + partial unique index; no conversions/custom-properties. The resource is
    `/v1/files` with `file.*` permissions (legacy `upload.*` rows are retired by migration).
    Like users, files have two route families: `/v1/files` behind `tenant.resolve` (rows stamped
    with the effective tenant, so impersonated uploads land in the acting tenant) and
    `/v1/internal/files` behind `tenant.internal` with system `file.internal.*` permissions
    (global uploads, cross-tenant reads). One handler serves both, parameterized by the family.
13. i18n catalogs cover the API-relevant message groups (`messages`, `http-statuses`,
    `validation`; ~390 keys × en/bn); validator tags map to `validation.*` keys via go-i18n.
14. Legacy `.xls` imports are rejected (xlsx/csv supported); per-row import errors use a
    `[{row, errors:{...}}]` shape; filter snapshots live in `data_processing_jobs`.
15. **Storage disks** (`config/storage.go`, `platform/storage`) work like Laravel's
    `config/filesystems.php`: each disk has a name, a driver (`local` | `s3`) and a visibility.
    Built-ins are `private`, `public` and `s3` (from `S3_*`); `STORAGE_DISKS=r2,minio` adds more
    from `STORAGE_<NAME>_*`. `FILES_DISK`/`DATA_PROCESSING_DISK` name any disk, so moving uploads
    to S3, Cloudflare R2, MinIO, Spaces or B2 is a `.env` change. Private disks (local or bucket)
    never hand out URLs; files are read through the authorized download route, and avatars go to
    `FILES_PUBLIC_DISK`. The S3 driver supports a key prefix (`ROOT`), presigned
    `TemporaryURL`s, and only sends checksums when required (S3-compatible services reject the
    SDK default). File URLs are computed from disk + path (no stored `url` snapshot).
16. **Contract.** `api/openapi.yaml` is canonical and served verbatim; oapi-codegen runs
    types-only (the handwritten chi chain + `Responder` own the HTTP layer) and its output is
    diff-gated in CI. Docs are served via Scalar from a CDN (config-gated with `DOCS_ENABLED`).
17. **Observability.** `/metrics` is a small dependency-free Prometheus text exporter
    (`internal/platform/metrics`, low cardinality: method/route/status only), config-gated with
    `METRICS_ENABLED`. A Content-Security-Policy header is deliberately omitted so the docs UI can
    load its CDN assets; baseline headers (nosniff, frame-deny, referrer, HSTS-over-TLS) are on.
18. **Tenant isolation (no RLS).** Postgres row-level security was evaluated and
    deliberately dropped: a correct setup needs a dedicated non-owner application role, a
    fail-closed policy, and the tenant GUC set on *every* connection (not just transactions) —
    and even then it would only backstop the explicit filters below. Instead, **explicit tenant
    scoping is the only isolation boundary**: every tenant-scoped query MUST receive and filter on
    an explicit `tenant_id` (and `branch_id` when branch-scoped), and a missing filter is a
    security bug. Coverage is enforced by the per-module isolation tests, not by the database.
19. **Deployment.** Single binary + systemd (units: `ims-go-admin-starter`, `-scheduler`,
    `-worker`); a **build-on-server** `deploy/scripts/release.sh` (no container/artifact host) that
    builds a static binary, runs migrations as a gated release step, atomically switches the
    `current` symlink, restarts units and health-checks. Writable state lives under
    `/var/lib/ims/ims-go-admin-starter` (never inside a release directory). Runbooks live in
    [Operations](../operations/deploy.md).
20. **Trusted proxies.** Forwarded headers (`X-Forwarded-For`/`X-Real-IP`) are honoured
    **only** when the immediate peer is listed in `HTTP_TRUSTED_PROXIES`, and the client IP is
    resolved right-to-left skipping trusted hops (an explicit, reviewable policy rather than
    chi's default `middleware.RealIP`). The resolved IP drives login throttling, rate limiting
    and access logs.
21. **Generators.** `ims gen module --fields="name:string,price:decimal,…"` scaffolds a
    goose migration, sqlc queries and a full module package, non-interactive and CI-friendly,
    then runs `sqlc generate` so the output compiles. Generated modules use explicit `tenant_id`
    scoping, `httpx.Kit` handlers and route-level permissions (`r.With(h.can(...))`); the
    generated service test needs no database.
22. **SQLite deferred.** v1 is **PostgreSQL-only**. SQLite was scoped and
    deliberately deferred to a future version: with sqlc it needs a second, permanently-duplicated
    repository set (per-module), and an ORM-style alternative would mean rewriting the whole data
    layer and giving up compile-time SQL + explicit tenant scoping. `DATABASE_DRIVER=sqlite` is
    rejected at startup; the schema/queries remain Postgres-only.
23. **Authorization at the HTTP edge.** Permissions are declared on routes
    (`r.With(h.can(perm))` → `authz.Require`), mirroring Laravel's `can:` middleware; a handler
    calls `MustCan` itself only when the permission depends on the payload. CLI commands, queue jobs and scheduled tasks are trusted callers
    and do not re-check permissions. Services remain permission-free unless they need a Gate-style
    decision, in which case they take `*authz.Checker` explicitly. Services and SQL **always**
    enforce tenant scoping and business invariants (Super-Admin protection, no self-escalation)
    regardless of caller. A route-coverage integration test asserts every protected route returns
    403 to a token without permissions, so a forgotten handler check fails CI.
24. **Laravel-style handlers.** Handlers embed `httpx.Kit` (`Bind`, `Validate`, `PathID` and the
    responder methods) and read `authn.UserID(r)` / `tenancy.From(r)`; config is split one file per
    section like `config/*.php`; CLI commands share one `withPlatform` helper.
25. **Feature-flag rollout.** An enabled flag can carry a percentage; subjects hash to a stable
    bucket per flag, so raising the percentage only adds users (see §6).
26. **Quotas under advisory locks.** `users.max`/`branches.max` count-check-insert runs under a
    per-tenant Postgres advisory lock (`TxManager.WithTenantLock`), so concurrent creates cannot
    exceed a plan limit. The plan limit is read before the lock (`Guard.QuotaFor`) and every
    query inside it runs on the locked transaction, so lock holders never wait for a second pool
    connection (`TestQuotaUnderConcurrency` runs 8 creates on a 3-connection pool).
27. **Dashboard audit.** Every dashboard mutation and dashboard sign-in (including failures, never
    the password) is written to `dev_actions` with IP and browser, kept for
    `DEV_DASHBOARD_AUDIT_RETENTION_DAYS`.
28. **Suspended tenants are refused at the tenancy gate.** `is_active = false` suspends a tenant.
    The user lookup that authentication runs on every request also reads the tenant's state (one
    joined query, no cache), so `tenancy.Resolve` and `tenancy.Exempt` refuse the tenant's users
    with `403 TENANT_SUSPENDED` from their next request, and sign-in refuses them after the
    password matches. The session routes (logout) stay open, and platform accounts can still act
    inside the tenant: the check reads the caller's own tenant. Reactivating restores access with
    the existing tokens. Public tenant routes (`tenancy.Public`) refuse an inactive tenant too.
    Billing suspends lapsed tenants only when `BILLING_SUSPEND_ON_EXPIRY` is on.
29. **One version, one file.** `VERSION` (semver, no `v`) is the single source of truth. Release
    builds stamp it with `-ldflags`; every other build reports `<version>-dev`, so a local binary is
    never mistaken for a release. See [Releases](../operations/releases.md).
30. **Docs live in the repository.** `docs/` is plain Markdown that reads on GitHub, so code and
    docs change in the same pull request. This repository publishes no site of its own: the
    backend's guides are also on the API starter's site. Reference pages (CLI, configuration, permissions, Make targets) are generated from the
    code by `ims gen docs` and checked by `make gen-check`, like sqlc and OpenAPI output.
31. **Act as a user, per request.** `X-Acting-User` swaps the identity inside `tenancy.Resolve`
    for one request instead of issuing a login-as token, so there is no second session to expire
    or revoke. Whoever holds `user.impersonate` may use it (Super-Admin, Developer, assigned staff
    roles); targets that could act as others are refused. Account changes are blocked while
    acting.
32. **System audit log in `activity_log`.** Rows with no tenant are the system log; reading them
    (and every tenant) needs `activity.view.all`. Act-as is written there: the start
    (fail-closed), every request, and refusals. Entries written while acting carry the
    impersonator; tenants see only `by_staff`.
33. **MCP tools are REST operations.** An operation marked `x-mcp` in `api/openapi.yaml` is a tool,
    and calling it serves that operation through the `/api/v1` router in-process, with the
    identity in the context (never the token). There is no second authorization path: permissions,
    tenancy, plan limits, validation, errors and the activity log are the REST call's. MCP-only
    endpoints are not allowed; a new tool is a new REST operation first.
34. **Our own OAuth server for the connector.** The API is its own OAuth 2.1 authorization server
    (authorization code + PKCE S256, rotating refresh tokens) instead of requiring an external
    identity provider, so the starter works as a Claude connector out of the box. Clients connect
    with a Client ID Metadata Document (no registration) or a client an admin registered; Dynamic
    Client Registration is deprecated by the MCP spec and not built. An external IdP stays possible:
    the protected resource metadata can name any issuer.
35. **MCP tokens are a separate kind.** An MCP access token is a personal access token with
    `mcp:*` scopes and an audience, tied to its connection; only `/mcp` accepts it and
    `authn.Authenticate` refuses it everywhere else, while `/mcp` refuses session tokens. A leaked
    agent token can't call the REST API, and a leaked session can't drive an agent.
36. **Social login is a provider check, not a second login system.** Google or GitHub only proves
    an e-mail address; the user is found by that verified address and signed in through the same
    checks and token as a password login, so tenancy, roles and permissions are untouched. The
    client runs the provider's flow and the API verifies one stateless POST: no link table, no
    server-side redirect state, no stored provider tokens, no OTP step. Providers are drivers in
    `internal/authn/social`. An unverified provider e-mail never signs anyone in.
37. **Every API login is in the activity log.** `authn.Service` reports finished attempts through
    `authn.LoginAudit` (implemented by `activity.Recorder`, like `tenancy.ImpersonationAudit`), so
    `authn` imports no module. Successful logins are tenant rows; failed ones are system rows.
38. **Maintenance mode lives in the database.** The API, worker and scheduler are separate
    processes, often on separate hosts, so `ims down` writes one row (`maintenance`, id 1) that all
    of them read, through a per-process copy refreshed every `MAINTENANCE_CHECK_INTERVAL` (no query
    per request). Laravel's file driver works for one host only, and the `memory` cache is
    per-process. While down, `/api/v1`, `/mcp` and `/oauth` answer `503 MAINTENANCE_MODE`; probes,
    `/dev` and `/docs` stay open, and `/up` stays 200 so supervisors and load balancers keep the
    servers. The bypass is a hashed secret sent in `X-Maintenance-Bypass` (a header, not a cookie
    or URL); staff tokens get no special pass. Scheduled tasks skip unless `EvenInMaintenance`;
    queue workers keep running.
39. **One delivery system for every message to a person.** OTP codes, recovery links and
    everything a product adds call `delivery.Service.Send`; mail (`internal/platform/mail`) and
    SMS (`internal/platform/sms`) are transports behind it, and push is a channel waiting for a
    provider. A message renders once at `Send`, so a broken template or a missing data key fails
    the caller instead of a background job, and again in the `delivery.send` job, so a template
    fix applies to retries. One job per channel means a slow SMS provider never delays the mail.
    Templates are folders of plain files with locale variants (`mail.bn.txt`), not i18n keys per
    sentence: a translator edits a whole mail, and plain text works without any HTML. The
    `deliveries` log stores the outcome and a masked address but never a body or the data, so
    one-time codes never reach the database.
40. **Two-factor reuses the OTP sign-in step.** A user with two-factor gets `OTP_REQUIRED` (with
    `method`) after the password and finishes at `/verify-otp`, exactly like `AUTH_OTP_ENABLED`, so
    clients, the MCP sign-in page and the dashboard need one code screen. One field takes an app
    code, a mailed code or a recovery code. The check is bound to the password step by a cached
    marker (five minutes), because a TOTP code exists at any time and would otherwise be a
    password on its own. TOTP is the standard library's HMAC-SHA1 (RFC 6238, tested with the RFC's
    vectors), so the starter adds no dependency; the API returns the `otpauth://` URL and the client
    renders the QR. Requiring two-factor gives a setup-only token instead of refusing the sign-in,
    so a user can enrol without an admin.
41. **`APP_KEY` encrypts, hashes compare.** Values the app must read back (TOTP secrets, the data
    of sensitive delivery jobs) are AES-256-GCM sealed with `APP_KEY`, each naming its key so
    `APP_PREVIOUS_KEYS` keeps old values readable during rotation. Passwords, tokens and recovery
    codes are hashed. Local environments without a key use a public development key with a warning;
    every other environment refuses to start without one.
42. **Duplicate writes are stopped on the server, keys are optional.** A guard refuses the same
    caller's identical write while it runs and for a few seconds after it succeeded (a cache lock),
    so double clicks and proxy re-sends are covered with no client work. Clients that need
    exact-once retries over minutes send `Idempotency-Key` (Stripe's semantics) and get the first
    answer replayed. Errors release the lock so a corrected request runs at once, server errors are
    never replayed, and a cache outage lets requests through: refusing every write would be worse
    than a rare duplicate.
43. **Webhooks are fed by the activity log.** `activity.Recorder` hands every stored entry to a sink,
    and the webhooks module queues a delivery for each subscribed endpoint, so a module becomes a
    webhook source by registering an action it already records (no second event bus, and payloads
    reuse the log's redaction). Only registered actions are deliverable. Signatures follow Standard
    Webhooks so receivers can use existing libraries. The delivery job schedules its own retries
    (eight attempts over about a day) rather than relying on the queue's backoff, and the client
    checks the address it actually connects to, which also stops DNS rebinding.
44. **Traces are OpenTelemetry, metrics stay Prometheus.** Tracing exports OTLP, so the backend is
    the team's choice (Tempo, Jaeger, Honeycomb, Datadog, Sentry), and it is off until an endpoint
    is set. Requests, SQL, queue jobs (which carry the trace in their envelope) and outbound HTTP
    are traced in the platform; modules add spans only around steps worth seeing alone. Metrics
    stay on the existing Prometheus exporter, which is what most teams scrape. Exemplars are left
    out to keep `/metrics` plain text format. No vendor SDK (Sentry) ships: OTLP reaches them all.
45. **The dashboard is styled by tokens, not a CSS framework.** Every colour, size and radius is a
    custom property in `web/dev/src/styles/tokens.css` (light and dark themes, five accent
    presets, a density), so a fork rebrands in one file and a check (`npm run check:style`)
    proves every text colour passes WCAG AA in both themes. Colour carries meaning only: green is
    healthy, red failed, amber needs a look, and the accent marks what is selected. Plain CSS kept
    the redesign a style change rather than a rewrite of every screen into utility classes.
46. **Scheduled runs are kept in a table; the heartbeat is not.** Each run of a task (scheduled or
    run by hand) is a `schedule_runs` row, so every process sees the history whatever the cache
    driver (an in-memory cache is per process), it can be filtered and paged, and it is pruned
    after `SCHEDULE_HISTORY_DAYS`. A run the scheduler lost is closed as `interrupted` when it
    starts again. The every-minute heartbeat opts out (`NoHistory`): it would be most of the
    table, and its timestamp is already in the cache for health. The Overview reads the cached
    health report in process (`health.Handler.Results`) rather than running the checks again.
47. **An erased user is a tombstone, and every module declares its own personal data.** Erasure
    keeps the `users` row (id, `erased_at`) with every personal column blanked, because
    `created_by`-style columns have no foreign key and billing and audit rows need an author that
    exists. Each module that stores data about a user registers a `privacy.Contributor`
    (`internal/privacy`; export + erase) in `internal/app/privacy.go`, and a coverage test fails
    when a migration adds a user-reference column no contributor or exemption covers. Audit rows
    (activity log, dashboard audit) are kept and scrubbed, not deleted, so the trail has no holes;
    the last active Admin of a tenant cannot be erased
    (`docs/design/account-deletion-and-export.md`).
48. **The client app is embedded in the binary and served at `/`.** `web/app` builds into the binary
    like the developer dashboard, so one deploy carries the API, the app and both dashboards. Every
    path outside the API and tool prefixes (`/api`, `/dev`, `/docs`, `/storage`, `/mcp`, `/oauth`,
    `/.well-known`, `/metrics`, `/up`) serves the app, so its routes and links in e-mails open it,
    while an unknown API path still answers a JSON 404. Its HTML carries a strict
    `Content-Security-Policy` (same-origin scripts, styles and connections, no inline code).
49. **An organization has an owner, and its Admin role is managed.** `tenants.owner_user_id` names
    the person who signed it up; nobody else may delete, deactivate, demote or reset them, and
    ownership moves only by transfer. The tenant Admin role is kept equal to
    `AdminTenantPermissions` (`permission:sync` adds what is missing) and cannot be renamed, deleted
    or edited, and a role editor can only grant permissions they hold. Together these stop an
    organization from locking itself out, or one admin from raising another above themselves.
50. **An announcement is one record with an audience.** Staff write it once: its kind, window,
    button, whether members may dismiss it, and either every organization or listed ones
    (`announcement_tenants`). Members read the live ones for their organization, and views, clicks
    and dismissals are one row per person (`announcement_events`), so reach is counted per
    organization without copying the announcement. Optional channels (mail, push) go out once
    through the notification system when it starts, claimed before sending so a crash sends to
    fewer organizations rather than twice to some.
51. **Lists follow the branch a member works in; not switching means every branch.**
    `users.active_branch_id` is set only by switching. A member who may view every branch and switch
    (`branch.view.all` + `user.branch.switch`) and has not switched sees every branch
    (`Scope.AllBranches`); anyone else sees the branch they switched to or their own.
    `scope.ListBranchID()` is what lists filter on (zero: every branch), while new rows are still
    stamped with `scope.EffectiveBranchID()` because a record always needs a real branch. A tenant
    is the isolation boundary; a branch only narrows what a member looks at.
52. **A background job belongs to whoever started it; admins see the organization's.** Owners
    open, download, retry and remove their jobs; `data_processing_job.view.all`, held by the tenant
    Admin role, does the same for every member's, since an admin may run those exports themselves.
    A personal-data export stays its owner's whoever asks: it is the one job holding someone's
    private data rather than the organization's.
