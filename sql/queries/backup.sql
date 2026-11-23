-- name: CreateBackupRun :one
INSERT INTO backup_runs (uuid, status, trigger_kind, actor_user_id, filename, scope, database_name)
VALUES (@uuid, 'pending', @trigger_kind, @actor_user_id, @filename, @scope, @database_name)
RETURNING *;

-- name: GetBackupRun :one
SELECT * FROM backup_runs
WHERE id = @id AND deleted_at IS NULL;

-- name: ListBackupRuns :many
SELECT * FROM backup_runs
WHERE deleted_at IS NULL
  AND (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListBackupRunsForCleanup :many
SELECT id, filename, created_at, size_bytes, destinations
FROM backup_runs
WHERE deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: SetBackupRunRunning :one
UPDATE backup_runs
SET status = 'running', started_at = now(), error = NULL
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: CompleteBackupRun :one
UPDATE backup_runs
SET status = @status::text,
    filename = @filename,
    size_bytes = @size_bytes,
    checksum_sha256 = @checksum_sha256,
    pg_dump_version = @pg_dump_version,
    database_name = @database_name,
    destinations = @destinations::jsonb,
    error = @error_message::text,
    finished_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: FailBackupRun :one
UPDATE backup_runs
SET status = 'failed',
    error = @error_message::text,
    destinations = @destinations::jsonb,
    finished_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteBackupRun :execrows
UPDATE backup_runs
SET status = 'deleted', deleted_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: LatestSuccessfulBackup :one
SELECT * FROM backup_runs
WHERE deleted_at IS NULL
  AND status IN ('completed', 'partial')
ORDER BY finished_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: BackupRunStats :one
SELECT
    count(*)::bigint AS total_runs,
    COALESCE(sum(size_bytes) FILTER (WHERE deleted_at IS NULL), 0)::bigint AS total_size,
    (max(finished_at) FILTER (WHERE status IN ('completed', 'partial')))::timestamptz AS last_success_at,
    count(*) FILTER (WHERE status IN ('completed', 'partial'))::bigint AS successful_runs
FROM backup_runs
WHERE deleted_at IS NULL;
