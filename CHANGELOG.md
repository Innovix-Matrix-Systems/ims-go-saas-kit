# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org) (see `VERSION`).

## [Unreleased]

## [0.1.0] - 2026-11-24

The first release of IMS Go Admin Starter: the
[IMS Go API Starter](https://github.com/Innovix-Matrix-Systems/ims-go-api-starter) 0.2.1 with the
client app its customers and staff use (an organization workspace and a staff console, embedded in
the binary and served at `/`), and the backend features that app needs. The entries below are the
changes on top of the API starter 0.2.1; everything before them is in the
[API starter's changelog](https://github.com/Innovix-Matrix-Systems/ims-go-api-starter/blob/main/CHANGELOG.md).

**Coming from the API starter:** an existing database runs `ims migrate` (migrations
`00037_create_onboarding`: `tenant_onboarding` and `user_agreements`; `00038_user_locale`:
`users.locale`; `00039_tenant_owner`: `tenants.owner_user_id`; `00040_payment_cancelled`: payments
may be `cancelled`; `00041_create_announcements`: announcements, their audience and their events),
`ims permission:sync` (adds `billing.view`, `billing.manage`, `organization.update`,
`announcement.view` and `announcement.manage`, and now also grants every tenant Admin role the
permissions it is missing) and `ims settings:sync` (the new **Sign-up** and **Billing** groups).
Restart `ims queue` and `ims schedule`: announcements are delivered by a new scheduled task.

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
- `DELETE /api/v1/files/{id}` (and the internal and dashboard deletes) answer 409 for an export's
  file; removing its job removes it. Export files do not count against the storage limit.
- An organization's own `data_processing_job.cleanup_days` now decides when its jobs are deleted
  (the cleanup used the platform's value only).
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
- Data-processing jobs report `expires_at`, failed ones can be retried
  (`POST /api/v1/data-processing/job/{jobId}/retry`) and finished ones removed
  (`DELETE /api/v1/data-processing/job/{jobId}`; staff not acting in an organization use
  `/api/v1/internal/data-processing/job/{jobId}` and `.../retry`); the activity log is exported as
  a background job (`POST /api/v1/activity/export`, staff: `POST /api/v1/internal/activity/export`).
- Dashboards: `GET /api/v1/dashboard` (an organization's members, activity, files, branches and
  roles, each section behind its permission) and `GET /api/v1/internal/dashboard` (staff:
  organizations, users, activity, revenue per currency, plans), with `?days=7|30|90` and
  `?tz=` (IANA); daily series come with the previous period's total.
- Client app home dashboards: staff see organizations, members, revenue, payments to review,
  plans and the newest organizations; an organization sees members, invitations, activity
  (trend, most frequent changes, most active members, recent entries), storage and its plan's
  usage; everyone gets quick actions.
- Client app: the sidebar header names the organization (its mark, name and plan) and opens to
  its profile, members, billing and settings; the top bar shows "Organization / Page"; the user
  card opens an account menu. New **Your profile** page (photo upload, details, organization,
  security summary) and **Organization** profile page (details with an edit dialog, owner, people,
  plan usage, counts). IDs and codes are shown as text with a copy button instead of read-only
  inputs.
- Client app **Connected apps** (Account settings): the AI assistants and other MCP clients
  connected to the account, with what they may do, where they act, when they were connected and
  last used, and a Disconnect button; it shows the connector address to paste when MCP is on, and
  says it is off otherwise.
- **Announcements** (`internal/modules/announcements`): one record per announcement, whatever it
  reaches. Staff manage them at `/api/v1/internal/announcements` (`announcement.view`,
  `announcement.manage`): a kind (`info`, `promotion`, `warning`, `critical`), an optional
  button, a window, a status, whether members may dismiss it, an audience of every organization
  or listed ones, and optional channels that deliver it once through the notification system
  when it starts (task `announcements.deliver`). Members get `GET /api/v1/announcements/live` and
  record views, clicks and dismissals (`POST /api/v1/announcements/{id}/events`, once per person);
  staff see the counts in all and per organization.
- Client app: the console's Announcements page lists one row per announcement with its state and
  reach, edits with a live preview (kind, button, audience, window, channels, draft or publish),
  and shows how each did per organization; members see the most urgent live one as a banner,
  closing it is remembered on the device and the server.
- Client app banners: the organization's plan when it needs attention (ending within a week,
  cancelled, overdue, ended, a payment awaiting approval, or no plan) for those who see billing,
  hidden for the day on request.
- `POST /api/v1/organization/logo` uploads the organization's logo (an image on the public disk,
  attached to the organization and listed with its files, replaced by the next upload) and sets
  its public `url` as the `branding.logo_url` setting. Tenant responses (staff) carry `branding`
  too. File responses carry `collection` (`logo`, `profile`, `output`).
- Client app: the logo is uploaded from the Logo setting or the organization profile (an edit
  button on its mark), shows its link to copy, and appears in the sidebar, the profile and the
  staff organization list and drawer; Files marks the organization logo and profile photos and
  shows a public file's link to copy.
- The organization dashboard's files section has `by_type` (count and bytes per file type).
- Client app: dashboard charts use Recharts (trend areas, donuts for plans, organization status,
  members by status and storage by type, ranked bars), loaded only with the home page; pricing
  cards line up (price and cycle, features with "Show more", the action at the bottom) and name
  unknown features readably; a plan-and-usage peek in the top bar; one "Account" entry in the
  sidebar; organization settings with profile and owner side by side and a card per settings
  group.
- `GET`/`PATCH /api/v1/organization` return `branding` (`logo_url`, `color` from the
  organization's e-mail branding settings) so clients can show its logo.
- Client app: a getting-started checklist for new organizations' admins; organization and
  platform settings in plain language with fitting controls (units, colour picker, plan picker);
  a member's panel that reads like a profile; photos wherever people appear; branches shown as
  text to those who may not edit them; dashboards that fit a phone.
- Finished exports are listed among the organization's files (`GET /api/v1/files`, with
  `generated: true`) until their job is removed or expires.
- Client app **Background jobs**: a top-bar tray and a page listing the signed-in person's
  exports and imports with progress, failures and per-row import errors, download until the file
  expires, retry and remove. Members (organization and staff console) and the activity log export
  as background jobs (the activity export covers every matching entry, not just the visible page),
  and starting one shows a dialog with its job ID that leads to Background jobs.
- Client app Files: grid and list views with image thumbnails, and a preview (images, video,
  audio, PDF) with the file's details; generated exports are marked and managed from Background
  jobs. Numbered lists page with first / previous / pages / next / last; cursor-paged feeds
  (activity, notifications) with previous and next only.
- Organizations withdraw a pending checkout (`POST /api/v1/billing/payments/{id}/cancel`) and
  download invoices (`GET /api/v1/billing/payments/{id}/invoice`, printable HTML);
  `GET /api/v1/packages/features` lists the feature catalog;
  `GET /api/v1/internal/notification` filters by `event_type`.
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
  the console adds feature flags (for everyone or one organization, with rollout and reset);
  staff get a platform home with counts; the
  plan editor reads the feature catalog from the API; the date picker and select follow the
  app's language. The activity log and notifications page with cursors (previous and next, rows
  per page); the console has an audit log of every organization and the system log; the per-page
  picker opens upwards when needed and is no longer cut off; settings pages show descriptions, not
  setting keys; a staff account reads its own notifications through the internal routes.
  The sidebar is resized by dragging its edge (remembered; double-click resets) and pinned open or
  folded to a rail with a pin button beside the logo.
  Every page shows a skeleton in its own shape (tables, lists, settings, forms, cards, tiles)
  while it loads; a page's outline shows while its code loads and the whole frame on first load.
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

### Removed

- The documentation website (`website/`, `make docs`, `make docs-build`, the docs-deploy
  workflow). `docs/` reads on GitHub, and the backend's guides are on the IMS Go API Starter's
  site; the README describes the app and links there.

[Unreleased]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/releases/tag/v0.1.0
