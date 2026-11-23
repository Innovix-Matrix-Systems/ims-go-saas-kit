-- name: StartScheduleRun :one
INSERT INTO schedule_runs (task, triggered_by, status, started_at)
VALUES (@task, @triggered_by, 'running', @started_at)
RETURNING id;

-- name: FinishScheduleRun :exec
UPDATE schedule_runs
SET status = @status, finished_at = @finished_at, duration_ms = @duration_ms, error = sqlc.narg('error')
WHERE id = @id;

-- name: ListScheduleRuns :many
-- History, newest first (keyset by id), optionally for one task or status.
SELECT * FROM schedule_runs
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
  AND (@task::text = '' OR task = @task::text)
  AND (@status::text = '' OR status = @status::text)
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListRecentScheduleRuns :many
-- The newest @per_task runs of each task in @tasks, newest first within a
-- task: one index range scan (task, id DESC) per task, however long the
-- history grows.
SELECT r.id, r.task, r.triggered_by, r.status, r.started_at, r.finished_at, r.duration_ms, r.error
FROM unnest(@tasks::text[]) AS t(task)
CROSS JOIN LATERAL (
    SELECT * FROM schedule_runs
    WHERE schedule_runs.task = t.task
    ORDER BY id DESC
    LIMIT @per_task::bigint
) r
ORDER BY r.task, r.id DESC;

-- name: InterruptScheduleRuns :execrows
-- Scheduled runs still marked running when the scheduler starts were lost
-- with the previous process (only one scheduler runs per database); a
-- manual run (the dashboard, the CLI) still open after @stale_before was
-- lost with the process that started it.
UPDATE schedule_runs
SET status = 'interrupted', finished_at = now()
WHERE status = 'running'
  AND (triggered_by = 'schedule' OR started_at < @stale_before::timestamptz);

-- name: PruneScheduleRuns :execrows
DELETE FROM schedule_runs WHERE started_at < @before::timestamptz;
