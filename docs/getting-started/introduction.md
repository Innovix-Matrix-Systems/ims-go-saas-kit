---
title: Introduction
description: What the IMS Go API Starter is, who it is for, and the ideas behind it.
---

The IMS Go API Starter is a **production-ready, multi-tenant HTTP API** written in Go:
**Laravel's batteries and workflow, Go's runtime, without the magic.** It is organised the way a
Laravel developer expects, and built the way Go code should be: explicit, typed and compiled into
one binary. You get a working application on day one — authentication
with two-factor, tenants and branches, roles and permissions, billing with plan limits, settings,
feature flags, mail, SMS and push, webhooks, files, background jobs, a scheduler, backups — and a
clear pattern for adding your own modules.

It ships as **one static binary**. The same `ims` binary serves HTTP, runs queue workers, runs the
scheduler, applies migrations and hosts maintenance commands. A developer dashboard (think Laravel
Telescope plus Horizon plus a little Nova) is embedded in it.

## Who it is for

- **Teams building a SaaS or internal platform** that serves many organisations (tenants), each
  with branches, users and roles.
- **Laravel developers moving to Go** who want Go's runtime and deployment story without giving up
  the conventions they rely on: controllers, form requests, `can:` middleware, artisan commands,
  queues, scheduled tasks, feature tests. [Coming from Laravel](./coming-from-laravel.md) maps each
  one, and says what you will miss.
- **Go developers** who want a codebase where every concern has an obvious home and the "boring"
  parts — errors, pagination, tenancy, audit trails — are already decided.

## What you get

| Area | Highlights |
| --- | --- |
| HTTP | chi router, RFC 9457 problem errors, envelopes, pagination, rate limiting, duplicate-write protection with an optional `Idempotency-Key`, OpenAPI as the contract |
| Identity | opaque tokens per device, OTP, two-factor by authenticator app or e-mail code, invitations, sessions users can see and revoke, login throttling, password reset, e-mail verification, sign-in with Google or GitHub (providers are drivers), every login in the activity log |
| Tenancy | tenants and branches, explicit `tenant_id` filters that fail closed, staff acting as a tenant or a user, public tenant routes |
| Access | permission catalog in Go, roles per tenant, Super-Admin, route-level checks, a tenant activity log and a system audit log |
| Billing | packages with features and limits, subscriptions, bKash/cash/bank payments, approvals |
| Platform | cache (memory/file/redis/database), River queue, cron scheduler, storage disks, i18n, one delivery system for mail, SMS and push (templates in English and Bangla, logged), notifications with per-user preferences |
| Integrations | outbound webhooks: tenants subscribe URLs to events, signed (Standard Webhooks), retried, logged |
| Data | sqlc type-safe SQL, goose migrations, transactions, advisory-lock quotas |
| Ops | health, Prometheus metrics, OpenTelemetry tracing, backups to S3/R2, systemd deploy with gated migrations, rollback, maintenance mode (`ims down` / `ims up`, also a dashboard toggle) |
| AI agents | an MCP server added as a connector (claude.ai, Claude Code, ChatGPT, Cursor), with built-in OAuth; tools are OpenAPI operations marked `x-mcp` |
| DX | `ims gen module`, `make rename`, `testkit`, hot reload, developer dashboard, AI-agent skills |

## The ideas behind it

1. **Explicit over magic.** No ORM, no global scopes, no reflection-driven wiring. Tenancy,
   transactions and dependencies are visible in the code that uses them. You can read a request
   from the route to the SQL without guessing.
2. **One direction of data flow.** Handler → service → repository → generated SQL. Each layer has
   one job and never skips the next. See [How it works](./how-it-works.mdx).
3. **Fail closed.** A forgotten tenant filter matches no rows. A route without a permission fails
   CI. A failed health check answers 503. Defaults are the safe choice.
4. **Laravel names for Laravel ideas, Go mechanics underneath.** Where Laravel has a well-known
   concept, this codebase uses the same shape and often the same name, so the mental model
   transfers. Underneath it is explicit Go: no ORM, container, facades or global scopes
   ([Coming from Laravel](./coming-from-laravel.md)).
5. **The code is the source of truth.** The OpenAPI spec, generated SQL code and these reference
   pages are checked in CI against the code, so they cannot quietly drift.

The full list of deliberate trade-offs — including what was *not* built and why — is in
[Design decisions](../architecture/decisions.md).

## What it is not

- **Not a framework.** It is an application you fork and own. Change anything.
- **Not database-agnostic.** v1 is PostgreSQL only, on purpose (see decision 22).
- **Not a frontend.** It is an API. The only UI is the developer dashboard; your product's admin
  app (billing screens, tenant management for staff) is a client of the API.

Ready? Head to the [Quick start](./quick-start.mdx).
