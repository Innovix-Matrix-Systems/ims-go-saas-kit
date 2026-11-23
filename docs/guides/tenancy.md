---
title: Tenancy
description: How tenants and branches are resolved, scoped in SQL, impersonated — and why a forgotten filter fails closed.
---

Every organisation using the API is a **tenant**; each tenant has one or more **branches** (shops,
clinics, offices). Users belong to a tenant and work in one branch at a time. Platform accounts —
the super admin, developers — belong to **no** tenant.

## Isolation is explicit, in SQL

There is no row-level security and no ORM global scope. **Every tenant-scoped query receives and
filters on an explicit `tenant_id`**, and a missing filter is a security bug. That is decision 18:
explicit scoping is the only isolation boundary, and per-module isolation tests enforce it.

```sql
-- single row
WHERE id = @id AND (@all_tenants::boolean OR tenant_id = @tenant_id)
-- lists
WHERE tenant_id = @tenant_id
```

## The scope

Middleware resolves a `tenancy.Scope` for every request in the tenant-resolved route group, and
handlers pass it down explicitly:

```go
scope := tenancy.From(r)            // in a handler
scope := tenancy.FromContext(ctx)   // below the handler, if you must
```

| Use | For |
| --- | --- |
| `scope.Filter()` | single-row reads/writes — rendered as `(@all_tenants OR tenant_id = @tenant_id)` |
| `scope.EffectiveTenantID()` | lists and inserts |
| `scope.EffectiveBranchID()` | branch-scoped lists |

:::tip[Fails closed]
A zero `tenancy.Filter{}` matches **no** tenant rows. Forget to pass the scope and you get empty
results or a 404 — never another tenant's data. Reading every tenant is explicit:
`scope.AllTenants()` / `tenancy.Unscoped()`, allowed only for internal routes or global
impersonation.
:::

## How a request is resolved

**Tenant users** must have a `tenant_id` and an active branch. The branch-belongs-to-tenant check
is cached for five minutes (`branch_check:*`) and invalidated when branches change.

**Impersonators** come in two kinds, both per request (no session, nothing stored):

|  | View as a tenant | Act as a user |
| --- | --- | --- |
| Header | `X-Acting-Tenant` (+ `X-Acting-Branch`) | `X-Acting-User: <user id>` |
| Who may send it | `user.tenant.switch` | `user.impersonate` |
| Identity in the context | still the caller | the target (`Identity.ImpersonatorID` = the caller) |
| Permissions checked | the caller's | the target's |
| Tenant and branch | the acting headers | the target's own tenant and active branch |
| Use it for | cross-tenant administration, support reads | "do what Jane would do", reproducing a user's bug |

View as a tenant:

| Headers | Result |
| --- | --- |
| none | a **global, read-only** view of all tenants (tenant 0); writes are refused with 403 |
| `X-Acting-Tenant: 7` | act as tenant 7, across all of its branches |
| `X-Acting-Tenant: 7` + `X-Acting-Branch: 12` | act as branch 12 of tenant 7 |

Act as a user (`tenancy.Resolve` swaps the identity, so permission gates, scoping, handlers and
the activity recorder all see the target):

- `user.impersonate` is a system permission: Super-Admin (bypass) and Developer (seeded) have it,
  other internal roles get it by assignment, tenant roles never can.
- Refused with 403 `IMPERSONATION_FORBIDDEN`: yourself, a missing or inactive user, a tenantless
  platform account, a user who holds `user.impersonate` (no sideways escalation, no chains), and a
  caller without the permission.
- Combined with `X-Acting-Tenant` or `X-Acting-Branch`: 422. On tenant-exempt routes: 422. The
  session routes (`/logout`) ignore it, so you never sign the target out.
- Account changes are refused while acting (`authn.DenyImpersonated`): profile update, avatar,
  password, branch switch. Mount it on any new endpoint that changes the account itself.
- Everything is audited in the system log: see the [activity log](activity-log.md#acting-as-a-user).

**Tenant-exempt routes** (`tenancy.Exempt(responder)`) skip resolution — tenants, permissions, packages,
internal users. `tenancy.InternalOnly` additionally rejects tenant-bound identities, so a granted
system permission alone can never reach cross-tenant endpoints.


## Public routes (before sign-in)

Pages shown before sign-in (tenant branding, a public catalogue, a booking form) resolve the
tenant from headers instead of a user:

```http
GET /api/v1/public/tenant
X-Tenant-Code: 3f2a…
X-Branch-Id: 12            (optional; default: the tenant's first branch)
```

`tenancy.Public` answers a missing, unknown, deleted or inactive tenant with the same 422
`TENANT_CONTEXT_NOT_RESOLVED`, so codes cannot be probed by the error text. A branch of another
tenant is 422 too. The scope it produces has `Public: true`; services can refuse writes that must
not happen anonymously. Lookups are cached for five minutes (`tenant_code:*`) and invalidated by
the tenants service on update and delete.

Add a module's own public routes with a hook mounted in the public group of
`internal/app/routes.go`:

```go
// RegisterPublicTenantRoutes mounts routes used before sign-in.
func (h *Handler) RegisterPublicTenantRoutes(r chi.Router) {
	r.Get("/public/catalogue", h.publicCatalogue) // tenancy.From(r) is the resolved tenant
}
```

## Deleting a branch

`DELETE /api/v1/branch/{id}` soft-deletes a branch and answers 409 for the organization's last
branch, for a branch its members belong to (`users.branch_id`; move them first) and for the seeded
default branch. Members who had switched into the branch (`active_branch_id`) go back to their own
branch.

## Writes: the service stamps the tenant

In a Laravel app with a `TenantAware` trait, a model hook stamps `tenant_id` on every insert and
update. Here the stamp is one explicit argument. The service takes the tenant from the scope,
never from the request body, and passes it to the insert:

```go
// internal/modules/branches/service.go, shortened
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateParams) (*Branch, error) {
	tenantID := scope.EffectiveTenantID() // the stamp
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	return s.repo.Create(ctx, CreateInput{TenantID: tenantID, Name: input.Name, ActorID: &actorID})
}
```

Updates and deletes go through `scope.Filter()`, so they can only touch rows the caller's tenant
already owns. Request DTOs never carry a `tenant_id` field, so a client cannot override the stamp.
That covers what the update hook protects against in an ORM: no stale model can move a row to
another tenant.

| Laravel (`TenantAware`, middleware) | Here |
| --- | --- |
| `TenantScope` global scope on reads | `scope.Filter()` / `WHERE tenant_id = @tenant_id` in the query |
| `creating` / `updating` hooks stamp `tenant_id` | the service passes `scope.EffectiveTenantID()` to the insert |
| `$model->skipTenantStamping = true` | pass a different tenant ID explicitly (seeders, jobs) |
| `Model::forAllTenants()` | `tenancy.Unscoped()` (trusted code only) |
| `tenant.resolve` middleware | `tenancy.Resolve(...)` |
| `tenant.exempt` middleware | `tenancy.Exempt(responder)` (+ `tenancy.InternalOnly` for system-only routes) |
| `ResolvePublicTenantMiddleware` (`X-Tenant-Code`) | `tenancy.Public(...)` |
| `X-Acting-Tenant` / `X-Acting-Branch` | the same headers, the same rules |

## Queue jobs carry the scope

A queue envelope stores `tenant_id` and `branch_id`; the dispatcher restores the scope before the
job handler runs, so a job sees exactly the tenant that enqueued it. Enqueueing a tenant job
without a tenant is refused.

## Proving isolation

Every module that holds tenant data gets an isolation test: a second tenant's admin must get
**404** on read, update and delete of the first tenant's records.

```go
other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))
app.As(other).Get(fmt.Sprintf("/api/v1/branch/%d", id)).Status(t, http.StatusNotFound)
```

`internal/app/tenancy_isolation_integration_test.go` runs the matrix across modules. See
[Testing](./testing.md).

## Suspending a tenant

Setting a tenant's `is_active` to `false` suspends it:

```bash
curl -X PATCH localhost:8080/api/v1/tenant/7 -H "Authorization: Bearer $TOKEN"   -H "Content-Type: application/json" -d '{"name":"Acme","is_active":false}'
```

From their next request, the tenant's users get `403 TENANT_SUSPENDED` ("This organization's
account is suspended.") on every authenticated route, and signing in answers the same after the
password matches. No token is revoked, so reactivating (`"is_active": true`) restores access with
the tokens they already hold.

- **Still open:** `POST /api/v1/logout`, so a client can clean up its token.
- **Support keeps working:** platform accounts can still view as the tenant (`X-Acting-Tenant`) or
  act as one of its users (`X-Acting-User`). The check reads the caller's own tenant.
- **Agents too:** MCP tool calls run through the same gate and are refused with the same code.
- **Audited:** `tenant.suspended` and `tenant.reactivated` are system rows in the
  [activity log](activity-log.md), with who did it.
- **Public routes:** an inactive tenant answers 422 on `tenancy.Public` routes.

How it works: authentication already loads the user on every request; that query also reads
whether the user's tenant is active and not deleted (`Identity.TenantActive`).
`tenancy.Resolve` and `tenancy.Exempt` refuse the request when `Identity.TenantSuspended()`, so
every tenant and exempt route is covered without touching a module.

### Suspending on expired subscriptions

Off by default, because suspending customers is a business decision. With
`BILLING_SUSPEND_ON_EXPIRY=true`, the `billing.expire-subscriptions` task also suspends every
tenant whose subscription has been expired for `BILLING_SUSPEND_GRACE_DAYS` (default `3`) and
that has nothing active, in trial, pending or past due. The default tenant is never suspended.
Each suspension is a `tenant.suspended` row with `"reason": "subscription_expired"`.

## The organization and its owner

Inside a tenant, the tenant is **the organization**. Its members reach it without platform
permissions:

| Endpoint | Who | Does |
| --- | --- | --- |
| `GET /api/v1/organization` | any member | name, code, contact details, `owner` and `is_owner` |
| `PATCH /api/v1/organization` | `organization.update` (tenant Admin) | name, phone, e-mail; code, slug, domain and status stay with staff |
| `POST /api/v1/organization/transfer-ownership` | the owner | `{user_id, password}` hands ownership to an active member who holds `organization.update` |

Every organization can have one **owner** (`tenants.owner_user_id`): the person who signed it up,
or whoever ownership was transferred to. Until ownership moves, other members cannot delete the
owner, deactivate them, change their roles, set their password or reset their two-factor, nobody
can deactivate them, and the owner's account cannot be erased (self-deletion and admin erasure
answer 409). Platform super admins can still act, for support. Transfers are
`organization.ownership_transferred` activity entries.
