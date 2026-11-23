---
title: Migrations
---

Migrations are **goose** SQL files embedded in the binary (`migrations`) and are the single
schema source for both goose and sqlc.

## Policy

- Migrations run as an **explicit, gated release step** (`ims migrate`), never on application boot
  or replica start. `release.sh` runs them before switching the `current` symlink.
- **Expand/contract** for multi-instance safety:
  1. *Expand* — additive, backwards-compatible changes (new nullable columns, new tables, new
     indexes `CONCURRENTLY`) ship with the release.
  2. Deploy the code that uses them.
  3. *Contract* — removing columns/tables — ships one release later, once nothing reads them.
- A migration that must not run under live traffic (a long table rewrite, a data fix) runs inside
  [maintenance mode](../guides/maintenance-mode.md): `ims down --with-secret`, `ims migrate`,
  smoke-test with the bypass header, `ims up`. Prefer expand/contract; use this when you can't.
- Never edit a migration that has shipped; add a new one.
- `internal/dbgen` is generated from the migrations — run `make sqlc` and commit the result
  (CI fails on a generated-code diff).

## Commands

```bash
ims migrate            # apply all pending migrations
ims migrate:status     # applied vs pending versions
ims migrate:rollback   # revert the most recent migration (goose Down)
```

The health endpoint flags schema drift (embedded vs applied versions), so a release that forgets to
migrate surfaces immediately.

## Concurrency

`ims migrate` acquires goose's advisory lock; run it once per deploy, not per replica. In
multi-instance setups, run migrations from the release step on a single host (or a dedicated
migration job) before rolling the units.

## SQLite

Not supported in v1 (PostgreSQL only). The SQLite adapter is deferred to a future version; see
[design decision 22](../architecture/decisions.md).

## Recovering from a failed migration

1. Read the error (`ims migrate` output / journald).
2. Fix forward with a new migration where possible.
3. To undo the last applied migration: `ims migrate:rollback`, then correct the file and re-apply.
4. Restore from backup if a data migration went wrong (see the DB-specific runbook).
