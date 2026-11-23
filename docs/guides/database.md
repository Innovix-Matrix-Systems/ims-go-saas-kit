---
title: Database and SQL
description: Migrations with goose, type-safe queries with sqlc, transactions, quotas under advisory locks, and the gotchas.
---

PostgreSQL is the only database (v1 is Postgres-only, on purpose — decision 22). There is no ORM:
you write SQL, [sqlc](https://sqlc.dev) turns it into typed Go, and repositories call that Go.

```text
migrations/*.sql  ──┐
                    ├──►  sqlc generate  ──►  internal/dbgen (Go)  ──►  repository.go
sql/queries/*.sql ──┘
```

## Migrations

[goose](https://github.com/pressly/goose) SQL files in `migrations/` are the **only** schema
source — sqlc reads them too. They are embedded in the binary, so `ims migrate` works on a server
with no source tree.

```sql title="migrations/00026_create_example.sql"
-- +goose Up
CREATE TABLE example (
    id         bigserial PRIMARY KEY,
    tenant_id  bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name       text NOT NULL,
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX example_tenant_idx ON example (tenant_id);
CREATE UNIQUE INDEX example_tenant_name_unique ON example (tenant_id, name) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE example;
```

**Conventions:** `tenant_id` (and `branch_id`) on scoped tables · `created_by`/`updated_by`
stamped by services · `deleted_at` soft deletes with **partial** unique indexes
(`WHERE deleted_at IS NULL`) · `timestamptz` · `jsonb` · `bigserial` ids.

| Command | Does |
| --- | --- |
| `ims migrate` | applies goose migrations, then River's queue tables |
| `ims migrate:status` | every migration and whether it ran, plus the River schema version |
| `ims migrate:rollback` | rolls back the last goose migration (River first when needed) |

An advisory lock serialises concurrent `migrate` runs. **Migrations never run on boot**: `make dev`
runs `migrate` once before starting; in production it is a gated release step.

:::caution
Comments must not contain the literal `+goose` — goose parses it as a directive. Never edit a
migration that has run anywhere but your machine; add a new one. Constraints on big existing
tables use `ADD CONSTRAINT … NOT VALID` (see `00020_indexes_and_constraints.sql`).
:::

## Queries

Write queries in `sql/queries/<module>.sql`, one per annotated block:

```sql title="sql/queries/branches.sql"
-- name: GetBranch :one
SELECT * FROM branches
WHERE id = @id
  AND (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND deleted_at IS NULL;

-- name: ListBranches :many
SELECT * FROM branches
WHERE tenant_id = @tenant_id
  AND deleted_at IS NULL
  AND (@search::text = '' OR name ILIKE '%' || @search::text || '%')
ORDER BY
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN name END ASC,
  id DESC
LIMIT @lim OFFSET @off;
```

- `:one`, `:many`, `:exec`, `:execrows` choose the return shape.
- `@name` is a required parameter; `sqlc.narg('name')` is nullable (a `*T` in Go).
- The tenant predicate `(@all_tenants::boolean OR tenant_id = @tenant_id)` is what
  `tenancy.Filter` renders — see [Tenancy](./tenancy.md).
- Ordering is whitelisted with `CASE WHEN`, never string-built.

Then regenerate and commit:

```bash
make sqlc       # pinned sqlc v1.31.1 → internal/dbgen
```

CI runs sqlc and fails on a diff, so `internal/dbgen` is always in sync.

## Repositories

A repository is an interface plus a sqlc-backed implementation. It maps `dbgen` rows to domain
types in `model.go`, and translates driver errors:

```go
func (r *repository) Get(ctx context.Context, filter tenancy.Filter, id int64) (*Branch, error) {
	row, err := r.queries.GetBranch(ctx, dbgen.GetBranchParams{
		ID: id, AllTenants: filter.AllTenants, TenantID: filter.TenantID,
	})
	if err != nil {
		return nil, database.MapError("branch.get", err) // no rows → 404, unique → 409, …
	}
	branch := mapBranch(row)
	return &branch, nil
}
```

## Transactions

Services own transaction boundaries. `TxManager.WithTx` gives you a `*dbgen.Queries` bound to one
transaction; build repositories from it:

```go
err := s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
	roles := access.NewRoleRepository(q)
	if err := roles.Detach(ctx, userID); err != nil {
		return err // rolls back
	}
	return roles.Attach(ctx, userID, ids)
})
```

### Quotas: `WithTenantLock`

For "count, check the plan limit, insert", `WithTenantLock` takes a per-tenant **advisory lock**
inside the transaction, so concurrent requests cannot both pass the check:

```go
// Read the plan limit BEFORE the lock …
allow, err := s.limits.QuotaFor(ctx, tenantID, "users.max")
if err != nil {
	return err
}
return s.tx.WithTenantLock(ctx, "users.max", tenantID, func(q *dbgen.Queries) error {
	repo := NewRepository(q) // … and use only q inside it
	count, err := repo.Count(ctx, ListFilter{TenantID: tenantID})
	if err != nil {
		return err
	}
	if err := allow(count); err != nil {
		return err // 402 USAGE_LIMIT_EXCEEDED
	}
	return create(repo)
})
```

:::danger[Inside a lock, only use `q`]
Every query inside `WithTenantLock` must run on the locked transaction (`q`). A call that takes a
**second** pool connection deadlocks as soon as concurrent requests for one tenant hold the whole
pool while waiting for the lock. Read anything else before taking the lock.
`TestQuotaUnderConcurrency` fires 8 creates on a 3-connection pool to keep this honest.
:::

## Gotchas

- **Nullable columns from `LEFT JOIN LATERAL`**: sqlc infers nullability from the schema, not the
  join. Wrap them: `coalesce(s.id, 0)::bigint AS subscription_id`.
- **Casting a bigint parameter to text**: `CAST(@tenant_id AS bigint)::text` — a bare
  `@tenant_id::text` gets a bad generated name.
- **Reserved words** (`"group"`): quote them and rename the parameter (`@group_name`).
- **Explicit ids** (seeding): call the repository's `SyncIDSequence` afterwards.
- **`ON CONFLICT … DO NOTHING RETURNING *`** returns `pgx.ErrNoRows` on conflict — treat it as
  "already exists".
- **Extensions** such as `pg_trgm` aren't visible across the per-package test schemas; avoid
  indexes that need them in migrations.
- **River's tables** are managed by `ims migrate` — never by hand.

## Connections and timeouts

`DATABASE_DSN` is the primary pool; `DATABASE_CONNECTIONS=primary,analytics` adds named extras
(`DATABASE_ANALYTICS_DSN`, …). Pools connect lazily, so the API boots while the database is down
and reports it through `/api/health`. Server-side backstops: `DATABASE_STATEMENT_TIMEOUT` kills
runaway queries and `DATABASE_IDLE_IN_TX_TIMEOUT` kills sessions idling inside a transaction. See
the [configuration reference](../reference/configuration.md#database).
