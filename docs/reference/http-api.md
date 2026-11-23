---
title: HTTP API
description: The contract, the interactive reference, envelopes, headers, pagination and errors in one place.
---

## The contract

`api/openapi.yaml` (OpenAPI 3.0) is the **canonical** description of the API. It is served
verbatim and rendered with Scalar at **`/docs`** on every running instance (`DOCS_ENABLED`, on by
default outside production) — try requests there with your token.

The contract is enforced, not aspirational:

- `make openapi-check` fails when a route exists on the router but not in the spec, or the
  other way round ("route drift"), and when generated sections are stale.
- `internal/contract` (types generated with oapi-codegen) is regenerated in CI and must match.
- Module sections marked as generated are refreshed from the code by `ims gen openapi`.

## Base URL and versioning

Everything lives under `/api/v1`. Breaking changes get a new version prefix; additive changes
(new fields, new endpoints) don't.

## Headers

| Header | When |
| --- | --- |
| `Authorization: Bearer <id>\|<secret>` | every authenticated request |
| `Content-Type: application/json` | request bodies (multipart for uploads) |
| `Accept-Language: en \| bn` | localised messages |
| `X-Acting-Tenant`, `X-Acting-Branch` | platform accounts working inside one tenant (needed for writes to tenant resources; without them, reads span every tenant) |
| `X-Acting-User` | acting as one user, with their permissions and tenant (`user.impersonate`; recorded in the system audit log) |
| `X-Tenant-Code`, `X-Branch-Id` | public routes that need a tenant |
| `X-Request-ID` (response) | the request's id — quote it in bug reports |
| `Idempotency-Key` | optional on any write: send a unique value (a UUID) and reuse it when you retry; the first answer is replayed for 24 h ([below](#retries-and-duplicates)) |
| `Idempotent-Replayed: true` (response) | this answer is a replay of an earlier request with the same key |

The MCP server is the exception: `/mcp` takes an MCP access token from its OAuth flow
(`/oauth/authorize`, `/oauth/token`), never a session token, and `/api/v1` refuses MCP tokens.
See [MCP server](../guides/mcp.md).

## Responses

| Case | Shape |
| --- | --- |
| a resource | `{"data": {…}}` |
| a page | `{"data": […], "links": {"first","last","prev","next"}, "meta": {"current_page","per_page","total",…}}` |
| a cursor feed | `{"data": […], "links": {"next"}, "meta": {"next_cursor",…}}` |
| a message | `{"message": "…"}` |
| nothing | `204 No Content` |
| an error | `application/problem+json` — RFC 9457 with `error_code`, `errors`, `meta` |

Pagination parameters: `page` + `per_page` (default 10, max 100) for management lists; `limit` +
`cursor` for feeds. Sorting: `order_by` (whitelisted per endpoint) + `order_direction`
(`asc`/`desc`). Search: `search`.

## Retries and duplicates

Writes (`POST`, `PUT`, `PATCH`, `DELETE`) are protected against running twice:

- **Without a key**, the same user sending the same method, path and body while the first request
  runs, or within a few seconds after it succeeded, gets `409 DUPLICATE_REQUEST` with
  `Retry-After` (a double click). A request that failed can be retried at once.
- **With `Idempotency-Key`**, a retry with the same key and body gets the first answer again
  (`Idempotent-Replayed: true`), even minutes later, and nothing runs twice. The same key with a
  different body answers `422 IDEMPOTENCY_KEY_REUSED`; while the first is still running,
  `409 IDEMPOTENCY_IN_PROGRESS`. Server errors are not kept, so retrying after a 5xx runs again.

Send a key whenever a retry must not repeat the action — payments above all, and any client on a
flaky network (mobile apps).

## Conventions

- JSON keys are `snake_case`.
- Timestamps are RFC 3339 in UTC.
- Money is an integer in **minor units** (paisa) of `BILLING_CURRENCY`.
- IDs are 64-bit integers.
- Booleans are real booleans; absent optional fields are `null` or omitted.

## Status codes you will see

| Code | Means |
| --- | --- |
| 200 / 201 / 204 | success |
| 400 | malformed request (bad JSON, bad id) |
| 401 | missing or invalid token |
| 402 | plan limit reached or no subscription (`meta.feature_key`) |
| 403 | authenticated, but not allowed (permission, write without acting headers) |
| 404 | not found — **including other tenants' records** |
| 409 | conflict (duplicate record, invalid state, a write sent twice: `DUPLICATE_REQUEST`) |
| 415 | unsupported content type |
| 422 | validation failed (`errors`) |
| 429 | rate limited (`retry_after`) |
| 503 | not ready (health), a gateway is unavailable, or acting as a user while the audit log cannot be written |

Full detail: [Validation & errors](../guides/validation-and-errors.md) and
[Handlers & routes](../guides/handlers-and-routes.md).
