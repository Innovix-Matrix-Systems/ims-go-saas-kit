---
title: Feature flags
description: Boolean flags resolved per user, tenant and globally, with stable percentage rollouts.
layers: [api, app, dashboard]
---

Feature flags switch code paths on and off without a deploy. They are a separate, boolean-only
catalog (so reading a flag never loads settings) with targets and percentage rollouts.

## Declaring a flag

`internal/modules/featureflags/registry.go`:

```go
{
	Key:         "feature.data_processing",
	Description: "Enables background import/export processing.",
	Default:     true,
	Group:       "features",
	// RolloutBy: RolloutByTenant — bucket whole tenants instead of users
},
```

`ims feature-flags:sync` stores missing globals.

## Resolution

**User → tenant → global → catalog default.** The first target with a value wins, so you can turn
a flag on globally and off for one tenant, or on only for one user.

## Percentage rollout

An enabled value can carry a percentage:

```json
PATCH /api/v1/internal/feature-flags
{ "target_type": "global", "flags": { "feature.new_checkout": true }, "rollout": { "feature.new_checkout": 20 } }
```

Each user (or tenant, with `RolloutBy: RolloutByTenant`) hashes to a fixed bucket 0–99 **per
flag**. So:

- the same user always gets the same answer;
- raising 20% → 60% only **adds** users — nobody who had it loses it;
- 100% is stored as plain "on";
- a `null` flag value resets that target, so it falls back to the next level.

The rollout is decided per request **after** resolution, so a user or tenant override still pins
the value for that target.

## Gating a route

```go
r.With(featureflags.Require(resolver, "feature.data_processing", h.Responder)).
	Post("/data-processing/export", h.export)
```

Requests where the flag is off get a problem response before the handler runs.

## Endpoints

| Endpoint | Who |
| --- | --- |
| `GET /api/v1/feature-flags` | any user — the flags as they resolve for the caller |
| `GET/PATCH /api/v1/internal/feature-flags` | platform staff — any target, with rollout |
| `DELETE /api/v1/internal/feature-flags/{key}` | reset a target's value |

In the dashboard, **Feature Flags** shows every flag per target with a rollout slider.

## In the app

Staff manage flags and their rollout under **Feature flags** in the console (`/console/flags`). Screens read a flag through the API like any other value.
