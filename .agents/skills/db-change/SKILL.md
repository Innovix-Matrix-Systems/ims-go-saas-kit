---
name: db-change
description: Use when changing the database schema or SQL in this repo — new goose migration, new or edited sqlc query, regenerating internal/dbgen, nullable output columns, bigserial sequence sync, soft-delete indexes, applying migrations to the dev database, or when CI's generated-code diff check fails.
---

# Database changes

Goose migrations in `migrations/` are the **only** schema source (sqlc parses them). Queries
live in `sql/queries/`. `internal/dbgen` is generated — never edit it.

## Adding a migration

1. Next number: `migrations/000NN_<snake_name>.sql` (check `ls migrations | tail`).
2. Structure:

```sql
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

3. Rules: `tenant_id`/`branch_id` on scoped tables; audit columns; `deleted_at` for soft deletes;
   partial unique indexes; `jsonb`; `timestamptz`. Constraints on existing big tables:
   `ADD CONSTRAINT ... NOT VALID` (see `00020_indexes_and_constraints.sql`).
4. Comments must not contain the literal `+goose` (goose parses it as a directive).
5. Apply: `go run ./cmd/app migrate` (also River); check `go run ./cmd/app migrate:status`.
   Migrations never run on boot; `make dev` runs `migrate` once before starting, so run it yourself
   after adding one while `make dev` is already running.

## Adding or editing queries

1. `sql/queries/<module>.sql` with `-- name: QueryName :one|:many|:exec|:execrows`.
2. Parameters: `@name` (required), `sqlc.narg('name')` (nullable → `*T`). Tenant predicate for
   single-row reads/writes: `(@all_tenants::boolean OR tenant_id = @tenant_id)`.
   Optional text filters: `(@search::text = '' OR name ILIKE '%' || @search::text || '%')`.
   Whitelisted ordering: `CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN name END`.
3. Regenerate: `make sqlc` (sqlc v1.31.1). On Windows, if Git Bash cannot run it, use PowerShell:
   `sqlc generate` (install with `CGO_ENABLED=1 go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`).
4. Commit the regenerated `internal/dbgen` (CI and `make gen-check` enforce the diff).
5. Map rows in `model.go`; wrap driver errors with `database.MapError("module.op", err)`.

## Gotchas

- **Nullable output columns**: sqlc infers nullability from the schema, not from `LEFT JOIN
  LATERAL`. Wrap such columns: `coalesce(s.id, 0)::bigint AS subscription_id`,
  `coalesce(s.status, '')::text AS status`. Subqueries returning counts need `::bigint`.
- **Casting a bigint param to text**: `CAST(@tenant_id AS bigint)::text` (a bare
  `@tenant_id::text` gets a bad generated name).
- Reserved words: quote them (`"group"`) and rename params (`@group_name`).
- **Sequences**: after inserting explicit ids (seeder), call the repository's `SyncIDSequence`
  (`modules/tenants`, `modules/branches`; used by `internal/seeder`). In tests,
  `dbtest.SyncSequence(t, pool, "table")`.
- `ON CONFLICT ... DO NOTHING RETURNING *` returns `pgx.ErrNoRows` on conflict — treat it as
  "already exists" (see `branches` `CreateWithID`).
- Extensions such as `pg_trgm` are not visible across the per-package test schemas; avoid indexes
  that need them in migrations (document a recipe instead).
- River tables are managed by `rivermigrate` through `ims migrate` — never by hand.
- Test schemas: `dbtest` migrates a schema per test package. `testkit.New` empties every
  application table; `dbtest.Clean(t, pool, "table", ...)` for repository-level tests.
- Verify with the DSN set: `$env:DATABASE_TEST_DSN=...; go test -count=1 ./...`.
