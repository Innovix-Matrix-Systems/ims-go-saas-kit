---
title: Backups
---

The app creates PostgreSQL archives with `pg_dump` and stores them **locally first**; an optional
S3-compatible destination (AWS S3, Cloudflare R2, MinIO, B2 S3) receives the same archive, which
contains the database dump, so the remote copy is a full database backup.

## Prerequisites

- `pg_dump` must be installed on the host **and match (or exceed) the server major version**.
  `pg_dump` from a newer major version can dump an older server; an older `pg_dump` cannot dump a
  newer server. Point `BACKUP_PG_DUMP_PATH` at the binary when it is not on `PATH`.
- The `BACKUP_DIR` directory is created with `0700`; the process user must be able to write it.
- Remote storage is optional. To use a remote destination, configure the shared S3 settings.

## Configuration

```dotenv
BACKUP_ENABLED=true                 # schedules + dashboard creation
BACKUP_DIR=backups                  # relative to APP_STORAGE_DIR
BACKUP_DESTINATIONS=local,s3        # local is always included
BACKUP_S3_PREFIX=backups            # object key: backups/<env>/<filename>
BACKUP_PG_DUMP_PATH=pg_dump
BACKUP_TIMEOUT=1h
BACKUP_RUN_CRON=30 1 * * *
BACKUP_CLEAN_CRON=30 2 * * *
```

Retention follows the Laravel-backup DefaultStrategy: keep every archive for 7 days, then one per
day for 16 days, one per week for 8 weeks, one per month for 4 months and one per year for 2 years.
The newest archive is never deleted. `BACKUP_MAX_TOTAL_MB` (0 = unlimited) caps total storage.

`BACKUP_INCLUDE_FILES=true` adds the configured `BACKUP_FILES_PATHS` under `files/` in the archive
(database-only by default). `node_modules`, `.git`, `tmp` and the backup directory itself are
skipped.

### S3 / R2 / MinIO

The backup destination reuses the storage S3 client:

```dotenv
S3_ENDPOINT=https://<account>.r2.cloudflarestorage.com   # omit for AWS S3
S3_REGION=auto                                           # R2 uses "auto"
S3_BUCKET=my-backups
S3_ACCESS_KEY=...
S3_SECRET_KEY=...
S3_PATH_STYLE=true                                       # MinIO and most S3-compatible stores
BACKUP_DESTINATIONS=local,s3
```

Each destination is attempted independently. If local succeeds and the remote copy fails, the run
is marked `partial` and the remote error is stored in the run's `destinations` JSON.

## Commands

```bash
go run ./cmd/app backup:run                     # synchronous manual backup
go run ./cmd/app backup:run --with-files
go run ./cmd/app backup:run --destinations=local,s3 --filename=before-upgrade.tar.gz
go run ./cmd/app backup:list --limit=20
go run ./cmd/app backup:clean --dry-run
go run ./cmd/app backup:clean
go run ./cmd/app backup:monitor                 # exits 1 when stale/failed
```

The CLI works even when `BACKUP_ENABLED=false`; scheduling and the dashboard creation button are
gated by the flag.

## Runtime

- `backup.run` and `backup.clean` are scheduler tasks when backups are enabled.
- The dashboard/API enqueues `backup.run` on the `heavy` queue; a Postgres advisory lock prevents
  overlapping runs. With `QUEUE_DRIVER=sync` the run executes inline.
- The `Backups` health check warns when the newest successful run is older than
  `BACKUP_MAX_AGE_HOURS` or storage exceeds `BACKUP_MAX_TOTAL_MB`, and fails when no successful run
  exists.
- Every run, delete and cleanup is audited in `dev_actions` and visible at `/dev/backups`.

## Restore (manual, out of scope in v1)

The archive is a normal gzip tarball with `manifest.json`, `database.dump` (PostgreSQL custom
format) and optional `files/`. Restore the dump manually:

```bash
tar -xzf <archive>.tar.gz database.dump
pg_restore --clean --if-exists --no-owner --no-privileges -d <database> database.dump
```

**Erased users come back with the dump.** A backup taken before someone's account was erased
still holds their personal data, and the restore also brings back the erasure ledger as it was.
Save the list of erasures from the live database *before* restoring, and erase those users again
after it ([Account deletion & export](../guides/privacy.md#after-a-backup-restore)):

```bash
ims privacy:ledger --since=<the backup's date> --out=erased.txt   # before the restore
# ... pg_restore ...
ims migrate
ims privacy:replay --from=erased.txt                             # after the restore
```

Restore automation, point-in-time recovery/WAL archiving, archive encryption and failure
notifications are not implemented in v1.
