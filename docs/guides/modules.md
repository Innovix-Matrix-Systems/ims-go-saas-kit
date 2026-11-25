---
title: Modules and the generator
description: How a module is organised, what `ims gen module` produces, and the recipes for adding endpoints and fields.
layers: [database, api]
---

Every business capability — users, branches, billing, your products — is a **module** under
`internal/modules/<name>`. A module owns its routes, handlers, DTOs, service, repository and model.
Modules never import each other's services; when one needs data from another, it declares a small
interface (a `…Lookup` or `LimitChecker`) and the composition root plugs in the implementation.

## Anatomy

| File | Holds | Must not |
| --- | --- | --- |
| `routes.go` | paths → handlers, each with its permission (`r.With(h.can(perm))`) | contain logic |
| `handler.go` | bind (`h.Bind`), call the service, respond (`h.Data` / `h.Error`) | contain SQL or business rules |
| `request.go` | request DTOs, response DTOs, `NewXResponse` mappers | contain logic |
| `service.go` | business rules, invariants, tenancy, transactions, activity | import `httpx` or `authn` |
| `repository.go` | `Repository` interface + sqlc-backed implementation, `database.MapError` | import `httpx` or `authn` |
| `model.go` | domain structs and row mappers — `dbgen` types never leak out | — |
| `commands.go`, `jobs.go` | optional CLI commands and queue jobs | — |

Small and medium modules are **flat**. A big module like billing keeps
`routes/handler/request` and its gateway adapters at the root, shared types in `domain/`, and
business services in `service/` — that layout is what keeps the packages free of import cycles.

**Copy from these:** `branches` (small and clean), `identity` (users — rich: photos, import/export,
roles), `billing` (big, with subpackages).

## Generate a module

```bash
go run ./cmd/app gen module --name=product \
  --fields="name:string,description:text?,price:decimal,is_active:bool"
```

The generator is deliberately conservative: it creates a **tenant-scoped, soft-deleting** module
with `id`, `tenant_id`, audit columns (`created_by`, `updated_by`) and timestamps. It writes:

- `migrations/000NN_create_products.sql` — goose up/down, tenant index, `deleted_at`
- `sql/queries/products.sql` — tenant-filtered CRUD, search and count
- `internal/modules/product/{model,repository,service,request,handler,routes}.go`
- `internal/modules/product/service_test.go` — unit tests that need no database
- the managed `/api/v1/product` section of `api/openapi.yaml`

…and then runs `sqlc generate` and refreshes the OpenAPI section, so the result compiles.

| Field type | Postgres | Go |
| --- | --- | --- |
| `string` | `text` (max 255 in validation) | `string` |
| `text` | `text` | `string` |
| `int` / `bigint` | `integer` / `bigint` | `int32` / `int64` |
| `bool` | `boolean` | `bool` |
| `decimal` | `bigint` (minor units: 12.50 → 1250) | `int64` |
| `date` / `datetime` | `timestamptz` (always nullable) | `*time.Time` |
| `json` | `jsonb` (always nullable) | `json.RawMessage` in requests, `[]byte` in the model |

Append `?` for a nullable field (`notes:text?` → `*string`). Flags: `--table` (the naive plural is
`product` → `products`), `--package`, `--no-sqlc`, `--no-openapi`, `--dry-run`, `--force`.

After generating, three things are always yours to do — the [tutorial](../tutorial/first-module.mdx)
walks through each:

1. **Wire it**: a field on `Dependencies` and its constructor in `internal/app/wiring.go`, and
   `d.Products.RegisterRoutes(r)` in the right group of `internal/app/routes.go`.
2. **Declare the permissions** in `internal/authz/catalog.go`, then `ims permission:sync`.
3. **Review the defaults**: `required` on numbers rejects 0; add unique indexes; add business
   rules to the service.

:::caution[Global or branch-scoped resources]
The scaffold assumes tenant scope. For a **global** resource, mount it in the tenant-exempt group
*and* remove the `tenant_id` filters from the generated SQL, repository and service. For a
**branch-scoped** resource, add `branch_id` to the table and filter on `EffectiveBranchID()`.
:::

## Add an endpoint to an existing module

1. **`request.go`** — the request DTO (with `validate` tags) and the response DTO + mapper.
2. **`service.go`** — the method: tenancy from the scope, business rules, `WithTx` if it writes
   more than one row, an activity entry if tenant admins should see it.
3. **`sql/queries/<name>.sql` + `repository.go`** — only if it needs new SQL; then `make sqlc`.
4. **`handler.go`** — `h.Bind` (or `h.PathID` for `{id}`) → service → `h.Data` / `h.Error`.
5. **`routes.go`** — the path with its permission: `r.With(h.can(PermX)).Post(...)`.
6. **`api/openapi.yaml`** — document it by hand, or run `ims gen openapi --module=<name>` for a
   scaffold-style module (refreshes only the managed section).
7. **`make openapi`** regenerates `internal/contract`; **`make openapi-check`** verifies the spec
   matches the router.
8. Tests, then `make check`.

## Add a field to an existing resource

1. `migrations/000NN_add_<field>_to_<table>.sql` — goose up **and** down.
2. `sql/queries/<name>.sql` — the column in selects, inserts and updates.
3. `make sqlc` — `internal/dbgen` updates.
4. `model.go` — the domain struct and row mapper.
5. `request.go` — request and response DTOs, and the `NewXResponse` mapper.
6. `api/openapi.yaml`, then `make openapi` and `make openapi-check`.
7. Update tests; `make check`.

:::tip[The cross-cutting rule]
Touched SQL? Run `make sqlc` and commit `internal/dbgen`. Touched the contract? Run
`make openapi`. Never edit `internal/dbgen` or `internal/contract` by hand — `make gen-check` (and
CI) will catch it.
:::

## Loading related data without N+1

There is no ORM and no lazy loading. A repository returns exactly what its query selects, and the
handler or service decides what to attach — **in bulk**:

```go
// one extra query per list, never one per row
ids := make([]int64, 0, len(users))
for _, u := range users {
	ids = append(ids, u.ID)
}
photos, err := h.photos.PhotoURLs(ctx, ids) // map[int64]string
```

- A join in the repository is also fine when the relation is naturally part of the row set.
- Related data appears in responses as a **slim** object (`RoleSlim`, `TenantSlim`): an id and a
  few display fields, never the full resource.
- A missing or soft-deleted relation is omitted (`nil` + `omitempty`), never faked.
- Never call a repository inside a `for` over rows you are about to return.
