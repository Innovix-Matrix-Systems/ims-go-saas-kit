-- name: CreatePrivacyRequest :one
INSERT INTO privacy_requests (tenant_id, user_id, type, status, source, requested_by, scheduled_for)
VALUES (sqlc.narg('tenant_id'), @user_id, @type, @status, @source, sqlc.narg('requested_by'), sqlc.narg('scheduled_for'))
RETURNING *;

-- name: GetPrivacyRequest :one
SELECT * FROM privacy_requests WHERE id = @id;

-- name: GetOpenErasureForUser :one
SELECT * FROM privacy_requests
WHERE user_id = @user_id AND type = 'erasure' AND status IN ('scheduled', 'processing');

-- name: MarkPrivacyRequestProcessing :execrows
-- Claims an open request. 0 rows means it was cancelled (or finished) in
-- the meantime and must not run. A non-null source / requested_by records
-- who made it run now (an admin erasing a scheduled request immediately).
UPDATE privacy_requests
SET status = 'processing',
    error = NULL,
    source = coalesce(sqlc.narg('source')::text, source),
    requested_by = coalesce(sqlc.narg('requested_by')::bigint, requested_by),
    updated_at = now()
WHERE id = @id AND status IN ('scheduled', 'processing');

-- name: CompletePrivacyRequest :execrows
UPDATE privacy_requests SET status = 'completed', report = @report, error = NULL, completed_at = now(), updated_at = now()
WHERE id = @id;

-- name: FailPrivacyRequest :execrows
-- Only an open request fails; a cancelled or finished one keeps its status.
UPDATE privacy_requests SET status = 'failed', error = @error, updated_at = now()
WHERE id = @id AND status IN ('scheduled', 'processing');

-- name: CancelPrivacyRequest :execrows
-- Only a scheduled erasure can be cancelled; one already running finishes.
UPDATE privacy_requests SET status = 'cancelled', cancelled_at = now(), updated_at = now()
WHERE id = @id AND status = 'scheduled';

-- name: SetPrivacyRequestJob :execrows
UPDATE privacy_requests SET processing_job_id = @processing_job_id, updated_at = now()
WHERE id = @id;

-- name: ListDueErasures :many
-- Scheduled erasures whose grace period is over, and erasures left
-- "processing" longer than @stale_minutes (the process died mid-run; the
-- erasure rolled back, so running it again is safe).
SELECT id, user_id FROM privacy_requests
WHERE type = 'erasure'
  AND ((status = 'scheduled' AND scheduled_for <= now())
       OR (status = 'processing' AND updated_at < now() - make_interval(mins => @stale_minutes::int)))
ORDER BY scheduled_for, id
LIMIT @page_limit;

-- name: ListSoftDeletedUsersToErase :many
-- Users deleted for more than @days days and not yet erased (the optional
-- retention erasure). One whose retention erasure was refused before is
-- not tried again.
SELECT u.id FROM users u
WHERE u.deleted_at IS NOT NULL
  AND u.erased_at IS NULL
  AND u.deleted_at < now() - make_interval(days => @days::int)
  AND NOT EXISTS (
      SELECT 1 FROM privacy_requests r
      WHERE r.user_id = u.id AND r.type = 'erasure' AND r.source = 'retention' AND r.status = 'failed'
  )
ORDER BY u.deleted_at, u.id
LIMIT @page_limit;

-- name: CountRecentExports :one
-- Exports of the user started since @since that did not fail (the
-- one-per-day limit on self-service exports).
SELECT count(*)::bigint FROM privacy_requests
WHERE user_id = @user_id AND type = 'export' AND status <> 'failed' AND created_at >= @since;

-- name: ListCompletedErasuresSince :many
-- The replay input after a backup restore: user ids only, no personal data.
SELECT DISTINCT user_id FROM privacy_requests
WHERE type = 'erasure' AND status = 'completed' AND completed_at >= @since
ORDER BY user_id;

-- name: ListUserReferenceColumns :many
-- Every column that may point at a user: a foreign key to users, a bigint
-- named like one (user_id, actor_id, *_user_id, *_by) or a polymorphic id
-- (owner_id, target_id, tokenable_id, model_id, subject_id). Over-matching is
-- fine: a column that does not hold users is exempted with a reason.
SELECT (c.table_name || '.' || c.column_name)::text AS column_ref
FROM information_schema.columns c
WHERE c.table_schema = current_schema()
  AND c.data_type = 'bigint'
  AND c.table_name NOT LIKE 'river\_%'
  AND c.table_name <> 'goose_db_version'
  AND (c.column_name IN ('user_id', 'actor_id', 'impersonator_id', 'owner_id', 'target_id', 'tokenable_id', 'model_id', 'subject_id')
       OR c.column_name LIKE '%\_user\_id'
       OR c.column_name LIKE '%\_by')
UNION
SELECT (kcu.table_name || '.' || kcu.column_name)::text AS column_ref
FROM information_schema.referential_constraints rc
JOIN information_schema.key_column_usage kcu
  ON kcu.constraint_name = rc.constraint_name AND kcu.constraint_schema = rc.constraint_schema
JOIN information_schema.constraint_column_usage ccu
  ON ccu.constraint_name = rc.unique_constraint_name AND ccu.constraint_schema = rc.unique_constraint_schema
WHERE ccu.table_name = 'users' AND ccu.column_name = 'id' AND kcu.table_schema = current_schema()
ORDER BY 1;
