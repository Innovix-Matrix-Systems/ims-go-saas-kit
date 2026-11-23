---
title: Rollback
---

## Fast path

```bash
sudo deploy/scripts/release.sh --rollback --dry-run   # preview
sudo deploy/scripts/release.sh --rollback             # switch to the previous release
```

`--rollback` re-points `current` at the next-most-recent release and restarts the units (with a
health check). It does **not** reverse migrations.

## Manual rollback

```bash
ls -1 /opt/ims/ims-go-api/releases            # newest last (sorted by timestamp)
sudo ln -sfn /opt/ims/ims-go-api/releases/<PREVIOUS> /opt/ims/ims-go-api/current.new
sudo mv -T /opt/ims/ims-go-api/current.new /opt/ims/ims-go-api/current
sudo systemctl restart ims-go-api ims-go-api-scheduler ims-go-api-worker
curl -fsS http://127.0.0.1:8080/up
```

## Migration caveat (expand/contract)

Rollbacks are safe **only if the previous release is compatible with the current schema**. Follow
the expand/contract policy (`migrations.md`):

- Additive ("expand") changes ship with the release and are backwards compatible.
- Destructive ("contract") changes — dropping columns/tables — ship **one release later**, after
  no running release reads them.

If a migration must be reverted, do it deliberately, not as part of a code rollback:

```bash
ims migrate:status
ims migrate:rollback        # reverts the most recent migration (goose Down)
```

Back up the database before any rollback that touches schema.

## Choosing the target

Prefer the previous release; if several are suspect, roll back further, then forward again once a
fix is deployed:

```bash
sudo deploy/scripts/release.sh --rollback          # previous
# then, after the fix:
sudo deploy/scripts/release.sh --source /srv/src/ims-go-api-starter
```
