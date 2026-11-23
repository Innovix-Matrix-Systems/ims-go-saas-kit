---
title: Account deletion & data export
description: Erase a user's personal data in every module, or hand them a copy of it — the right to erasure and the right of access (GDPR and similar laws).
---

A user can ask for their account to be deleted, or for a copy of everything the application
holds about them. Admins and the CLI can do both for a user, so support can answer a
data-subject request. The design and its reasons are in
[`docs/design/account-deletion-and-export.md`](https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/blob/main/docs/design/account-deletion-and-export.md).

## What erasure does

An erased user **keeps their `users` row as a tombstone**: the id stays, so every
`created_by`, invoice and audit row still points at someone, but the row now says
"Deleted user", the e-mail becomes `erased+<id>@invalid`, the phone and names are gone, the
password can never match and `erased_at` is set. Sign-in refuses it, and the e-mail address is
free to sign up again.

Every module then removes its own data about the person, in one transaction:

| Data | What happens |
| --- | --- |
| Sessions, two-factor, MCP connections, roles | deleted |
| Profile picture and other files owned by the user | deleted (row and stored object) |
| Notifications received, read state, preferences | the user's links deleted; the notifications stay |
| User settings and feature-flag overrides | deleted |
| Personal-data exports of the user | deleted (job and archive) |
| Activity log | kept; rows the user made lose the IP, rows about the user (including their logins) lose the copied values, failed logins with their e-mail are scrubbed |
| Deliveries log, webhook payloads, dashboard audit | kept, without the address, the copied values, the IP and browser |
| Payments, invoices, records the user created for the tenant | kept; their author is the tombstone |
| Dashboard request captures | deleted |

The super admin cannot be erased, and neither can a tenant's **last active Admin**: assign the
Admin role to someone else first. Every erasure is a row in `privacy_requests` (ids and counts,
never values) and an id-only `user.erased` activity entry, deliverable as a
[webhook](webhooks.md) so connected systems can erase their copies too. The person gets a last
mail at the address the account had.

## For the user

```http
POST   /api/v1/user/privacy/deletion   {"password": "…"}   ask for deletion (202)
GET    /api/v1/user/privacy/deletion                       the scheduled deletion (404: none)
DELETE /api/v1/user/privacy/deletion                       cancel it
POST   /api/v1/user/privacy/export     {"password": "…"}   export my data (202)
```

These are session routes (like two-factor): tenantless accounts reach them and `X-Acting-User`
never applies, so the account is always the caller's own.

**Deletion** waits `privacy.deletion_grace_days` (a setting, 14 by default, per tenant). Asking
signs out every session and MCP connection and mails the date; the user can sign in again and
cancel until then. Signing in does not cancel by itself. The `privacy.erase-due` task (every
15 minutes) erases the due ones, and finishes an erasure left "processing" for over an hour by a
process that died (it rolled back, so running it again is safe). A cancel that lands while the
task runs still wins. A refused or failed run is marked `failed` and not retried — rerun it with
`ims privacy:erase`. Erasures of one tenant's users run one at a time, so two can never both pass
the last-Admin check.

**Export** builds a zip in the queue: one folder per module with JSON files, the user's files,
and `manifest.json`. It never contains a password hash, token, two-factor secret or recovery
code. The user gets a mail with the link, polls `GET /api/v1/data-processing/job/{jobId}` and
downloads `/api/v1/data-processing/job/{jobId}/download`. Only whoever asked can see or download
it, whatever their data-processing permissions. One self-service export a day; the archive is
deleted after `privacy.export_ttl_hours` (72 by default) by `privacy.expire-exports`.

## For admins

```http
POST   /api/v1/user/{id}/erase            {"immediate": false}   user.erase
DELETE /api/v1/user/{id}/erase                                   user.erase (cancel)
POST   /api/v1/user/{id}/privacy-export                          user.privacy_export
POST   /api/v1/internal/user/{id}/erase                          user.internal.erase
DELETE /api/v1/internal/user/{id}/erase                          user.internal.erase
POST   /api/v1/internal/user/{id}/privacy-export                 user.internal.privacy_export
```

Erasing schedules with the same grace period and mail (202); `immediate: true` erases now
(200), and records the admin as the requester even when the user had scheduled it. A user can
cancel only a deletion they asked for; one an admin scheduled is cancelled with `DELETE`. A user
outside the caller's tenant answers 404 whatever their state. An admin's export belongs to the admin: the link is mailed to them and only they can
download it. Tenant Admins hold `user.erase` and `user.privacy_export` (run
`ims permission:sync`).

Users deleted the ordinary way (`DELETE /api/v1/user/{id}`, which can be undone) keep their
data. Set `privacy.erase_soft_deleted_after_days` to erase them automatically after that many
days (`privacy.erase-soft-deleted`, daily; 0 turns it off).

## From the CLI

```bash
ims privacy:erase --user=42 --force         # now, no grace period
ims privacy:export --user=42 --out=42.zip   # to a local file
ims privacy:coverage                        # which module erases each user column
```

## Adding a module that stores user data

Every column that may hold a user id must be **claimed** by a contributor or **exempted** with a
reason, or `TestCoverage` (and `ims privacy:coverage`, also in the dashboard's Commands panel)
fails. `created_by` and `updated_by` are already covered: they point at the tombstone.

Give the module a `privacy.go` with a contributor:

```go
// PrivacyContributor covers the user's saved addresses.
type PrivacyContributor struct{}

func (PrivacyContributor) Name() string      { return "addresses" }
func (PrivacyContributor) Columns() []string { return []string{"addresses.user_id"} }

func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	rows, err := q.ListAddressesForPrivacy(ctx, s.UserID)
	if err != nil {
		return database.MapError("addresses.privacy_export", err)
	}
	return w.JSON("addresses", rows) // map to a struct; never export secrets
}

func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	deleted, err := q.DeleteAddressesForUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("addresses.privacy_erase", err)
	}
	return privacy.Report{Deleted: deleted}, nil
}
```

and register it in `internal/app/privacy.go`, before `identity` (which writes the tombstone and
must run last). Delete what is only the person's; keep and scrub what the tenant must keep. A
contributor that also implements `privacy.Guard` can refuse an erasure. Report stored files in
`Report.Objects`: they are deleted after the transaction commits. A column that does not hold
users (or is kept on purpose) goes in `privacyExemptions` with the reason.

## After a backup restore

A backup taken before an erasure brings the person back, and the restore brings back the old
ledger too. Save the list first, then replay it ([Backups](../operations/backups.md)):

```bash
ims privacy:ledger --since=2026-11-01 --out=erased.txt   # on the live database, before
ims privacy:replay --from=erased.txt                     # after the restore
```

Replay skips users that are still erased or gone, and does not run the guards: the erasure was
already decided.

## What is outside the database

Finished queue jobs (River) keep their payloads, a queued mail's address included, until the
queue's retention removes them. Logs and traces never carry e-mails or phone numbers by design;
their retention is an operations setting. Mail and SMS providers and other processors keep their
own copies: the `user.erased` webhook is how the application tells them.
