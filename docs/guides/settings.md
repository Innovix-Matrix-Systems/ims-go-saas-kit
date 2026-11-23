---
title: Settings
description: A typed, grouped settings catalog with global values and per-tenant overrides.
---

Settings are runtime-editable values — a display name, whether notifications fan out, how long to
keep import jobs. They are declared in a typed catalog, stored in the `settings` table, and cached
per target.

## The catalog

`internal/modules/settings/registry.go`:

```go
{
	Name:        "notification.enabled",
	Type:        TypeBool,          // TypeString | TypeInt | TypeBool | …
	Default:     true,
	Description: "Toggles outbound notification fan-out.",
	Group:       "notifications",
	SortOrder:   10,
	Scope:       ScopeTenant,       // ScopeTenant: tenants may override; ScopeGlobalOnly: they can't
},
```

`ims settings:sync` creates the groups and any missing global rows **without** overwriting values
or group assignments made in the dashboard. Values are validated against the type (an int setting
refuses `1.9` instead of truncating it).

## Reading a setting

```go
enabled, err := s.settings.GetBool(ctx, scope, "notification.enabled")
days, err := s.settings.GetInt(ctx, scope, "data_processing_job.cleanup_days")
```

Resolution is **tenant override → global row → catalog default**. One cache blob per target
(global, each tenant) keeps reads cheap; writes invalidate their own blob.

## Endpoints

| Endpoint | Who |
| --- | --- |
| `GET /api/v1/settings` | any tenant user — effective values, grouped |
| `PATCH /api/v1/settings/tenant/{key}` | tenant admins (`setting.update`) — the tenant's override |
| `GET/PATCH/DELETE /api/v1/internal/settings[/{key}]` | platform staff — any target |
| `/api/v1/internal/settings-groups` | platform staff — group CRUD |
| `PUT /api/v1/internal/settings/{key}/group` | move a key to another group |

## In the dashboard

**Settings** shows every key with its global value and lets you pick a tenant to edit its
overrides inline. Drag keys between groups, reorder groups, and drag ungrouped keys from the side
rail into a group. Every change is audited.

## Settings or feature flags?

| Use a **setting** for | Use a **feature flag** for |
| --- | --- |
| a value (string, number, boolean) | an on/off switch for code paths |
| configuration tenants may own | rolling something out gradually |
| grouped, documented options | per-user or per-tenant targeting |

See [Feature flags](./feature-flags.md).
