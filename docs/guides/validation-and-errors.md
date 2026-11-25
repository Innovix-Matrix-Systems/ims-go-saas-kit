---
title: Validation and errors
description: Structural validation on requests, business validation in services, and the one RFC 9457 error format.
layers: [api, app]
---

## Two kinds of validation

| Kind | Where | Example | Tool |
| --- | --- | --- | --- |
| **Structural** — is the input well-formed? | the request DTO | "price must be ≥ 0", "email is an e-mail" | `validate:"…"` tags + `h.Bind` |
| **Business** — is it allowed? | the service | "this SKU is already used", "the branch belongs to another tenant" | `apperr.Validation(...)` |

### Structural: struct tags

```go
type InsertUpdateRequest struct {
	Name     string  `json:"name" validate:"required,min=2,max=255"`
	Email    string  `json:"email" validate:"required,email,max=255"`
	Password *string `json:"password" validate:"omitempty,min=8,max=72"`
	BranchID *int64  `json:"branch_id" validate:"omitempty,gt=0"`
	Roles    []int64 `json:"roles" validate:"omitempty,dive,gt=0"`
}
```

`h.Bind` decodes **strict** JSON (unknown fields are rejected when `HTTP_STRICT_JSON_FIELDS=true`)
and runs the tags with [go-playground/validator](https://github.com/go-playground/validator).
Messages are humanised and translated (`Accept-Language: en | bn`).

:::caution[`required` on numbers means "not zero"]
For `int` fields use `gte=0` (or a pointer plus `required` when "missing" and "zero" must differ).
:::

### Business: from the service

```go
if exists {
	return nil, apperr.Validation(map[string][]string{
		"email": {"The email has already been taken."},
	})
}
```

Field keys use dot paths for nested input (`items.0.quantity`).

## Errors are values, responses are uniform

Services and repositories return **`*apperr.Error`** values (`internal/apperr`); handlers pass any
error to `h.Error`, which renders the right problem. **Never write a JSON error by hand.**

```go
return nil, apperr.New(apperr.BusinessLogicError, "This package does not offer a trial period.")
return nil, apperr.Newf(apperr.ResourceNotFound, "Product with ID '%d' was not found.", id)
return apperr.Wrap(apperr.InternalServerError, "encode overrides", err) // keeps the cause for logs
```

Repositories translate driver errors with `database.MapError("module.op", err)`:

| Postgres | Becomes | HTTP |
| --- | --- | --- |
| `pgx.ErrNoRows` | `ResourceNotFound` | 404 |
| `23505` unique violation | `ResourceAlreadyExists` | 409 |
| `23503` foreign key violation | `Conflict` | 409 |
| `23502` not null violation | `ValidationFailed` | 422 |

## The problem format

Every error — validation, permission, not found, rate limit, a panic — is
`application/problem+json` ([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)):

```json
{
  "type": "https://api.example.com/errors/usage-limit-exceeded",
  "status": 402,
  "detail": "The plan limit for this feature has been reached.",
  "instance": "/api/v1/user",
  "error_code": "USAGE_LIMIT_EXCEEDED",
  "timestamp": "2026-11-20T10:15:04Z",
  "meta": { "feature_key": "users.max", "limit": 5, "current": 5 }
}
```

| Member | When |
| --- | --- |
| `type` | always — `APP_URL/errors/<code>` |
| `error_code` | always — a stable machine-readable code clients switch on |
| `errors` | 422 — `{"field": ["message", …]}` |
| `meta` | when there is structured context (plan limits) |
| `retry_after` | 429 — seconds until the next attempt |
| `trace_id` | 5xx — quote it when reporting a bug; it is in the logs |
| `debug` | outside production when `APP_DEBUG=true` |

Common codes: `VALIDATION_FAILED` (422), `UNAUTHORIZED` (401), `FORBIDDEN` (403),
`RESOURCE_NOT_FOUND` (404), `RESOURCE_ALREADY_EXISTS` / `CONFLICT` (409), `DUPLICATE_REQUEST` (409, the same write sent twice; see [retries](../reference/http-api.md#retries-and-duplicates)), `IDEMPOTENCY_KEY_REUSED` (422) / `IDEMPOTENCY_IN_PROGRESS` (409),
`INSUFFICIENT_PERMISSIONS`, `IMPERSONATION_FORBIDDEN` (403, a refused `X-Acting-User`),
`SOCIAL_ACCOUNT_NOT_FOUND` (403, a social login for an e-mail no user has), `TENANT_SUSPENDED` (403, the user's tenant is [suspended](tenancy.md#suspending-a-tenant)), `MAINTENANCE_MODE` (503 with `Retry-After`, the app is down for [maintenance](maintenance-mode.md); its `detail` is the message given to `ims down`), `SUBSCRIPTION_REQUIRED` / `USAGE_LIMIT_EXCEEDED` (402),
`RATE_LIMIT_EXCEEDED` (429), `TENANT_CONTEXT_NOT_RESOLVED`, `INTERNAL_SERVER_ERROR` (500). The
full list is in `internal/apperr`.

## In the app

The app's API client turns every problem response into an `ApiError` with the same fields: `useApiErrors()` shows a 422's field errors under the matching inputs, and other errors in a box with the `trace_id` to quote. App-wide answers (401, two-factor required, suspended, maintenance) switch screens instead. See [The app](../app/overview.md#how-a-screen-talks-to-the-api).
