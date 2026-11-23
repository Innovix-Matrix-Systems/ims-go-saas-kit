---
title: Activity log
description: Record who changed what in a tenant, like spatie/laravel-activitylog, and read it back as a feed.
---

The activity log is the tenant-facing audit trail: "Rahim created branch Dhaka", "Admin assigned
role Cashier to Karim". Services record entries after a change succeeds; tenant admins read them
as a feed.

## Recording

Give the service an optional recorder and call it after the write:

```go
// WithActivity records branch changes in the tenant's activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

s.activity.Record(ctx, activity.Entry{
	TenantID:  tenantID,
	Action:    "branch.created",          // <subject>.<verb>
	Subject:   "branch",
	SubjectID: branch.ID,
	Changes:   map[string]any{"name": branch.Name},
})
```

Wire it in the composition root with `.WithActivity(platform.Activity)`.

- **Nil-safe**: a nil recorder records nothing, so unit tests need no setup.
- **Never fails the caller**: a logging failure is logged, not returned.
- **Context does the rest**: the actor, IP address and request id come from the request context.
- **Redacted**: keys that look secret (`password`, `token`, `secret`, …) are masked; add your own
  with `DEV_DASHBOARD_REDACT_KEYS`.

Record what a tenant admin would want to see — creations, deletions, role and permission
changes, plan changes (`subscription.assigned`, `subscription.cancelled`). Don't record reads.

## Reading

```http
GET /api/v1/activity?per_page=25&cursor=…        (activity.view or activity.view.all)
```

A newest-first **cursor** feed. Filter with `?subject_type=`, `?subject_id=` and `?actor_id=`.

Entries with no tenant (`tenant_id: null`) are the **system audit log**: internal users and
roles, system settings, act-as. Who reads what:

| Reader | Sees | Extra filters |
| --- | --- | --- |
| Tenant user with `activity.view` | their own tenant only, never system entries | none |
| Internal user with `activity.view.all` | every tenant and the system log | `?tenant_id=7`, `?tenant_id=system`, `?impersonator_id=`, `?impersonated=true` |
| Internal user without `activity.view.all` | nothing: 403 | |

## Logins

Every API login is an `auth.login` entry with `method` (`password`, or the
[social provider](social-login.md): `google`, `github`), `email`, `device` and `status`. A
successful login is a row in the user's tenant, so tenant admins see their users sign in. A failed
one is a system row with the `error` code, since the attempt may not belong to any account. An OTP
login is recorded once, when the code is verified. `authn.Service` reports the attempts through
the `authn.LoginAudit` interface, which `activity.Recorder` implements.

## Maintenance mode

Every [`ims down` / `ims up`](maintenance-mode.md), from the CLI or the dashboard, is a system
entry: `maintenance.down` (with `message`, `retry_after` and `with_bypass`, never the secret) or
`maintenance.up` (with `down_since` and `down_seconds`). The actor is the dashboard user who
toggled, or none from the CLI. `maintenance.Service` reports the toggles through the
`maintenance.Audit` interface, which `activity.Recorder` implements.

## Suspended tenants

[Suspending a tenant](tenancy.md#suspending-a-tenant) or bringing it back writes a system entry,
`tenant.suspended` or `tenant.reactivated`, with the tenant's `name`, `code` and `is_active`. The
actor is the user who changed it. A suspension made by billing (`BILLING_SUSPEND_ON_EXPIRY`) has no
actor and carries `"reason": "subscription_expired"`. Saving a tenant without changing `is_active`
writes nothing.

## Acting as a user

Everything done with `X-Acting-User` is recorded three ways:

1. `impersonation.started`: a system entry the first time a caller acts as a given user within 30
   minutes. It is **fail-closed**: if it cannot be written, the request is refused with 503 and
   nothing runs as the user.
2. `impersonation.request`: a system entry for **every** request made while acting, reads
   included (method, route, status, the user's tenant and branch). This covers modules that never
   call `Record`.
3. Every ordinary entry written while acting carries `impersonator_id` and `impersonator_name` (a
   snapshot, kept if the staff account is deleted). The actor stays the user; the shared
   `request_id` links it to its `impersonation.request` entry.

Refused attempts write `impersonation.denied`. In the `impersonation.*` entries the actor is the
staff member and the subject is the user.

Tenant users see `by_staff: true` on entries staff made while acting, but not who
(`impersonator: null`), and never the `impersonation.*` entries. `activity.view.all` readers see
`impersonator: {id, name}`. The system entries have no tenant, so they survive the tenant being
deleted.

## Retention

The `activity.prune` task deletes entries older than `ACTIVITY_RETENTION_DAYS` (default 365;
`0` keeps everything).

## Not the same as the dashboard audit log

| Activity log | Dashboard audit log |
| --- | --- |
| changes made through the API, API logins, and act-as (system log) | developer-dashboard actions and sign-ins |
| read at `/api/v1/activity`: tenant admins their tenant, `activity.view.all` everything | read in the dashboard's **Audit log** |
| `activity_log` table | `dev_actions` table |
| `ACTIVITY_RETENTION_DAYS` | `DEV_DASHBOARD_AUDIT_RETENTION_DAYS` |
