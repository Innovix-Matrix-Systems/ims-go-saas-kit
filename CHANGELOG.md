# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org) (see `VERSION`).

## [Unreleased]

## [0.2.5] - 2026-11-25

**Currencies as data, Akaunting style.** A currency is how amounts are written (symbol, where it
goes, decimals, separators), one row each, and one of them is the default. Amounts stay integers in
minor units and are never converted: changing the currency changes the label. USD, BDT and EUR are
built in; staff add and edit currencies under Console → Currencies. See
[Money and currency](docs/guides/currencies.md).

**Upgrading from 0.2.3:** run `ims migrate` (migrations 00043 and 00044). Things to check:

- **`BILLING_CURRENCY` is gone.** The currency is data now. The migration makes the currency your
  payments already use the default (BDT for existing installs), so prices keep reading the same; a
  new install starts on USD. Remove the variable from your `.env`.
- **API:** new `GET /api/v1/public/currencies` (no sign-in) and staff `/api/v1/currencies`
  (`currency.*` system permissions; run `ims permission:sync`). Plan prices are in the default
  currency; each payment keeps its own `currency`.
- **Go:** `billing.NewOrchestrator`, `NewPaymentService` and `SubscriptionService.WithCurrencies`
  take a `domain.CurrencySource` instead of a code (`domain.FixedCurrency("BDT")` in tests);
  `service.FormatMoney` is gone, use `internal/platform/money`.
- **App:** `useMoney()(minor, code?)` defaults to the default currency instead of `BDT`; type
  amounts with `MoneyInput` and `toMinor` (`src/money.ts`).

### Added

- **Console → Currencies:** every currency with a live example; add one, change its symbol,
  position, decimals and separators with a preview, enable or disable it, make it the default
  (from its row or its drawer). Every change is recorded in the system audit log.
- `internal/platform/money` and `web/app/src/money.ts`: one formatter per side, the same output
  for the same row, digits in the reader's language (`৳১,২৩৪.৫০`), compact form for charts.
- `MoneyInput`: the currency's symbol on its side and its decimals; a typo is refused, never
  rounded.
- `notifications.Money`: a notice's amount is written in each reader's digits.
- A "Money and currency" guide, including how to store and show money in your own module.

### Changed

- Payments are recorded in the default currency; invoices (HTML and PDF), payment notices and the
  receipt e-mail write amounts from the currency's row (the PDF keeps the code: its fonts can't
  draw every symbol).
- bKash and Nagad refuse a payment unless the default currency is BDT; bKash amounts are sent
  without floating-point maths.
- "Amount (৳)" and "Price (৳)" lost the symbol in every language: the input shows the default
  currency's symbol.
- `payments.currency` no longer defaults to `BDT` in the database; the application always sets it.

### Fixed

- A one-day renewal reminder's body was empty in the feed and e-mail (a plain message sharing its
  params with a plural one).

## [0.2.3] - 2026-11-25

**Six languages, right to left, and a language system that isn't tied to any language.** The kit
speaks English, Bangla, German, Arabic, Spanish and French everywhere a person reads: the app, API
messages, notifications, e-mails and SMS, and the OAuth sign-in pages. Arabic reads right to left.
Adding another language is a descriptor and catalog files, with no code changes, and checks fail
until it is complete. See [Languages](docs/guides/languages.md).

**Upgrading from 0.2.0:** no migrations. Things to check:

- **`APP_SUPPORTED_LOCALES`** empty now offers every registered language, and an unknown code stops
  the start. A `.env` that still says `en,bn` keeps offering only those two: remove the line (or
  list the codes you want) to offer German, Arabic, Spanish and French.
- **`APP_LOCALE`** is now the default language for requests that match nothing (it was unused);
  `APP_FALLBACK_LOCALE` stays the e-mail fallback.
- **API:** the sign-up `locale` is a supported code instead of `en`/`bn` (and becomes the new
  organization's language); `users.locale` is validated against the offered languages. New: public
  `GET /api/v1/languages`; `GET /organization` returns `language`; settings items may carry
  `choices`. Feed items of system notifications are in the reader's language.
- **Delivery templates moved** out of `internal/platform/delivery/templates/` into the packages that
  send them (`internal/authn/templates/`, `internal/modules/*/templates/`,
  `internal/app/templates/`). A project that registered its own folder under a kit template's name
  still overrides it. `Registry.Layout` takes the language.
- **The app's catalogs** moved from `src/i18n/en.ts`/`bn.ts` to
  `src/i18n/catalogs/<namespace>/<code>.json`; `Locale` is a plain string and `locales` is
  `getLanguages()`. A screen you added needs its keys moved and translated into every language
  (`npm run i18n:check` lists them). The `right` and `align-right` CSS classes are `end` and
  `align-end`.
- The 46 `messages.*` API keys from the Laravel port were never used and are gone.

### Added

- **German, Arabic (right to left, Western digits), Spanish and French** for the app, the API and
  every e-mail and SMS template, each translated and reviewed with a glossary.
- **A language menu** in the top bar and on the signed-out screens; a pick is saved to the account.
  Ctrl+K lists "Change language…".
- **The organization's language** (`general.default_locale`, organization settings → General). One
  lookup order: the account's language → the organization's → the default, for the screen, API
  answers and e-mails. Sign-up sets it.
- **Stackable, module-owned translations:** each module registers its own catalog bundle and
  templates; a project changes wording in an overrides folder without editing kit files.
- **Plurals** (CLDR forms) on both sides: no more "1 permissions"; durations in e-mails read
  "1 Stunde 30 Minuten", "ساعة واحدة و30 دقيقة".
- **Checks:** `ims i18n:coverage`, `ims i18n:missing --locale <code> [--json]`, `npm run i18n:check`
  and `npm run i18n:types` (typed keys and placeholders), `npm run check:logical`, and Go tests for
  coverage, template variants, hard-coded language codes and the app/API language lists. All run in
  CI.
- **The `translate` skill** and glossaries for every language; **Inspector → Translations** in the
  developer dashboard; `ims gen module` writes the module's catalog.
- **Activity log** actions and subjects have labels in every language.
- **Settings `Choices`:** a setting can list its accepted values (checked on write, shown in the API).

### Changed

- The app loads only English up front and fetches another language when it is chosen (one chunk
  per language).
- System notifications (payment notices, renewal reminders, disabled webhooks) carry a message key
  and are rendered in each reader's language, in the feed and in e-mail.
- The app's stylesheets use logical properties only, so every screen mirrors right to left.
- "Mail and translations" became the **Languages** guide; the mail transport moved to the Delivery
  guide.

### Fixed

- Platform staff could not save their own profile, photo or language from the app (the
  organization route answered 403).
- Three Bangla template variants that never existed (invitation SMS; notification subject, SMS and
  push now read the same in every language).

## [0.2.0] - 2026-11-25

The project is now **IMS Go SaaS Kit**: the multi-tenant API, the app for organizations and staff,
and the developer dashboard in one binary. The binary and CLI keep their name, `ims`. The
documentation is published as a site at
[ims-go-saas-kit.innovixmatrixsystem.com](https://ims-go-saas-kit.innovixmatrixsystem.com) and
now covers the app as fully as the API.

**Upgrading from 0.1.8:** no migrations, and no API changes. The rename touches deploy names:

- **systemd:** the units are now `ims-go-saas-kit.service`, `ims-go-saas-kit-worker.service` and
  `ims-go-saas-kit-scheduler.service`, and the default install path is `/opt/ims/ims-go-saas-kit`
  (environment file `/etc/ims/ims-go-saas-kit/.env`). To keep a server on its current names, run
  `release.sh` with `IMS_SERVICE_PREFIX=ims-go-admin-starter`, `IMS_BASE_DIR`, `IMS_ENV_FILE` and
  `IMS_STATE_DIR` set to the old paths. To move to the new names, stop the old units, install the
  new ones from `deploy/systemd/`, then disable the old.
- **Docker:** the image is `ims-go-saas-kit`; update any `docker run` or registry tags.
- **Defaults:** `APP_NAME` is `ims-go-saas-kit` and `MAIL_FROM_NAME` is `IMS Go SaaS Kit` in the
  example environments. A deployed `.env` keeps its own values. `APP_NAME` is also the default
  OpenTelemetry service name, so traces appear under the new name only if you change it.
- **Code:** the Go module is `github.com/Innovix-Matrix-Systems/ims-go-saas-kit`. A project made
  from this template has its own module and is not affected.

### Added

- **The documentation site** (`docsite/`, Astro Starlight over `docs/`): a landing page with the
  one-binary route map, OpenTelemetry tracing, the four screens and Bangla; a layer strip on each
  page (database, API, app, dev dashboard); `make docs` and `make docs-build`. The build fails on a
  page missing from the sidebar or a broken link or anchor. CI builds it on every push, and
  `.github/workflows/docs-deploy.yml` deploys it to Cloudflare Workers when a release is published.
- **App documentation:** an overview, the organization workspace, the staff console, adding a
  screen, and translations and theming. The first-module tutorial now gives the module a screen,
  with a sidebar entry, permissions and both languages.
- **Make it yours:** `make rename`, rebranding the app and publishing your own docs, on one page.
- **Live updates guide:** how polling and the SSE stream work together, subscribing from any
  client, and adding live updates to your own feature.

### Changed

- Renamed to **ims-go-saas-kit** (module, deploy names, title); the `ims` binary and the `IMS_`
  deploy variables are unchanged.
- `make rename` starts from the kit's own names, so a project made from this template can rename
  itself again.
- The introduction, quick start, project tour, how it works, architecture overview, local
  development and releases pages, and both READMEs, describe the whole kit; every feature guide
  with a screen says where it is in the app.

### Fixed

- `deploy/scripts/release.sh` builds the app (`web/app`) as well as the developer dashboard before
  compiling, so a server deploy serves the app at `/` instead of an empty page. Docker builds were
  not affected.

## [0.1.8] - 2026-11-25

The developer dashboard gets charts on its panels (Recharts) and shows database connection
pools, and the health check warns before a pool runs out. Fixes: logging out no longer signs out
your other browsers, sign-up refuses a plan it cannot finish on before creating anything, error
messages say when a record is still in use, and the developer tooling works the same in
PowerShell as in a Unix shell.

**Upgrading from 0.1.6:** no migrations. `POST /api/v1/logout` now revokes only the token that
made the request; `device` is optional. If you implement `authn.Authenticator` yourself, `Revoke`
takes the token id instead of the device name. A `.env` copied from an older `.env.example` may
still carry `KEY=   # comment` lines; move those comments to their own line. A custom
`delivery.Repository` implementation needs a `CountByDay` method (the Deliveries charts).

### Added

- Charts on the dashboard's panels, drawn with Recharts: **Requests** (status classes over 1 hour,
  24 hours or 7 days; the busiest or slowest routes), **Exceptions** (per hour over the last day,
  by class), **Queue** (each queue's jobs), **Deliveries** (per day by outcome, by channel),
  **Files** (by type and disk, size or count) and **Sessions** (active tokens by last use). New
  endpoints `GET /dev/api/requests/stats`, `/dev/api/exceptions/stats` and
  `/dev/api/deliveries/stats`; the chart kit (`DonutChart`, `HBarChart`, `TimeBars`) and six
  series colour tokens are on `/dev/kit`.
- The dashboard shows database connection pools: **Inspector → System → Database pools** (in
  use, idle, opening, maximum, requests that waited, average time to get a connection, read live
  from `GET /dev/api/inspector/database`) and `3/20 active` next to the Database check on the
  Overview. The public `/api/health` report never shows the counts.

### Changed

- The Database health check warns when a pool has 80% or more of its connections in use
  (`primary: busy`); the API stays ready.
- The dashboard's charts (the Overview traffic chart and its sparklines) are drawn with Recharts,
  as in the client app, instead of hand-drawn SVG. They look and behave as before.
- `POST /api/v1/logout` revokes the calling token; the body is optional.
- `make skills-sync` and `make skills-check` run `go run ./cmd/skills`, and `make gen-check`
  calls `git --no-pager diff`, so they work in PowerShell and in non-interactive shells.
- The local-tracing docs explain why the endpoint is `http://127.0.0.1:4318`, name every port the
  Jaeger binary opens on Windows, and explain the failed `hello` span on Redis older than 6. The
  realtime guide lists the `billing` event.

### Fixed

- Logout deleted every token of the user with the same device name, so two browsers called
  "Web · Chrome on Windows" signed each other out.
- Sign-up with a plan whose `users.max` is 0 created the organization and its subscription
  before refusing the owner, and the clean-up left the subscription active, which kept the plan
  from being deleted. Such a plan is refused up front (503), and the clean-up cancels the
  subscription.
- Deleting a record that others still point at (a package with subscriptions) answered "The
  operation references a record that does not exist"; it now says the record is still in use.
- The last Admin of a deleted organization could not be erased.
- `.env.example` copied as-is did not load: six empty keys read their comment as the value
  (`APP_KEY` failed to decode); `TestEnvExampleLoads` keeps it loadable.
- `make rename` turned a URL-encoded title in the OpenAPI `otpauth://` example into
  "Your Title%20Go%20API"; encoded titles are renamed whole.
- `make dev` on Windows kept answering with an old API build left running by an air that was
  stopped without its process tree; it now stops that build first.

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

[Unreleased]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/compare/v0.2.5...HEAD
[0.2.5]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/compare/v0.2.3...v0.2.5
[0.2.3]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/compare/v0.2.0...v0.2.3
[0.2.0]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/compare/v0.1.8...v0.2.0
[0.1.8]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/compare/v0.1.6...v0.1.8
[0.1.6]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/compare/v0.1.0...v0.1.5
[0.1.0]: https://github.com/Innovix-Matrix-Systems/ims-go-saas-kit/releases/tag/v0.1.0
