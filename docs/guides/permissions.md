---
title: Roles and permissions
description: The permission catalog in Go, tenant roles, Super-Admin, and where permissions are checked.
---

## The catalog

Every permission is declared **once**, in Go, in `internal/authz/catalog.go`:

```go
{Name: PermBranchCreate, Group: "branch", Description: "Can create branches"},
{Name: PermRoleInternalCreate, Group: "role", IsSystem: true, Description: "Can create new internal roles"},
```

`ims permission:sync` upserts the catalog into the `permissions` table (run it after adding one;
`make seed` does too). The generated [permissions reference](../reference/permissions.md) lists
all of them with the seeded roles that hold each.

- **System** permissions (`IsSystem: true`) are platform-only: they are never granted to tenant
  roles and cannot be edited from tenant surfaces.
- Name them `<resource>.<action>` — `product.view`, `product.view.all`, `product.create`,
  `product.update`, `product.delete`.

## Roles

| Role | Scope | Holds |
| --- | --- | --- |
| **Super-Admin** | platform (system role) | bypasses every check — `Checker.Can` returns true |
| **Developer** | platform | the `dev.*` permissions (developer dashboard) and `user.impersonate` (act as a tenant user) |
| **Admin** | per tenant | the tenant-level groups: `role`, `branch`, `user`, `setting`, `activity` |
| custom roles | per tenant | whatever a tenant admin assigns |

Every new tenant gets its Admin role from `authz.AdminTenantPermissions()`. Add a group to that
list (as the [tutorial](../tutorial/first-module.mdx#5-register-the-permissions) does for
`product`) and new tenants' admins get it.

**Invariants** the services enforce for every caller: the Super-Admin cannot be stripped of its
role or deleted, users cannot escalate their own roles, and system roles are immutable from tenant
surfaces.

## Where permissions are checked

**On the route**, like Laravel's `can:` middleware:

```go
r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)
```

**Inside a handler** only when the permission depends on the payload:

```go
if err := h.checker.MustCan(r.Context(), authn.UserID(r), authz.PermUserRoleAssign); err != nil {
	h.Error(w, r, err)
	return
}
```

**Not in services, jobs, commands or scheduled tasks.** Those are trusted code (decision 23).
A service that needs a Gate-style decision — "may this actor touch that record?" — takes an
`*authz.Checker` explicitly.

## Caching

`Checker` caches each user's permission set for 10 minutes and invalidates it whenever roles or
permissions change through the API. If you change roles directly in the database, run
`ims cache:clear`.

## The safety net

`internal/app/route_permissions_integration_test.go` walks the live router and asserts that a
token **without permissions** is refused on every protected route. A route mounted without
`h.can(...)` fails CI. Routes deliberately open to every authenticated user (your own profile,
notifications) are listed in that test's allowlist with a reason.

## Managing roles through the API

| Endpoint | Permission |
| --- | --- |
| `GET /api/v1/role`, `GET /api/v1/role/{id}` | `role.view.all`, `role.view` |
| `POST /api/v1/role`, `PATCH /api/v1/role/{id}`, `DELETE /api/v1/role/{id}` | `role.create`, `role.update`, `role.delete` |
| `POST /api/v1/user/assign-role` | `user.role.assign` |
| `GET /api/v1/permission` | permission metadata (tenant-exempt) |

The developer dashboard's **Inspector → Roles & permissions** shows the live catalog, every role
and any drift between the catalog and the database.
