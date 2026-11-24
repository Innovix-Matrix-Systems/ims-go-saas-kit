---
title: "Design: account deletion and personal-data export"
description: Proposal for GDPR-style erasure and data export across every module.
---

> **Status: built.** All three phases of section 12 are in the code; how to use it is
> in [Account deletion & data export](../guides/privacy.md), the decision is number 47 in
> [Design decisions](../architecture/decisions.md). Where the build differs from the proposal
> below, section 14 says how and why. This page stays as the design record.

## 1. Problem

The starter can soft-delete a user (`SoftDeleteUser` sets `deleted_at`), but it has no way to
**erase** a person or to **hand them their data**:

- A soft-deleted row still holds `name`, `email`, `phone`, `last_active_device`, and the
  password hash.
- Personal data is spread over about 20 tables (see section 6). Several of them reference users
  through untyped `bigint` columns (`created_by`, `owner_id`, `target_id`, `tokenable_id`)
  with no foreign key, so a cascade cannot find them.
- `activity_log.changes` copies names and emails in as JSON (`user.deleted` records
  `{"name", "email"}`), as do `webhook_deliveries.payload` and `dev_requests` bodies.
- The `identity` import/export is a bulk admin CSV of *users*, not a copy of everything held
  about *one* person.
- There is no self-service route. `Service.Delete` refuses to let a user delete their own
  account.

GDPR Art. 15/20 (access, portability) and Art. 17 (erasure) need both features. Other
regimes (CCPA, UK GDPR, LGPD) need the same things. The cost of adding this grows with every
module, so the starter should own the mechanism and make each new module declare its part.

## 2. Goals and non-goals

**Goals**

1. A user can request an export of their personal data and download it as machine-readable
   JSON in a zip.
2. A user can request deletion of their account. After a grace period, their personal data is
   erased or anonymized in every module.
3. Admins (tenant and internal) and the CLI can run both for a user, so support can answer a
   data-subject request.
4. **Every table that references a user is accounted for**, and CI fails when a new migration
   adds one without saying how it is exported and erased.
5. Erasure is idempotent, auditable without keeping personal data, and can be replayed after a
   backup restore.

**Non-goals**

- Deleting a whole **tenant** (an organisation leaving). This design is about users only;
  tenant deletion is not part of it.
- Consent records, cookie banners, privacy-policy versioning: these are product or frontend
  concerns.
- Rewriting old backup archives (section 9 covers what to do instead).
- Crypto-shredding (a per-user encryption key). It would be a stronger design but is invasive,
  so it is noted as a possible future step.

## 3. Key decisions

| # | Decision | Why |
| --- | --- | --- |
| D1 | **Tombstone the `users` row, do not hard-delete it.** Keep `id`, blank every personal column, and set `erased_at` and `deleted_at`. | `created_by`/`updated_by`/`invited_by` are spread across tables without FKs, billing rows must keep a valid author, and `activity_log.actor_id` keeps meaning "the same (now anonymous) person" for audit. A tombstone holds no personal data. |
| D2 | **Each module declares its own personal data** through a `privacy.Contributor` registered in `wiring.go`. The privacy module only orchestrates. | It matches how webhooks (`RegisterEvent`) and file collections already work, and it keeps knowledge of each table with the module that owns it. |
| D3 | **A schema coverage test** lists every column that looks like a user reference, and each must belong to a contributor or to an explicit exemption with a reason. | This is the guardrail that keeps the feature correct as modules are added. Without it, erasure is quietly incomplete. |
| D4 | **Erasure runs in one database transaction across all contributors**; deleting storage objects happens after commit, with retries and an orphan sweep. | Either all the database rows are erased or none are. File deletion cannot join a transaction. |
| D5 | **Self-service deletion has a grace period** (default 14 days, the `privacy.deletion_grace_days` setting) and can be cancelled. The user can still sign in during it and sees when the deletion will happen. Admin, CLI and retention erasures may skip it. | It protects against mistakes and account takeover, and stays well inside GDPR's one-month response window. |
| D6 | **Audit rows are kept but scrubbed** (not deleted, not configurable). `activity_log` rows the user *performed* keep `actor_id` (now pointing at the tombstone) with `ip` cleared; their `changes` stay, because they describe other records ("created branch Dhaka"). Rows *about* the user, which include their `auth.login` rows (subject = the user), have `changes` replaced; failed logins with the user's e-mail are scrubbed by matching it. `activity.prune` (`ACTIVITY_RETENTION_DAYS`) still removes them when they age out. | Keeping security and accountability logs is a legitimate interest, but they do not need to name the person. Deleting them would leave holes in the tenant's audit trail. |
| D9 | **The last tenant admin cannot request deletion.** A guard refuses with "assign another admin first"; there is no role hand-over inside the request. | Erasing the only admin would lock the tenant out of user management. Hand-over is an ordinary role assignment the tenant can do first. |
| D7 | **A ledger without personal data** (`privacy_requests`) records every erasure by user id. | It gives proof of compliance and is the input for replaying erasures after a restore (section 9). |
| D8 | **Exports never include secrets**: no password hash, token hashes, 2FA secret, recovery codes or `remember_token`. | Credentials are not "data about the subject", and leaking them through a download link would be a vulnerability. |

## 4. Architecture

```
internal/privacy/                  registry and interfaces (no HTTP, no module imports)
    contributor.go                 Subject, Contributor, Guard, ExportWriter, ErasureReport
    registry.go                    Register, Contributors, coverage declarations
internal/modules/privacy/          orchestration module (handler → service → repository)
    service.go                     RequestDeletion, Cancel, Erase, StartExport, BuildExport
    jobs.go                        queue kinds privacy.erase, privacy.export
    routes.go, handler.go          self-service and admin routes
internal/modules/<each>/privacy.go one contributor per module that holds user data
internal/cli/privacy.go            ims privacy …
```

`internal/privacy` is a leaf package, like `authz` or `tenancy`. Modules import it to declare
their contributor and never import `modules/privacy`, which keeps the layering rule intact.

### 4.1 Interfaces

```go
package privacy

// Subject is the person whose data is exported or erased. Email and phone
// are captured before erasure so contributors can match rows keyed by
// address (deliveries) rather than by id.
type Subject struct {
    UserID   int64
    TenantID *int64
    Email    string
    Phone    string
}

type Contributor interface {
    // Name is the export folder and report key, e.g. "notifications".
    Name() string
    // Tables lists the user-reference columns this contributor handles,
    // as "table.column"; the coverage test checks this list.
    Tables() []string
    // Export writes the subject's records. Implementations write JSON
    // documents (and, for files, raw objects) through w.
    Export(ctx context.Context, q *dbgen.Queries, s Subject, w ExportWriter) error
    // Erase deletes or anonymizes inside the caller's transaction and returns
    // counts (never values). Storage objects to remove after commit go in
    // report.Objects.
    Erase(ctx context.Context, q *dbgen.Queries, s Subject) (ErasureReport, error)
}

// Guard may veto an erasure (last tenant admin, unpaid invoice under a
// legal hold, …). Reasons are shown to the requester.
type Guard interface {
    CheckErasure(ctx context.Context, q *dbgen.Queries, s Subject) ([]string, error)
}

type ExportWriter interface {
    JSON(name string, v any) error            // <contributor>/<name>.json
    Object(name string, r io.Reader) error    // <contributor>/<name> (binary)
}

type ErasureReport struct {
    Deleted, Anonymized int64
    Objects             []storage.ObjectRef
}
```

Contributors are registered in `wiring.go` next to the module they belong to:
`privacy.Register(identity.PrivacyContributor{})`. `Exempt("dev_queries.args", "dev-only
telemetry, erased through dev_requests.request_id")` records a deliberate exemption.

### 4.2 Coverage test

`internal/modules/privacy/coverage_integration_test.go` queries `information_schema.columns`
for user-reference columns:

- named `user_id`, `actor_id`, `impersonator_id`, `created_by`, `updated_by`, `invited_by` or
  `requested_by`;
- or polymorphic pairs (`owner_type`/`owner_id`, `target_type`/`target_id`,
  `tokenable_type`/`tokenable_id`, `model_type`/`model_id`);
- or any FK to `users(id)`.

The test fails, naming each `table.column` that is neither in some contributor's `Tables()` nor
exempted. The `add-module` skill and the scaffold generator gain a `privacy.go` stub and a
checklist line, so the failure explains itself.

## 5. Flows

### 5.1 Self-service deletion

```
POST /v1/user/privacy/deletion   {password | code}
  → DenyImpersonated, re-authenticate (5.4)
  → run Guards; any reason → 409 with the reasons
  → privacy_requests(type=erasure, status=scheduled, scheduled_for=now+grace, source=self)
  → revoke all sessions and OAuth grants (auth.RevokeAll); account stays able to sign in
  → mail + notification: "Your account will be deleted on <date>. Cancel: <link>"
  → activity "user.deletion_requested"

DELETE /v1/user/privacy/deletion    cancel while status=scheduled → status=cancelled
GET    /v1/user/privacy/deletion    current request, if any
```

During the grace period the user can still sign in. The profile response gains
`deletion_scheduled_for`, so the frontend can show a banner with "Cancel deletion". Signing in
does **not** cancel the request automatically, because a stolen password would then be enough to
stop it.

The scheduled task `privacy.erase-due` (every 15 minutes) enqueues `privacy.erase` for each due
request.

### 5.2 Erasure job (`privacy.erase`)

1. Load the request. If it is already `completed` or `cancelled`, do nothing (idempotent
   retries).
2. Load the `Subject` (email and phone are captured before they are blanked).
3. `WithTx`: run the Guards again, then each contributor's `Erase` in registration order, with
   identity (the tombstone) **last**.
4. Write `report` (counts per contributor) and `status=completed`, then commit.
5. After commit: delete the storage objects in the reports (retried, and any failures are
   logged for the orphan sweep), invalidate the permission cache, and delete cache keys for
   OTPs and sessions.
6. Record activity `user.erased` with `subject_id` only. Webhook event `user.erased`, with an
   id-only payload.
7. Send a final confirmation mail to the captured address, then forget it. The mail job payload
   is the last place the address exists; delivery rows are anonymized like the rest.

### 5.3 Export

```
POST /v1/user/privacy/export  {password | code}
  → one open export per user; at most one per 24h (429 otherwise)
  → data_processing_jobs(type=privacy_export, user_id=caller) + privacy_requests(type=export)
  → queue privacy.export
GET  /v1/data-processing/{job}/download   existing route; owner-only for privacy_export
```

The job streams a zip to the processing disk:

```
manifest.json        generated_at, user_id, contributors[], schema_version, counts
identity/profile.json
auth/sessions.json  auth/two_factor.json (method, confirmed_at only)
oauth/grants.json
access/roles.json
notifications/received.json  notifications/preferences.json
activity/performed.json      activity/about_me.json
files/avatar.<ext>  files/index.json
delivery/messages.json
settings/user_settings.json  featureflags/user_flags.json
```

When the job completes, the user gets a notification and a mail with the download link. The file
expires after `privacy.export_ttl_hours` (default 72) and is removed by the existing
`dataprocessing.cleanup-completed` task, which gets a shorter window for this job type.

### 5.4 Re-authentication

The user confirms with their password (`auth.Service.ConfirmPassword`). If 2FA is confirmed, a
current TOTP or email code is also required. Accounts that never set a password (created through
social login) confirm with an emailed one-time code using the existing cache-only OTP path
(decision 9).

### 5.5 Admin and CLI

| Surface | Route / command | Permission | Grace |
| --- | --- | --- | --- |
| Tenant admin | `POST /v1/user/{id}/erase` | `user.erase` (new) | optional `immediate` flag |
| Tenant admin | `POST /v1/user/{id}/privacy-export` | `user.privacy_export` (new) | n/a |
| Internal | `POST /v1/internal/user/{id}/erase`, `…/privacy-export` | `user.internal.erase`, `user.internal.privacy_export` | optional |
| CLI | `ims privacy erase --user ID [--now]` | operator | optional |
| CLI | `ims privacy export --user ID --out FILE` | operator | n/a |
| CLI | `ims privacy replay --since DATE` | operator | n/a (section 9) |
| CLI | `ims privacy coverage` | operator | prints contributors, tables, exemptions |

Admins cannot erase themselves or a super admin through these routes, which matches the current
rules in `Service.Delete`. An admin export goes to the admin, and the activity log records it
under the admin's id as `user.privacy_exported`.

### 5.6 Soft-deleted users

The existing `DELETE /user/{id}` stays as it is (it removes access and can be undone). A new,
optional retention task `privacy.erase-soft-deleted` erases users who have been soft-deleted
for longer than `privacy.erase_soft_deleted_after_days` (default 0, which turns it off), with
`source=retention`.

## 6. Per-table treatment

| Table (column) | Export | Erase |
| --- | --- | --- |
| `users` | profile fields, timestamps, branch, verification flags | **tombstone**: `name='Deleted user'`, `email='erased+<id>@invalid'`, `first_name/last_name/user_name/phone/last_active_device/remember_token = NULL`, `password` = unusable random hash, `is_active=0`, `deleted_at=coalesce(deleted_at, now())`, `erased_at=now()` |
| `personal_access_tokens` (`tokenable_*`) | name, device, ip, user_agent, last_used_at | delete |
| `user_two_factor` | method, confirmed_at | delete |
| `oauth_grants` (+ codes, refresh tokens by cascade) | client_name, scopes, created/last used | delete |
| `model_has_roles` (`model_*`) | role names | delete |
| `notification_reads`, `notification_targets` (`target_type='user'`), `notification_preferences` | received notifications + read state; preferences | delete |
| `notifications` (`created_by`) | — (authored for others) | keep; author resolves to the tombstone |
| `files` (`owner_type='user'`: avatar) | binary + metadata | delete row + object |
| `files` (`created_by`, owned by other entities) | index only | keep (tenant business data) |
| `deliveries` (`user_id`, `address`) | channel, template, status, sent_at | `address=''`, `user_id=NULL`; also match `address` = captured email/phone |
| `activity_log` (`actor_id`) | actions performed | keep, `ip=NULL` |
| `activity_log` (`auth.login`) | — | successful logins are rows about the user (below); failed ones matched by e-mail: `changes='{"erased":true}'`, `ip=NULL` |
| `activity_log` (`subject_type='user'`) | entries about the user | `changes='{"erased":true}'` |
| `activity_log` (`impersonator_id`) | — | `impersonator_name='Deleted user'` |
| `webhook_deliveries.payload` (subject is the user) | — | replace `data` with id-only payload |
| `data_processing_jobs` (`user_id`) | own privacy exports only | delete own `privacy_export` jobs + files; others `user_id=NULL` |
| `settings`, `feature_flags` (`target_type='user'`) | values | delete |
| `dev_requests` (`user_id`), `dev_queries`, `dev_cache_events` (by `request_id`) | — | delete (dev-only telemetry) |
| `payments`, `payment_approval_requests`, `subscriptions` (`created_by`) | — | keep (legal retention; tenant-owned) |
| `users.invited_by`, `*.created_by/updated_by` elsewhere | — | keep (they point at the tombstone) |
| cache | — | OTP keys, permission cache, unread counters |

`tenants.email`/`phone` are the organisation's contact details, not the user's, so they are out
of scope.

## 7. Data model

```sql
-- +goose Up
ALTER TABLE users ADD COLUMN erased_at timestamptz;

CREATE TABLE privacy_requests (
    id                 bigserial PRIMARY KEY,
    tenant_id          bigint REFERENCES tenants (id) ON DELETE CASCADE,
    user_id            bigint NOT NULL REFERENCES users (id),
    type               text NOT NULL CHECK (type IN ('erasure', 'export')),
    status             text NOT NULL CHECK (status IN
                         ('scheduled', 'processing', 'completed', 'cancelled', 'failed')),
    source             text NOT NULL CHECK (source IN ('self', 'admin', 'cli', 'retention')),
    requested_by       bigint REFERENCES users (id) ON DELETE SET NULL,
    scheduled_for      timestamptz,
    processing_job_id  bigint REFERENCES data_processing_jobs (id) ON DELETE SET NULL,
    report             jsonb NOT NULL DEFAULT '{}'::jsonb,  -- counts only, never values
    error              text,
    completed_at       timestamptz,
    cancelled_at       timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX privacy_requests_open_erasure_unique ON privacy_requests (user_id)
    WHERE type = 'erasure' AND status IN ('scheduled', 'processing');
CREATE INDEX privacy_requests_due_idx ON privacy_requests (scheduled_for)
    WHERE type = 'erasure' AND status = 'scheduled';
CREATE INDEX privacy_requests_tenant_idx ON privacy_requests (tenant_id, created_at DESC);
```

The `users_email_unique` partial index already excludes `deleted_at IS NOT NULL`, so an erased
person can sign up again with the same address. Sign-in queries already filter
`deleted_at IS NULL`, so a tombstone can never authenticate.

## 8. Settings, permissions, events

- **Settings** (registry, admin-editable): `privacy.deletion_grace_days` (14),
  `privacy.export_ttl_hours` (72), `privacy.erase_soft_deleted_after_days` (0).
- **Permissions** (catalog, `make docs-gen`): `user.erase`, `user.privacy_export`,
  `user.internal.erase`, `user.internal.privacy_export`. Self-service routes need only
  authentication plus `DenyImpersonated`.
- **Activity actions**: `user.deletion_requested`, `user.deletion_cancelled`, `user.erased`,
  `user.privacy_exported`. All of them are id-only.
- **Webhook events** (`RegisterEvent`): `user.erased`, so downstream systems can erase their
  copies too.
- **Queue kinds**: `privacy.erase`, `privacy.export`. Both are tenant-scoped, except internal
  users, which are `SystemJob` and carry the user id.
- **Scheduled tasks**: `privacy.erase-due` (every 15 min), `privacy.erase-soft-deleted` (daily,
  no-op when off).
- **Rate limits**: export 1 per 24 h, deletion request 5 per hour per user.

## 9. Backups, logs, external systems

- **Backups.** Archives cannot be edited. The documented position is: backups expire under the
  existing retention (section "Retention" in [Backups](../operations/backups.md)), and after any
  restore the operator runs `ims privacy replay --since <backup time>`. That command reads
  `privacy_requests` (completed erasures) from the live database, *before* the restore
  overwrites it (the runbook exports the ledger first), and erases those users again. The
  restore runbook in [Rollback](../operations/rollback.md) gains this step.
- **Logs and traces.** Structured logs must not contain email or phone. This is already the
  intent of the redaction tests, and this work adds an `email`/`phone` key check. Log and trace
  retention is an operations setting and is documented, not enforced by the app.
- **Mail/SMS providers and other processors** keep their own copies. The `user.erased` webhook
  and a documented list of processors are the starter's part; contracts are the adopter's.

## 10. Security considerations

- Re-authentication on both features (section 5.4), and refused while impersonating.
- An export download is owner-only, expires, and is served from the existing authenticated
  download route. There is no public signed URL.
- Exports never contain secrets (D8). A unit test asserts that the zip has no field named
  `password`, `secret`, `token`, `recovery_codes` or `remember_token`.
- Guards stop someone who has taken over an account from erasing the last tenant admin and
  locking the tenant out. The grace period plus the cancel link in the mail covers takeover of
  ordinary accounts.
- Activity rows record who requested what (`requested_by`) without recording personal data.

## 11. Testing

- Coverage integration test (section 4.2).
- Per-contributor integration tests: seed a user with data in that module, erase, then assert no
  row still holds the seeded email, phone or name (a generic `assertNoPII(t, db, needles...)`
  helper that scans every text and jsonb column of the covered tables).
- An end-to-end testkit flow: request deletion, try a second request (409), cancel, request
  again, advance time, run the due task, then check that the tombstone exists, sign-in fails, the
  email can be reused, and the webhook was delivered.
- Export: the zip layout matches the manifest, the secret-field check passes, and another user
  gets a 404 on the download.
- Idempotency: run the erase job twice and get the same final state.
- Replay: erase, restore an older snapshot in the test database, replay, and assert no personal
  data remains.

## 12. Rollout plan

1. **Foundation.** Add `internal/privacy`, the migration, contributors for the tables in
   section 6, the coverage test, and `ims privacy erase|export|coverage`. Nothing is exposed over
   HTTP yet; this alone answers data-subject requests through support.
2. **Self-service and admin HTTP.** Add the routes, grace period, scheduled tasks, mail and
   notification templates (i18n), the webhook event, the OpenAPI contract and the permissions.
3. **Developer experience.** Add the scaffold `privacy.go` stub, `add-module` skill
   step, `guides/privacy.md`, decisions entries, an optional dashboard panel (open requests,
   coverage view), the retention task for soft-deleted users, and `ims privacy replay` with the
   runbook change.

## 13. Resolved questions

| Question | Decision |
| --- | --- |
| Tombstone or hard delete? | Tombstone (D1). |
| Signing in during the grace period | Allowed, with the scheduled date shown so the user can cancel (D5). Signing in does not cancel. |
| Activity log | Keep the rows and scrub personal data, including `auth.login` rows; not configurable (D6). |
| Last tenant admin | Refuse until another admin is assigned (D9). |
| Grace period length | 14 days by default, changeable through the `privacy.deletion_grace_days` setting. |
| Tenant deletion | Out of scope; this design is about users only. |

## 14. As built

Where the code differs from the sections above:

- **Re-authentication is the password** (5.4), as for turning two-factor off. Every account has a
  password (social login only signs existing users in, invitations set one), so there is no
  mailed-code path; the grace period and the mail cover account takeover.
- **The scheduled-deletion mail says how to cancel** (sign in, then
  `DELETE /api/v1/user/privacy/deletion`) instead of carrying a cancel link: the starter has no
  client page to link to. The profile response does not gain `deletion_scheduled_for`;
  `GET /api/v1/user/privacy/deletion` answers it.
- **Due erasures run in the scheduled task** (`privacy.erase-due`), not as a `privacy.erase` queue
  job: each is one transaction, and admin and CLI erasures already run inline. The only queue
  job is `privacy.export`. A refused or failed scheduled erasure is marked `failed` and not
  retried.
- **Exports expire through their own task** (`privacy.expire-exports`, hourly), since the
  lifetime is in hours. The download is the existing data-processing route, made owner-only for
  `privacy_export` jobs. Only self-service exports have the one-a-day limit; the deletion
  request has no separate rate limit beyond the duplicate-request guard.
- **Replay reads a saved file** (`ims privacy:ledger --since` before the restore,
  `ims privacy:replay --from` after), because the restore replaces the ledger it would read.
- **Activity rows the user performed keep their changes** and lose only the IP (D6): their
  changes describe other records. Rows about the user, including their logins, are scrubbed.
- **No scaffold stub:** modules from `ims gen module` only have `created_by`/`updated_by`, which
  the identity contributor already covers; the `add-module` skill and the coverage test handle
  anything else. **No dashboard panel:** `privacy:coverage` is a dashboard command (Commands
  panel, Info) instead; open requests are in `privacy_requests`.
