# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org) (see `VERSION`).

## [Unreleased]

## [0.1.6] - 2026-11-25

Live updates, invoice PDFs and payment notices, plus one frontend setup instead of two. The
notification bell, announcement banners and billing screens update the moment something changes,
over one Server-Sent Events stream per browser fed by Postgres `LISTEN/NOTIFY` (no Redis needed).
Invoices download as PDF, and a bank-transfer payment now tells the staff who approve it and the
organization's billing managers what is happening, ending with a receipt e-mail that carries the
invoice PDF. The developer dashboard builds the same way as the app, and the Developers page can
give the Developer role every permission or reset it.

### Added

- **Live updates instead of polling.** The notification bell and announcement banners now update
  as soon as something changes. One Server-Sent Events stream per browser
  (`GET /api/v1/realtime/stream`) says when the account's notifications or announcements changed,
  and the app refetches them. A new notification shows up when its fan-out finishes, reading one
  clears the badge in your other tabs, and an announcement appears the minute it starts. Polling
  stays as a slower fallback (bell every 5 minutes, banners every 15 while the stream is up).
- **A live-update bus for every module.** Any process (API, queue worker, scheduler) publishes a
  nudge with `pg_notify`; each server keeps one `LISTEN` connection and feeds its open streams.
  No Redis needed, works across several servers. Settings: `REALTIME_ENABLED`,
  `REALTIME_HEARTBEAT`, `REALTIME_MAX_STREAM`, `REALTIME_MAX_STREAMS_PER_USER` and
  `REALTIME_LISTEN_DSN` (for installs whose database URL goes through PgBouncer in transaction
  mode). See `docs/guides/realtime.md`, which also covers nginx.
- **Invoices as PDF.** `GET /api/v1/billing/payments/{id}/invoice.pdf` and an **Invoice** button
  on the billing page download a one-page PDF (the HTML invoice stays, as **HTML**).
- **Everyone hears about a payment.** When an organization submits a bank-transfer or cash
  payment, the staff who can approve it get a "Payment to review" notification and e-mail, and
  the organization's billing managers (and whoever paid) get "Payment submitted". Approving it
  sends them "Payment received" and a receipt e-mail with the invoice PDF attached; rejecting it
  sends the reviewer's note. Billing pages and the approval list refresh on their own.
- **Notifications can open a page.** A notification whose data has an in-app `url` (the payment
  ones do) opens it when clicked.
- **Give the Developer role every permission from the Developers page.** "Grant all permissions"
  (after a confirmation) lets developer accounts test any API feature; "Reset to baseline" takes the
  role back to developer-dashboard access only. New endpoint `POST /dev/api/developers/role/all`,
  and the role stats now report `all_permissions` next to the baseline `total_permissions`.

### Changed

- **The developer dashboard no longer uses TanStack Start.** It only ran Start in SPA mode, so it
  used none of Start's server features. It now has an `index.html` and `src/main.tsx` like the
  app, and the `@tanstack/react-start` package is gone. The pages, routes and look are unchanged.
- **The dashboard's theme script is a file.** The saved theme, accent and density are applied
  before first paint from `/dev/appearance.js` instead of an inline script, so a strict
  Content Security Policy can cover the dashboard too.
- **The `/dev` handler serves `index.html`** (it served Start's `_shell.html`).

### Fixed

- **The notification badge could show too few.** When the unread counter was not cached (never
  read yet, or expired after a week), a new notification set it to 1 instead of recounting, so
  someone with three unread saw "1". The counter is now recounted from the database instead.
- **A notification arriving while the app loaded could leave the badge stale.** The app now
  refetches notifications and announcements every time its live-update stream connects, not
  only after a reconnect.
- **The Developers page said the role had all permissions.** It holds the developer-dashboard
  baseline unless you grant all; the page and its tiles now say which.

### Upgrading from 0.1.5

- **No migrations and no required settings.** Live updates are on by default
  (`REALTIME_ENABLED=true`); the `REALTIME_*` settings are optional.
- **Behind nginx**, give `/api/v1/realtime/stream` `proxy_buffering off` and a long
  `proxy_read_timeout` (see `docs/guides/realtime.md`), or the stream is held back and the app
  falls back to polling.
- **Behind PgBouncer in transaction mode**, set `REALTIME_LISTEN_DSN` to a direct (or session-mode)
  connection: `LISTEN` does not work through a transaction pooler.
- **Restart the server, the worker and the scheduler together**: they share mail templates and
  notification events, and the receipt mail is sent by the worker.

## [0.1.5] - 2026-11-24

A tidy-up and visibility release. Every change to your data now shows up as a named step in a
trace, the personal-data export email works from any inbox, and the code reads the same way in
every module. Nothing in the API changes shape.

### Added

- **A span for every change.** With tracing on, each create, update, delete, import, export,
  erasure, sign-in, payment approval and delivery is its own step in the trace (`identity.create`,
  `billing.approve_payment`, `privacy.erase` …), with the queries it ran underneath. Spans carry ids
  and counts only, never names, e-mail addresses or tokens.
- **Failures and refusals look different.** A refused request (validation, not found, conflict)
  records its error code on the step without turning the trace red; only real failures do.
- **Traces now include file storage, Redis, mail and maintenance mode.** S3, R2 and MinIO calls,
  every Redis command (through the official `redisotel` package), SMTP sends, message deliveries,
  turning maintenance mode on and off, and retrying, cancelling, deleting, pausing or resuming queue
  jobs.
- **A test that keeps it that way.** A new service write without a span fails the test suite unless
  it is listed with a reason, and `ims gen module` writes services that already comply.
- **HTML versions of six account emails**, in English and Bangla: data export ready (with a
  download button), account deletion scheduled, account erased, recovery code used, and two-factor
  turned on or off.

### Fixed

- **The "your data export is ready" link answered 401.** It pointed straight at the API, which a
  browser opens without a sign-in token. It now opens the app's privacy page, which downloads the
  file for the signed-in owner. The new `AUTH_DATA_EXPORT_URL` setting chooses that page (default
  `{APP_URL}/settings/privacy?export={job}`); the link carries no secret.
- **The privacy page forgot your export after a reload.** It now finds the export the email links
  to, or your latest one, says until when it can be downloaded, and offers a new one when it has
  expired or failed.
- **Signing in from a link lost part of it.** After signing in you now land on the exact link you
  opened, query included.
- Members whose role holds no permissions can export and download their own data. This was always
  allowed; it is now tested.

### Changed

- **Every module reads the database through its repository.** Announcements, the dashboard, the
  activity feed and its export, data-processing retries and billing invoices and usage called the
  database from their services; they now follow the same handler → service → repository shape as
  the rest, with their own `routes.go` and request or model files.
- Internal roadmap references were removed from comments, tests and migration headers.
- The renewal-reminder task's description says what it does: it notifies organizations whose plan
  expires in 7, 3 or 1 days.
- The README links straight to the API starter's documentation site.

### Upgrading from 0.1.0

No migrations. If you build on the starter's Go packages, a few constructors changed:
`activity.NewService`, `announcements.NewService` and `dashboard.NewService` take a repository
(`NewRepository(queries)`), `billingservice.NewInvoiceService` and `NewUsageService` take
`domain.NewReportRepository(queries)`, the activity export's `New` takes a repository instead of
the queries and checker (its routes move to `activityexport.NewHandler`), `dataprocessing.Service`
lost `WithQueries`, `privacy.Service.WithExports` takes the export page URL instead of the API
URL, and the Redis cache's `New` now also returns an error.
Optionally set `AUTH_DATA_EXPORT_URL` if your client app does not live at `APP_URL`.

## [0.1.0] - 2026-11-24

The first release of **IMS Go Admin Starter**. It is the
[IMS Go API Starter](https://github.com/Innovix-Matrix-Systems/ims-go-api-starter) (0.2.1) with the
app your customers and your staff actually use, built into the same single binary and served at
`/`. Organizations get a workspace for their people, billing, files and settings; your team gets a
console to run the platform. Everything is in English and Bangla, in light and dark.

Changes to the backend are listed below too. For the backend's history before this release, see the
[API starter's changelog](https://github.com/Innovix-Matrix-Systems/ims-go-api-starter/blob/main/CHANGELOG.md).

### Added

**For an organization**

- **Sign-up and first steps.** Anyone can create an organization from the sign-in page: they
  accept the terms, confirm their e-mail and land in a setup wizard (pick a plan, invite the team,
  turn on two-factor). A getting-started checklist on the home page shows what is left.
- **Home dashboard.** Members, invitations, activity over time, storage by file type, plan usage,
  the most active members and quick actions, for the last 7, 30 or 90 days.
- **Members, roles and branches.** Invite people, edit, deactivate or remove them, reset their
  two-factor and export or import the list. Roles come with a permission editor that offers only
  what you may grant; the Admin role is managed and cannot be broken. Branches can be created,
  edited and deleted, and a branch switcher in the top bar narrows members and notifications to
  one branch or shows **All branches**.
- **Organization profile and settings.** Name, contact details, logo and owner (with ownership
  transfer); preferences grouped by area in plain language, with fitting controls such as a colour
  picker. The uploaded logo appears in the sidebar, the profile and the staff console.
- **Billing.** The current plan and how much of each limit is used, plans to choose from with a
  bank-transfer checkout, payment history, invoices to download, and a banner when the plan is
  about to end, has ended or waits for a payment.
- **Files and background jobs.** Files in a grid or a list with previews of images, video, audio
  and PDFs. Exports and imports run in the background, with progress, errors per row, retry, and
  downloads until the file expires. Admins see everyone's jobs and filter them by member, type and
  status; everyone else sees their own.
- **Activity log, webhooks and notifications.** The organization's audit log with filters, details
  and export; webhook endpoints with a delivery log; a notification bell and page; and staff
  announcements shown as banners.

**For your staff (the console)**

- **Overview** of organizations, members, revenue, payments to review and plans.
- **Organizations and users**: search, open, suspend or reactivate, change a plan, impersonate an
  organization or log in as a user. Both show a banner with the way back and are recorded in the
  audit log.
- **Billing**: approve or reject payments with a note, and edit plans and their features.
- **Announcements**: one announcement to every organization or chosen ones, as a notice, warning
  or promotion, with an optional button, a schedule, optional e-mail or push, and views, clicks and
  dismissals per organization.
- **Platform**: the system audit log, staff accounts, feature flags (for everyone or one
  organization, with a rollout) and platform settings, including the bank details shown at
  checkout.

**For everyone**

- Your profile and photo, e-mail and password changes, two-factor with an authenticator app or
  e-mail codes and recovery codes, signed-in devices, notification preferences, a download of your
  personal data and account deletion.
- **Connected apps**: see the AI assistants (MCP clients) connected to your account, what they may
  do and when they were last used, and disconnect them.
- Ctrl+K to jump anywhere, keyboard shortcuts, accent and density presets, a sidebar you can resize
  or fold, loading skeletons in the shape of each page, and layouts that work on a phone.

**In the backend**

- New endpoints for what the app needs: self-service sign-up, the organization and its owner,
  onboarding, dashboards, plan usage, invoices, withdrawing a checkout, invitation previews,
  changing your e-mail address, the organization logo, announcements, branch switching back to all
  branches, and the whole organization's background jobs. Every one is in the OpenAPI reference at
  `/docs`.
- Organizations manage their own billing with the new `billing.view` and `billing.manage`
  permissions, held by the Admin role.
- One password rule wherever a password is chosen: at least 8 characters and not one of the most
  common passwords.
- The binary serves the app at `/` with a strict Content-Security-Policy; `make dev` starts it on
  port 8082 next to the API and the developer dashboard.

### Changed

- A member's list of people and their notifications follow the branch they work in; those who may
  switch branches see every branch until they pick one.
- An organization's Admin role is managed: it cannot be renamed, deleted or given other permissions,
  and roles can only be given permissions their editor holds.
- Updating a user or a profile keeps the fields you leave out instead of clearing them.
- Subscribing to a free plan starts it at once, with no payment.
- An organization's own cleanup setting decides when its finished jobs are deleted, and exported
  files no longer count against its storage limit.
- A branch that still has members, or an organization's last branch, cannot be deleted.
- Staff no longer see every organization's organization-wide notifications in their own feed.

### Fixed

- Organizations could not see their payments or choose a plan themselves: the routes needed
  permissions no organization role could hold.
- A fresh install could not add users, because the default organization had no plan.
- Erasing a user left their sign-in requests in the developer dashboard's request log.

### Removed

- The documentation website. The docs in `docs/` read on GitHub, and the backend's guides are on
  the [API starter's site](https://ims-go-api-starter.innovixmatrixsystem.com).

### Upgrading from the API starter

If you run this on a database created by the API starter, run `ims migrate`,
`ims permission:sync` and `ims settings:sync`, then restart `ims queue` and `ims schedule`
(announcements are delivered by a new scheduled task).

[Unreleased]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.6...HEAD
[0.1.6]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.0...v0.1.5
[0.1.0]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/releases/tag/v0.1.0
