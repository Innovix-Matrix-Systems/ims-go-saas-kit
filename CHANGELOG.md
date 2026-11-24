# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org) (see `VERSION`).

## [Unreleased]

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

[Unreleased]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/releases/tag/v0.1.0
