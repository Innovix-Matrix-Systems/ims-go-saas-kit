# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org) (see `VERSION`).

## [Unreleased]

**Upgrading:** run `ims migrate` (migrations `00037_create_onboarding`: `tenant_onboarding` and
`user_agreements`; `00038_user_locale`: `users.locale`; `00039_tenant_owner`: `tenants.owner_user_id`;
`00040_payment_cancelled`: payments may be `cancelled`), `ims permission:sync` (adds `billing.view`, `billing.manage` and `organization.update`, and now also
grants every tenant Admin role the permissions it is missing) and `ims settings:sync` (the new
**Sign-up** and **Billing** groups).

### Changed

- `POST /api/v1/billing/subscribe` needs `billing.manage` and `GET /api/v1/billing/payments`
  needs `billing.view` (were the system permissions `subscription.manage` and `payment.view`,
  which no tenant role could hold).
- Signing in no longer checks the password's length (it only must be sent), so accounts with
  passwords from older rules keep working; setting a password follows the new rule below.
- `PATCH /api/v1/user/{id}` and `POST /api/v1/user/profile/update` keep `first_name`,
  `last_name` and `phone` when they are omitted or null (they were cleared); send an empty string
  to clear one.
- Subscribing to a free package starts it at once, with no payment (`payment_method` is only
  required for paid packages and `payment` is null).
- A staff account's notification feed no longer includes every organization's organization-wide
  notifications; those reach that organization's members (and staff acting in it) only.
- A tenant's Admin role is managed: it cannot be renamed, deleted or given other permissions
  (403), and no other role can be renamed to Admin. Role responses carry `managed`.
- `POST /api/v1/role/assign-permission` only adds permissions the caller holds (403 otherwise;
  removing is always allowed; Super-Admins are not capped).
- `DELETE /api/v1/branch/{id}` answers 409 for an organization's last branch and for a branch its
  members belong to; members who had switched into a deleted branch go back to their own.

### Added

- **Client app** (`web/app`): a React single-page app for the people who use the API, embedded in
  the binary and served at the site root. It has sign-in with two-factor codes and recovery codes,
  "keep me signed in", a collapsible sidebar, a Ctrl+K command palette, keyboard shortcuts, light
  and dark themes with accent and density presets (shared design tokens with the developer
  dashboard), and English and Bangla. Ended sessions, required two-factor setup, suspended
  organizations and maintenance mode each get their own screen.
- Client app account screens: sign-up (with terms acceptance), e-mail verification and a new
  link, forgot and reset password, accepting an invitation (shows the organization and the
  inviter), confirming an e-mail change, forced two-factor setup, and a first-run setup wizard for
  new owners (choose a plan with bank-transfer payment, invite the team, turn on two-factor).
  Account settings: profile with the saved language, e-mail change, password change, two-factor
  (authenticator app with QR code or e-mail codes, recovery codes), signed-in devices,
  notification preferences, personal-data export and account deletion. Signing in applies the
  user's saved language and sends a new owner to the setup wizard.
- Organizations withdraw a pending checkout (`POST /api/v1/billing/payments/{id}/cancel`) and
  download invoices (`GET /api/v1/billing/payments/{id}/invoice`, printable HTML);
  `GET /api/v1/packages/features` lists the feature catalog; staff send announcements to every
  or chosen organizations (`POST /api/v1/internal/notification/broadcast`, event
  `platform.announcement`); `GET /api/v1/internal/notification` filters by `event_type`.
- `GET /api/v1/user/profile/permissions` returns the effective user's permissions, the target's
  while staff act as a user; `GET /api/v1/user?platform=true` lists staff accounts; payment
  approvals carry `details` (organization, plan, amount, invoice) and filter by `status`.
- `GET /api/v1/billing/usage` (`billing.view`) reports an organization's use of each plan limit,
  and the global setting `billing.payment_instructions` holds the bank details shown at checkout.
- `GET /api/v1/activity` filters by `action`, `from`, `to` and `search` and reports `meta.total`;
  `GET /api/v1/activity/facets` lists the actions, subject types and people in the feed with
  counts, and the entries of the last 24 hours.
- `GET /api/v1/role/{id}` includes the role's `permissions`, and
  `GET /api/v1/role/grantable-permissions` (`role.update`) lists the permissions the caller may
  put on a role.
- Client app organization screens: members (search, invited or joined filter, invite with roles,
  edit, deactivate, resend an invitation, reset two-factor, remove) and organization settings
  (profile and contact details, the owner and ownership transfer, and the organization's own
  values for tenant settings such as branding, two-factor and privacy), roles (create, rename,
  delete, and a permission editor grouped by area that offers only what the editor may grant; the
  managed Admin role is read-only) and branches (create, edit with a manager, delete), plus a
  branch switcher in the top bar and moving a member to another branch. Also the activity log
  (filter by subject and member, details, load more), files (upload, filter by type, download,
  delete) and webhooks (endpoints with an event picker, the signing secret shown once, test
  event, turn on or off, new secret, delivery log with payloads and sending again). Every list
  has a pagination footer (count, rows per page, pages); the activity log follows the developer
  dashboard's audit log (counters, search, action, person, area and date filters, a details
  drawer with previous and next, CSV export); dropdowns use the custom select, and checkboxes
  and radios are drawn to match the theme. Billing shows the current plan, usage of each limit,
  the plans with a bank-transfer checkout (with the bank details) and the payment history; the
  setup wizard's plan step uses the same checkout. A staff console for platform accounts:
  organizations (search, create, details with owner, plan, usage and members, suspend or
  reactivate, change plan with an optional recorded payment, impersonate), users across every
  organization with log in as, payment approvals (approve or reject with a note), plans with a
  feature editor, staff accounts (invite with a platform role) and platform settings (including the
  bank details). Impersonating an organization and logging in as a user each show a banner with
  the way back; logging in as a user renders the app as that user sees it. Billing adds invoice
  downloads, withdrawing a payment that waits for confirmation, and moving to a free plan at once;
  the console adds feature flags (for everyone or one organization, with rollout and reset) and
  announcements (to every or chosen organizations); staff get a platform home with counts; the
  plan editor reads the feature catalog from the API; the date picker and select follow the
  app's language. The activity log and notifications page with cursors (previous and next, rows
  per page); the console has an audit log of every organization and the system log; the per-page
  picker opens upwards when needed and is no longer cut off; settings pages show descriptions, not
  setting keys; a staff account reads its own notifications through the internal routes.
  The sidebar is resized by dragging its edge (remembered; double-click resets) and pinned open or
  folded to a rail with a pin button beside the logo.
- The binary serves the client app at `/` and on every path outside the API and tool prefixes
  (`/api`, `/dev`, `/docs`, `/storage`, `/mcp`, `/oauth`, `/.well-known`, `/metrics`, `/up`),
  so client routes and links in e-mails open the app; unknown API paths still answer a JSON 404.
  The app's HTML is sent with a strict `Content-Security-Policy` (same-origin scripts, styles and
  connections only, no inline code).
- **Self-service sign-up** ([guide](docs/guides/sign-up.md)): `POST /api/v1/signup` creates an
  organization, starts it on the sign-up package and creates its owner, who confirms the address
  before signing in. It never reveals whether an address already has an account (that owner is
  mailed instead), records the accepted terms and privacy-policy version, and is limited to ten
  attempts per IP an hour. Settings `signup.enabled`, `signup.package_id`, `signup.terms_version`.
- **Change your own e-mail address** ([guide](docs/guides/authentication.md#changing-the-e-mail-address)):
  `POST /api/v1/user/profile/change-email` mails a confirmation link to the new address
  (`AUTH_EMAIL_CHANGE_URL`); `POST /api/v1/email/change/confirm` switches the account, tells the old
  address and signs out every session.
- **Invitation preview**: `POST /api/v1/invitation/preview` shows the invitee, the organization
  and the inviter without using the link up.
- **Language per user**: `locale` on the profile (read and update).
- **The organization and its owner** ([guide](docs/guides/tenancy.md#the-organization-and-its-owner)):
  `GET/PATCH /api/v1/organization` let members read and admins (`organization.update`) edit their own
  organization; `POST /api/v1/organization/transfer-ownership` lets the owner hand it over. The owner
  (`tenants.owner_user_id`, set at sign-up and backfilled for signed-up organizations) cannot be
  deleted, deactivated, demoted, have their password or two-factor reset by others, or be erased
  until ownership is transferred.
- **First-run setup**: `GET /api/v1/onboarding` and `POST /api/v1/onboarding/complete` (owner only)
  track a new organization's setup wizard.
- Tenant billing permissions `billing.view` and `billing.manage`, held by the tenant Admin role:
  an organization can now see its payments and choose or pay for a plan itself.
- `ims db:seed` gives the default tenant the hidden **Unlimited** package when it has no active
  plan, so a fresh install can add users.
- One password rule wherever a password is chosen (sign-up, invitation, reset, own change, admin
  change, import): at least 8 characters, at most 72 bytes, not only spaces, and not one of the
  most common passwords.
- `make dev` also starts the client app's Vite server on `:8082` (once `npm --prefix web/app ci`
  has run); new targets `app-build`, `app-typecheck`, `app-style-check`, `app-openapi-check` and
  `dev-app`; `build-all`, the Dockerfile and CI build the client app too.

## [0.2.1] - 2026-11-23

Account deletion and personal-data export (the right to erasure and the right of access):
users can delete their account after a grace period or download everything the application
holds about them, admins and the CLI can do both for a user, and every module declares its own
personal data so nothing is missed.

**Upgrading from 0.2.0:** run `ims migrate` (migration `00036_privacy_requests`: the
`privacy_requests` ledger and `users.erased_at`), then `ims permission:sync` (four new
permissions; tenant Admin roles gain `user.erase` and `user.privacy_export`) and
`ims settings:sync` (the new **Privacy** group). Restart the `ims queue` and `ims schedule`
processes: there is a new queue job (`privacy.export`) and three new scheduled tasks. A backup
restore now needs one extra step: see [Backups](docs/operations/backups.md).

### Added

- **Account erasure** ([guide](docs/guides/privacy.md), design:
  `docs/design/account-deletion-and-export.md`). The `users` row stays as a tombstone ("Deleted
  user", `erased+<id>@invalid`, no phone or usable password, `erased_at` set) so audit stamps
  and invoices keep an author; sessions, two-factor, roles, MCP connections, user-owned files,
  notification links, user settings and flags, and the user's exports are deleted; the activity
  log, deliveries, webhook payloads and the dashboard audit log are kept but scrubbed. The super
  admin and a tenant's last active Admin are refused.
- **Self-service deletion with a grace period:** `POST /api/v1/user/privacy/deletion` (password
  required) schedules the erasure after `privacy.deletion_grace_days` (default 14), signs every
  session out and mails the date; `GET` shows it and `DELETE` cancels it. The
  `privacy.erase-due` task (every 15 minutes) runs the due ones.
- **Personal-data export:** `POST /api/v1/user/privacy/export` (password required, once a day)
  queues a zip with one JSON folder per module, the user's files and a manifest — never
  passwords, tokens or two-factor secrets — and mails the download link. Only whoever asked can
  download it; it expires after `privacy.export_ttl_hours` (default 72, `privacy.expire-exports`).
- **Admin routes:** `POST /api/v1/user/{id}/erase` (`user.erase`; scheduled, or `immediate`),
  `DELETE /api/v1/user/{id}/erase` (cancel) and `POST /api/v1/user/{id}/privacy-export`
  (`user.privacy_export`), with `internal` twins (`user.internal.erase`,
  `user.internal.privacy_export`). A user cannot cancel an erasure an admin scheduled.
- **Retention erasure:** `privacy.erase_soft_deleted_after_days` (off by default) erases users
  deleted longer ago (`privacy.erase-soft-deleted`, daily).
- **CLI:** `ims privacy:erase --user=ID --force`, `privacy:export --user=ID --out=FILE`,
  `privacy:coverage`, and `privacy:ledger` / `privacy:replay` to erase again the users a backup
  restore brings back. `privacy:coverage` is also a dashboard command.
- **A privacy contributor per module** (`internal/privacy`, registered in
  `internal/app/privacy.go`): `TestCoverage` and `ims privacy:coverage` fail when a migration
  adds a column that may hold a user id and no contributor or exemption covers it. Every
  erasure and export is a `privacy_requests` row (ids and counts only) and an id-only
  `user.erased` / `user.privacy_exported` activity entry; `user.erased` is a webhook event.
- Mail templates `privacy-deletion-scheduled`, `privacy-account-erased` and
  `privacy-export-ready` (English and Bangla).

### Changed

- `privacy_export` processing jobs are listed, visible and downloadable only to their owner,
  whatever the caller's data-processing permissions.

## [0.2.0] - 2026-11-23

The developer dashboard, redesigned: calm light and dark themes with an accent you
choose, a status-first Overview, a Scheduler panel, a keyboard for everything, and every panel
on one page template, with the fixes found on the way.

**Upgrading from 0.1.9:** run `ims migrate` (migration `00035_schedule_runs`, the scheduler run
history) and restart the `ims schedule` process so it records each run. `SCHEDULE_HISTORY_DAYS`
(default `30`) sets how long runs are kept. Run `ims permission:sync` (four new
`file.internal.*` permissions; `file.view.all` and `file.delete` become tenant permissions).
Existing tenant Admin roles do not gain the `file.*` permissions by themselves — grant them where
tenant admins should manage files. Platform clients that used `/api/v1/files` for global files
move to `/api/v1/internal/files`.

### Added

- **Themes and accents:** light, dark and system themes, five accent presets (indigo by default;
  a fork changes one line in `web/dev/src/styles/tokens.css`) and a compact density, all in the
  top bar's appearance menu, kept per browser and applied before first paint.
- A **status-first Overview**: health, migrations, queue, errors in 24 hours, last backup, and
  settings or flags missing from the database; the last hour of traffic as a per-minute chart
  (2xx/3xx, 4xx, 5xx) with the error rate and p95; every health check next to recent activity;
  errors and failed jobs; the environment and its Go packages. `GET /dev/api/overview`
  (`dev.view`) serves it in one call.
- A **Scheduler** panel: every scheduled task with its schedule in words, next run and last ten
  runs, a **History** of every run filtered by task and status, and Run now (`dev.manage`, audited
  as `schedule.run`). Every run is recorded in the new `schedule_runs` table (status, how it was
  started, duration, error; `interrupted` when the scheduler stopped mid-run) and kept for
  `SCHEDULE_HISTORY_DAYS` (default `30`, pruned nightly by `scheduler.prune-history`); the
  every-minute heartbeat keeps none (`NoHistory`).
  `GET /dev/api/scheduler`, `GET /dev/api/scheduler/runs` and `POST /dev/api/scheduler/{id}/run`
  serve the panel.
- A **keyboard layer**: G then a letter opens a panel, `/` focuses search, R refreshes, `[`
  collapses the sidebar, `?` lists every shortcut; Escape closes the top overlay only.
- Settings groups can be created, renamed and (custom ones) deleted from the All settings view.
- `/dev/kit` (local only): every dashboard component in every tone.
- `npm run check:style` / `make web-style-check` (and a CI step): WCAG AA contrast for every
  token pair in both themes and every accent, the six-step type scale, no raw colours.
- **Internal file routes** `/api/v1/internal/files` (list, upload, show, download, delete) for
  platform accounts, with the system permissions `file.internal.view`, `file.internal.view.all`,
  `file.internal.create` and `file.internal.delete`. Uploads there are global files.

### Changed

- Dashboard: calmer and easier to read. Neutral surfaces, colour only for meaning (green healthy,
  red failed, amber needs a look; the accent for what is selected or on), 14 px content with six
  font sizes, sentence case, no idle animation. "Telescope" is now **Requests**; the sidebar is
  grouped Monitor, Operate, Configure, Data, Diagnose, Reference and Sandbox, collapses to an
  icon rail and becomes a menu on phones; production shows a red line across the top.
- Every panel moved to one page template and component kit, with fixes found on the way:
  request details with a summary strip and SQL totals (slowest and repeated statements);
  exceptions grouped by kind; log rows that open the entry and link the request; queue tiles,
  state tabs and a `?state=` link; job actions that always confirm; a delivery drawer; backups
  with the setup on top and runs that load as you scroll (failed ones show their error); one
  colour per setting or flag source everywhere.
- The compose `otel` profile runs Jaeger (`jaegertracing/jaeger`, UI on :16686) instead of
  Grafana's `otel-lgtm`, which stays in the file, commented, for teams that prefer Grafana. The
  tracing guide shows Jaeger screenshots and how to run the Jaeger binary without Docker.
- `/api/v1/files` is now a tenant route: tenant Admin roles get the `file.*` permissions, and
  reads and deletes are scoped to the effective tenant.

### Fixed

- `/api/health` could cache "context canceled" failures for every caller for 30 seconds when the
  request that started a check run was cancelled (a browser aborting the fetch). The shared run
  no longer depends on one caller's connection; each check keeps its own timeout.
- A super admin uploading with `X-Acting-Tenant` / `X-Acting-Branch` created a global file: the
  files routes ignored the acting headers. Uploads now land in the acting tenant, and a platform
  upload without acting headers is refused (use `/api/v1/internal/files`).
- The dashboard's Files panel took a file's tenant from its uploader, so anything a platform
  account uploaded showed as "Internal" and was missing from the tenant filter. It now reads the
  file's own tenant.
- A scheduled task that panicked took its process down with it: the API for a dashboard Run now,
  `ims schedule` for a scheduled run. A panic is now a failed run with the panic as its error.
- Run now on a task that is still running (here or in the `ims schedule` process) is refused with
  409 instead of reporting a run that never happened. A manual run lost with its process no
  longer shows as running forever: it stops counting after the task's timeout and is closed as
  `interrupted` when the scheduler next starts.
- `ims permission:sync` never updated whether a permission is system-only, so a permission the
  catalog moved to tenants stayed system-only on existing databases and was stripped from tenant
  roles. The sync now applies the catalog's flag.

## [0.1.9] - 2026-11-22

Production essentials: one delivery system for mail, SMS and push, two-factor authentication,
invitations, sessions users manage, notification channels and preferences, duplicate-request
protection, outbound webhooks, OpenTelemetry tracing, and suspended tenants that are really
locked out. 0.1.8 was not released; this follows 0.1.7.

**Upgrading from 0.1.7:** set `APP_KEY` (`ims key:generate`; staging and production refuse to
start without it), run `ims migrate` (migrations `00029_deliveries`, `00030_token_client_info`,
`00031_user_two_factor`, `00032_user_invitations`, `00033_notification_channels`,
`00034_webhooks`), `ims settings:sync` (the new Branding and Security settings) and
`ims permission:sync` (`user.two_factor.reset`, `webhook.manage`). Projects that called
`authn.NewRecovery` or `OTPService.WithMailer` pass an `authn.Deliverer` instead (see
`internal/app/delivery.go`). Tracing stays off until `OTEL_EXPORTER_OTLP_ENDPOINT` is set.

### Added

- **One delivery system for mail, SMS and push** (`internal/platform/delivery`). Every message to
  a person goes through `delivery.Service.Send`: templates are folders (`subject.txt`,
  `mail.txt`, optional `mail.html`, `sms.txt`, `push.json`) with locale variants (`mail.bn.txt`),
  plain text works alone, a missing data key fails at `Send`, and one `delivery.send` job per
  channel retries on its own. OTP codes, password resets and e-mail verification now send
  branded HTML + text mail in English or Bangla. See the [delivery guide](docs/guides/delivery.md).
- **Delivery log** (`deliveries` table): template, channel, masked address, status, attempts and
  error for every message, never a body; pruned after `DELIVERY_RETENTION_DAYS` (default `30`) by
  the `delivery.prune` task or `ims delivery:prune`.
- **SMS** (`internal/platform/sms`): `SMS_DRIVER=log` (default) or `twilio`
  (`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM`).
- **Mail**: HTML with a text alternative, CC, BCC, Reply-To and attachments from storage disks
  (`MAIL_MAX_ATTACHMENT_MB`, default `10`); subjects and names are RFC 2047 encoded.
- **Branding settings** (`branding.name`, `branding.logo_url`, `branding.color`,
  `branding.footer`) for the mail layout; a tenant's own values need the `custom.branding` plan
  feature.
- **Outbound webhooks** (`internal/modules/webhooks`): tenants subscribe URLs to events
  (`/api/v1/webhook*`, `webhook.manage`, plan `api.access`); every matching activity-log entry is
  POSTed signed per Standard Webhooks, retried for about a day, logged (`/deliveries`,
  `/redeliver`, `/test`), and an endpoint failing `WEBHOOK_DISABLE_AFTER` times in a row is
  disabled and its creator notified. Secrets are encrypted with `APP_KEY` and rotate with a day of
  overlap. https only and no private or metadata addresses outside local development
  (`WEBHOOK_ALLOW_PRIVATE`). `user.created` and `branch.updated` are the example events;
  `webhooks.RegisterEvent` adds more. See the [webhooks guide](docs/guides/webhooks.md).
- `activity.Recorder.WithSink`: every stored entry can feed another system.
- **OpenTelemetry tracing** (`internal/platform/tracing`), off until `OTEL_EXPORTER_OTLP_ENDPOINT`
  is set: a span per request named by route, SQL spans named by sqlc query, queue jobs linked to
  the request that queued them (even across processes), scheduled tasks, and outbound HTTP
  (webhooks, Twilio, bKash, social sign-in, MCP client metadata) passing `traceparent` on. Panics
  and 5xx mark the span failed, and log lines inside a span carry `trace_id` and `span_id`.
  Parent-based 10% sampling by default (`OTEL_TRACES_SAMPLER_ARG` sets the ratio,
  `OTEL_TRACES_SAMPLER` replaces it); a caller's trace is continued only through a trusted proxy.
  Example spans in branches and billing; `tracing.Start` / `tracing.Fail` for modules. A compose
  `otel` profile runs Grafana with Tempo locally, and `ims about` shows the state. See
  [health and observability](docs/guides/health-and-observability.md#tracing-opentelemetry).
- **Duplicate-request protection** on every write behind sign-in (`internal/platform/dedupe`):
  the same user's identical write while it runs, or within `DEDUPE_WINDOW` (default `3s`) after it
  succeeded, answers `409 DUPLICATE_REQUEST`; money routes keep a 30 s window. Clients may send
  `Idempotency-Key` to have the first answer replayed for `IDEMPOTENCY_TTL` (default `24h`,
  `Idempotent-Replayed: true`). New error codes `DUPLICATE_REQUEST`, `IDEMPOTENCY_IN_PROGRESS`,
  `IDEMPOTENCY_KEY_REUSED`. `DEDUPE_ENABLED=false` turns it off.
- **Notifications by mail, SMS and push:** `channels` on a new notification delivers it through
  the delivery system (template `notification`) to every user fan-out newly addresses, besides the
  feed. Users opt out per event type (`GET/PUT /api/v1/notification/preferences`, internal twins)
  unless the event is mandatory; mails carry a sealed one-click unsubscribe link
  (`/api/v1/notification/unsubscribe`, `List-Unsubscribe` headers). SMS needs the plan's
  `sms.notifications` and a verified phone, capped by `SMS_DAILY_LIMIT_PER_TENANT`. Event types
  are registered with `notifications.RegisterEvent`; renewal reminders are now also mailed.
- **Invite a user instead of choosing their password:** `send_invitation: true` on
  `POST /api/v1/user` creates the user inactive and mails a link (`AUTH_INVITATION_URL`, valid
  `AUTH_INVITATION_TTL`, default 7 days); `POST /api/v1/invitation/accept` sets the password,
  activates the account and verifies the address. Resend with `POST /api/v1/user/{id}/invitation`
  (and the internal twin), list with `?invitation=pending|accepted`; users carry
  `invitation_status`. `user.invited` / `user.invitation_resent` / `user.invitation_accepted`
  activity. `testkit.App.Queued` returns the data of a queued message (a link or a code) for
  tests.
- **Two-factor authentication**, by authenticator app (TOTP) or by e-mail code, with eight
  recovery codes: `GET/POST/DELETE /api/v1/two-factor`, `POST /api/v1/two-factor/confirm`,
  `POST /api/v1/two-factor/recovery-codes`. Sign-in asks for the code through the existing
  `OTP_REQUIRED` → `/verify-otp` step (`method`: `totp` or `email`; recovery codes in the same
  field), including social logins, the MCP sign-in page and the dashboard (which now has a code
  screen). Security mails on enable, disable and recovery-code use; `user.two_factor_*` activity.
- **Requiring two-factor:** setting `security.require_two_factor` (per tenant) and
  `AUTH_REQUIRE_2FA_PLATFORM`; until a user enrols, sign-in gives a setup-only token
  (`TWO_FACTOR_SETUP_REQUIRED`), and confirming returns a full one. New error code
  `TWO_FACTOR_SETUP_REQUIRED` (403).
- **Admin reset:** `DELETE /api/v1/user/{id}/two-factor` (`user.two_factor.reset`, in the tenant
  Admin role) and `ims two-factor:reset --email`.
- **`APP_KEY`** (`APP_PREVIOUS_KEYS` for rotation) and `internal/platform/crypto`: values the app
  must read back are AES-256-GCM encrypted; `ims key:generate` writes a key to `.env`. The data of
  sensitive delivery jobs (codes, links) is now sealed in the queue.
- **Users manage their own sessions:** `GET /api/v1/sessions` (device, IP, browser, last used,
  `current`), `DELETE /api/v1/sessions/{id}` and `POST /api/v1/sessions/revoke-others`, recorded
  as `auth.session_revoked`. Tokens now remember the IP and user agent they were issued to.
- **New sign-in alert** (setting `security.new_sign_in_alert`, off by default): a mail when a user
  signs in from an IP address not used in the last 90 days.
- `auth.prune-tokens` scheduled task: deletes expired tokens daily, and sessions unused for
  `AUTH_SESSION_IDLE_DAYS` (default `0` = never).
- `ims delivery:test` sends one message now through the configured driver; it and
  `delivery:prune` are also dashboard commands.
- Developer dashboard **Deliveries** page: the log with filters and a template preview for each
  channel and language (`/dev/api/deliveries`, `/dev/api/deliveries/templates`,
  `/dev/api/deliveries/templates/{name}/preview`).
- `BILLING_SUSPEND_ON_EXPIRY` (default `false`) and `BILLING_SUSPEND_GRACE_DAYS` (default `3`):
  the `billing.expire-subscriptions` task can suspend tenants whose subscription expired with
  nothing current or pending. The default tenant is never suspended.
- `tenant.suspended` / `tenant.reactivated` system rows in the activity log.

### Changed

- `MAIL_PORT` defaults to `1025` (Mailpit) instead of `2525`.
- `authn.Authenticator` has `IssueSetup` (setup-only tokens); custom implementations add it.
- `authn.MailEnqueuer` is replaced by `authn.Deliverer`; `OTPService.WithMailer` is now
  `WithDeliverer` and `OTPService.Send` takes the user record.
- The dashboard's queue panel hides `delivery.*` job payloads, like `mail.*`.
- `Idempotency-Key`, `traceparent` and `tracestate` are in the default `CORS_ALLOWED_HEADERS`.
- The access log is written with the request's context, so log handlers see its trace.
- `notifications.EventTypes` is replaced by `notifications.Events` / `RegisterEvent`.

### Fixed

- **Suspended tenants are locked out.** A tenant with `is_active = false` kept working; now its
  users get `403 TENANT_SUSPENDED` on every authenticated route (REST, dashboard API and MCP tool
  calls) and at sign-in. Logout stays open, platform accounts can still act inside the tenant, and
  reactivating restores access with the existing tokens. New error code `TENANT_SUSPENDED` (403).
  See [Suspending a tenant](docs/guides/tenancy.md#suspending-a-tenant).

## [0.1.7] - 2026-11-22

Maintenance mode: take the API down and bring it back with one switch, from the CLI or the
developer dashboard.

**Upgrading from 0.1.6:** run `ims migrate` (migration `00028_maintenance`). No new permission
(the toggle uses `dev.manage`). Nothing changes until someone runs `ims down`.

### Added

- **Maintenance mode.** `ims down [--message --retry --secret | --with-secret]`, `ims up` and
  `ims maintenance:status` take the API down and bring it back, like Laravel's `artisan down`.
  While down, `/api/v1`, `/mcp` and `/oauth` answer `503 MAINTENANCE_MODE` with `Retry-After` and
  your message; `/up`, `/api/health`, `/metrics`, `/dev` and `/docs` keep working. A client
  sending the bypass secret in `X-Maintenance-Bypass` passes. The state is one database row every
  process re-reads every `MAINTENANCE_CHECK_INTERVAL` (default `5s`), so it works across servers.
  New error code `MAINTENANCE_MODE` (503). See the
  [maintenance mode guide](docs/guides/maintenance-mode.md).
- **Maintenance in the developer dashboard:** a red **Maintenance** pill in the top bar while
  down, a **Maintenance mode** card with the toggle on Inspector → System (`dev.manage`, audited),
  `down` / `up` / `maintenance:status` in the Commands panel and Ctrl+K, and
  `GET /dev/api/maintenance`, `POST /dev/api/maintenance/down|up`.
- `GET /api/health` has a `maintenance` block (`down`, `since`, `message`, `retry_after`); its
  status code does not change while down.
- `maintenance.down` / `maintenance.up` entries in the system activity log, written through the
  new `maintenance.Audit` interface (implemented by `activity.Recorder`).
- Scheduled tasks are skipped while down unless they set `EvenInMaintenance` (the scheduler
  heartbeat and the backup tasks do); `scheduler.Runner.WithMaintenance` wires the check.
  `ims schedule:run` always runs. Queue workers keep running.
- `ims activity:prune [--days]`, the CLI twin of the dashboard command and the `activity.prune`
  scheduled task.
- **`ims route:list`**, like Laravel's: every HTTP route with the permission it requires and its
  handler, from the same walker as the dashboard's Inspector → Routes. Filters `--method`,
  `--path` and `--except-path`; `--middleware` adds each route's middleware (and prints the global
  stack once); `--json` prints the full table for scripts and AI agents.
- **A guide to CLI commands** ([CLI commands](docs/guides/cli-commands.md)): how the `ims` command
  tree is registered (`BuildRoot`'s registrars list) and how to add your own command, with its
  smoke test, the generated reference, and the matching dashboard command or scheduled task. The
  CLI reference links to it, and AI agents get an `add-cli-command` skill.
- **A "Coming from Laravel" page** ([Coming from Laravel](docs/getting-started/coming-from-laravel.md)):
  what the starter takes from Laravel (the workflow, artisan → `ims`, the packages it replaces),
  what it takes from Go, the "magic" it leaves out on purpose and what you will miss, and how the
  developer dashboard covers Telescope, Horizon and more. The tagline is now "Laravel's batteries
  and workflow. Go's runtime, without the magic." across the README and the site.

### Changed

- `X-Maintenance-Bypass` is in the default `CORS_ALLOWED_HEADERS` and redacted in telemetry.
- The dashboard's Inspector names the maintenance middleware `maintenance` in the global stack.
- 5xx problem details stay masked outside debug, except `MAINTENANCE_MODE`: its `detail` is the
  message written for clients with `ims down --message`.

## [0.1.6] - 2026-11-22

Sign in with Google or GitHub, and every login in the activity log.

**Upgrading from 0.1.5:** nothing to run: no migration, no new permission. Social login is off
until you set `SOCIAL_PROVIDERS`; see the [social login guide](docs/guides/social-login.md).

### Added

- **Social login with Google and GitHub.** `POST /api/v1/social/{provider}/login` takes a Google
  `id_token` or a GitHub authorization `code` (with the client's PKCE verifier and redirect URI),
  verifies it with the provider and signs in the existing user with that verified e-mail, with
  the same token and response as `POST /login` (no OTP step). `GET /api/v1/social/providers`
  lists what is enabled. Stateless: the front end or mobile SDK runs the provider's flow; the API
  stores nothing. New settings `SOCIAL_PROVIDERS`, `SOCIAL_GOOGLE_CLIENT_IDS`,
  `SOCIAL_GITHUB_CLIENT_ID` and `SOCIAL_GITHUB_CLIENT_SECRET`, checked at startup. New error code
  `SOCIAL_ACCOUNT_NOT_FOUND` (403).
- **Social login providers are drivers** in `internal/authn/social`: one file each, built on an
  OpenID Connect verifier (`go-oidc`) or an OAuth code exchange (`golang.org/x/oauth2`). The
  guide has the recipe for adding Microsoft, Apple, GitLab or others.
- **Logins in the activity log.** Every API login (password, OTP or social), successful or not, is
  an `auth.login` entry with its `method`, `device` and `status`. Successful ones are in the user's
  tenant; failed ones are system rows with the error code.
- **Docs:** a [social login guide](docs/guides/social-login.md) (how it works, GitHub and Google
  setup, front end and mobile, production, troubleshooting) and a 15-minute tutorial,
  [Sign in with GitHub locally](docs/tutorial/sign-in-with-github.mdx).

### Changed

- `code_verifier` is redacted in telemetry and the activity log, like `id_token` and
  `access_token`.

## [0.1.5] - 2026-11-21

An MCP server that AI apps add as a connector, with a built-in OAuth 2.1 authorization server.

**Upgrading from 0.1.2:** run `ims migrate` (migration `00027`), then `ims permission:sync` (new
system permission `oauth_client.manage`). MCP is off until you set `MCP_ENABLED=true`.

### Added

- **MCP server as a connector.** With `MCP_ENABLED=true`, AI agents (claude.ai, Claude Desktop,
  Claude Code, ChatGPT, Cursor, VS Code) add the API by its URL, `/mcp`. The user signs in on the
  API's own page, approves read or read-and-change access (platform accounts also pick a tenant),
  and the agent works as that user. Built in:
  - an OAuth 2.1 authorization server: authorization code with PKCE S256, rotating refresh
    tokens with reuse detection, RFC 8414 / RFC 9728 discovery, RFC 9207 `iss`, revocation;
  - clients connect **without registration** through Client ID Metadata Documents (SSRF-guarded
    fetch), or with a client an admin **registered by hand**: `/api/v1/oauth-client` (new system
    permission `oauth_client.manage`) and `ims oauth:client:create|list|rotate|delete`;
  - tools from the OpenAPI document: an operation marked `x-mcp` is a tool, served through the
    `/api/v1` router so permissions, tenancy, plan limits, validation and the activity log apply
    unchanged. Five to start: `whoami`, `list_branches`, `create_branch`, `list_users`,
    `create_user`;
  - `GET/DELETE /api/v1/user/connections` for a user's connections; activity entries written by a
    tool carry `_via` (tool and client); `oauth.authorized` / `oauth.revoked` entries; an MCP chip
    in the dashboard's request list; `ims_mcp_tool_calls_total`;
  - `testkit.WithMCP()` and `app.Agent(t, user, testkit.AllowChanges())` to test a tool through
    the real OAuth flow, next to its REST test (`agent.Call(...).OK(t)` / `.Refused(t, code)`).

  MCP access tokens work only on `/mcp`, and session tokens never do. See `docs/guides/mcp.md`, and the
  step-by-step tutorial `docs/tutorial/connect-an-ai-agent.mdx` (Claude Desktop, Claude Code and
  OpenCode against a local API).

## [0.1.2] - 2026-11-21

Act as a user, a system audit log, public tenant routes, request telemetry that knows who made each
request, and `make rename` to turn the template into your own app.

**Upgrading from 0.1.0:** run `ims migrate` (migrations `00025` and `00026`), then
`ims permission:sync` and `ims db:seed` (idempotent; it gives the Developer role
`user.impersonate`). Internal accounts that read `GET /api/v1/activity` now need
`activity.view.all`. Code calling `tenancy.Exempt()` passes the responder:
`tenancy.Exempt(responder)`.

### Added

<!-- rename:keep -->
- **`make rename`.** Turns a project made from the template into your own app in one step:
  `make rename MODULE=github.com/acme/shop-api [NAME=] [BINARY=] [TITLE=] [DRY=1]` rewrites the
  module path, binary and CLI name, deploy names and paths, display title and `IMS_` deploy
  variables, renames the systemd units, and regenerates code and docs. Seeded e-mails and credit
  links are kept; it refuses uncommitted changes and a project that was already renamed.
<!-- rename:end -->
- **Act as a user.** `X-Acting-User: <user id>` runs one tenant request as that user (their
  permissions, tenant and branch), for holders of the new `user.impersonate` permission
  (Super-Admin, Developer, or any internal role it is assigned to). Admins, platform accounts and
  inactive users cannot be targets; profile, password, avatar and branch-switch changes are refused
  while acting (`IMPERSONATION_FORBIDDEN`).
- **System audit log.** Activity entries without a tenant are the system log. Internal users with
  the new `activity.view.all` read every tenant and the system log at `GET /api/v1/activity`,
  filtered with `tenant_id=<id>` or `tenant_id=system`, plus `impersonator_id` and `impersonated`.
- **Act-as audit trail.** `impersonation.started` (fail-closed), `impersonation.request` for every
  request and `impersonation.denied` are system entries. Entries written while acting carry the
  staff member (`impersonator_id`, `impersonator_name`); tenants see only `by_staff: true`.
- **Public tenant routes.** `tenancy.Public` resolves the tenant from `X-Tenant-Code` (and
  optionally `X-Branch-Id`) before sign-in; first endpoint `GET /api/v1/public/tenant`. Modules add
  their own with `RegisterPublicTenantRoutes`.
- **Documentation site at [ims-go-api-starter.innovixmatrixsystem.com](https://ims-go-api-starter.innovixmatrixsystem.com).**
  - Innovix Matrix System credited in the hero, a footer on every page, the home page and the
    README.
  - The search dialog suggests pages and example searches before you type.

### Fixed

- **Request telemetry records who made the request.** Captured requests always had an empty
  `user_id` and `tenant_id`: telemetry read them from its own outer request context, which the
  authentication and tenancy layers never write to. They now fill an `httpx.RequestInfo` holder
  that telemetry and the access log read after the handler. Requests made while acting as a user
  also record `impersonator_id` (migration `00026`), shown as an "acting" chip in the dashboard;
  the access log gains `user_id`, `tenant_id` and `impersonator_id`.

- **Dashboard date filters use your calendar day.** The audit log and files pages sent the picked
  date as a bare `YYYY-MM-DD`, which the API reads as a UTC day, so near midnight (00:00-06:00 in
  UTC+6) "today" missed that night's entries. They now send the local day as RFC 3339 bounds with
  the browser's offset. The audit-log test built "today" from local time and failed in the same
  window; it now checks both forms.

### Changed

- Internal accounts without `activity.view.all` now get 403 on `GET /api/v1/activity` instead of
  every tenant's entries.
- `tenancy.Exempt` takes the responder and refuses `X-Acting-User` with 422.
- `CORS_ALLOWED_HEADERS` default includes `X-Acting-User`.

## [0.1.0] - 2026-11-20

The first versioned release: the full starter, its developer dashboard and its documentation site.

### Added

- **Versioning.** `VERSION` holds the release (`0.1.0`); release builds stamp it, local builds report
  `0.1.0-dev`, and a test keeps it valid semver.
- **Documentation site.** An Astro Starlight site in `website/` renders `docs/`: getting started, a
  step-by-step tutorial, guides for every feature, the dashboard tour, operations runbooks,
  architecture and design decisions. Published to Cloudflare Workers when a GitHub Release is
  published.
- **Docker support.**
  - What ships: a multi-stage `Dockerfile`, one image that runs every role, and a `compose.yaml`
    that runs PostgreSQL, the migration step, the API, the worker and the scheduler.
  - Optional: systemd stays the reference deploy.
  - CI builds the image and boots the stack on every push.
- **Generated reference pages.** `ims gen docs` renders the CLI, configuration, permissions and Make
  references from the code; `make gen-check` fails when they are stale.
- **Core API.** Multi-tenant tenants and branches with explicit SQL scoping that fails closed;
  opaque per-device tokens with OTP, throttling, password reset and e-mail verification; a
  permission catalog with roles per tenant and route-level checks; RFC 9457 errors; OpenAPI as the
  enforced contract.
- **Billing.** Packages with features and limits, subscriptions, bKash/cash/bank payments with staff
  approvals, staff plan assignment and per-tenant overrides (a `null` override resets to the plan).
- **Platform.** Settings and feature flags (with stable percentage rollout), activity log,
  notifications, files on local/S3/R2/MinIO disks, import/export jobs, backups to S3-compatible
  storage, River queue, a cron scheduler, mail, English and Bangla catalogs, health checks and
  Prometheus metrics.
- **Developer dashboard** at `/dev`: Telescope-style requests, logs and exceptions, queue, backups,
  commands, an inspector (system health, routes, roles, config, packages), audit log, tenants,
  sessions, settings, flags, files and developers, with a Ctrl+K palette.
- **Developer experience.** `ims gen module`, `testkit` integration tests, hot reload, and AI-agent
  skills in `.agents/skills`.

### Fixed

- Subscribe and the user/branch quota checks no longer deadlock under concurrent requests: inside the
  per-tenant lock every query runs on the locked transaction.
- A failed backup run reports `destinations: []` instead of `null`.

[Unreleased]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.9...v0.2.0
[0.1.9]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.7...v0.1.9
[0.1.7]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.2...v0.1.5
[0.1.2]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.0...v0.1.2
[0.1.0]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/releases/tag/v0.1.0
