-- name: InsertActivity :exec
INSERT INTO activity_log (tenant_id, actor_id, impersonator_id, impersonator_name, action, subject_type,
                          subject_id, changes, ip, request_id)
VALUES (sqlc.narg('tenant_id'), sqlc.narg('actor_id'), sqlc.narg('impersonator_id'), sqlc.narg('impersonator_name'),
        @action, @subject_type, sqlc.narg('subject_id'), @changes, sqlc.narg('ip'), sqlc.narg('request_id'));

-- name: ListActivity :many
-- Newest first, keyset-paginated on (created_at, id). The service picks the
-- tenant predicate: system_only = the system audit log (tenant_id IS NULL);
-- otherwise all_tenants, or exactly tenant_id. all_tenants and system_only
-- are only set for activity.view.all readers.
SELECT a.id, a.tenant_id, a.actor_id, a.impersonator_id, a.impersonator_name, a.action, a.subject_type,
       a.subject_id, a.changes, a.ip, a.request_id, a.created_at, u.name AS actor_name
FROM activity_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE CASE
        WHEN @system_only::boolean THEN a.tenant_id IS NULL
        ELSE (@all_tenants::boolean OR a.tenant_id = sqlc.narg('tenant_id')::bigint)
      END
  AND (sqlc.narg('subject_type')::text IS NULL OR a.subject_type = sqlc.narg('subject_type'))
  AND (sqlc.narg('subject_id')::bigint IS NULL OR a.subject_id = sqlc.narg('subject_id'))
  AND (sqlc.narg('actor_id')::bigint IS NULL OR a.actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('impersonator_id')::bigint IS NULL OR a.impersonator_id = sqlc.narg('impersonator_id'))
  AND (NOT @impersonated_only::boolean OR a.impersonator_id IS NOT NULL OR a.action LIKE 'impersonation.%')
  AND (sqlc.narg('action')::text IS NULL OR a.action = sqlc.narg('action'))
  AND (sqlc.narg('from_at')::timestamptz IS NULL OR a.created_at >= sqlc.narg('from_at'))
  AND (sqlc.narg('to_at')::timestamptz IS NULL OR a.created_at <= sqlc.narg('to_at'))
  AND (sqlc.narg('search')::text IS NULL
       OR a.action ILIKE '%' || sqlc.narg('search') || '%'
       OR a.subject_type ILIKE '%' || sqlc.narg('search') || '%'
       OR a.ip ILIKE '%' || sqlc.narg('search') || '%'
       OR u.name ILIKE '%' || sqlc.narg('search') || '%'
       OR a.changes::text ILIKE '%' || sqlc.narg('search') || '%')
  AND (sqlc.narg('before_at')::timestamptz IS NULL
       OR (a.created_at, a.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::bigint))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @row_limit;

-- name: CountActivity :one
-- The rows ListActivity would page through (same predicates, no cursor).
SELECT count(*) FROM activity_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE CASE
        WHEN @system_only::boolean THEN a.tenant_id IS NULL
        ELSE (@all_tenants::boolean OR a.tenant_id = sqlc.narg('tenant_id')::bigint)
      END
  AND (sqlc.narg('subject_type')::text IS NULL OR a.subject_type = sqlc.narg('subject_type'))
  AND (sqlc.narg('subject_id')::bigint IS NULL OR a.subject_id = sqlc.narg('subject_id'))
  AND (sqlc.narg('actor_id')::bigint IS NULL OR a.actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('impersonator_id')::bigint IS NULL OR a.impersonator_id = sqlc.narg('impersonator_id'))
  AND (NOT @impersonated_only::boolean OR a.impersonator_id IS NOT NULL OR a.action LIKE 'impersonation.%')
  AND (sqlc.narg('action')::text IS NULL OR a.action = sqlc.narg('action'))
  AND (sqlc.narg('from_at')::timestamptz IS NULL OR a.created_at >= sqlc.narg('from_at'))
  AND (sqlc.narg('to_at')::timestamptz IS NULL OR a.created_at <= sqlc.narg('to_at'))
  AND (sqlc.narg('search')::text IS NULL
       OR a.action ILIKE '%' || sqlc.narg('search') || '%'
       OR a.subject_type ILIKE '%' || sqlc.narg('search') || '%'
       OR a.ip ILIKE '%' || sqlc.narg('search') || '%'
       OR u.name ILIKE '%' || sqlc.narg('search') || '%'
       OR a.changes::text ILIKE '%' || sqlc.narg('search') || '%');

-- name: ActivityActionFacets :many
-- The actions in the reader's scope, most frequent first.
SELECT a.action, count(*) AS total FROM activity_log a
WHERE CASE
        WHEN @system_only::boolean THEN a.tenant_id IS NULL
        ELSE (@all_tenants::boolean OR a.tenant_id = sqlc.narg('tenant_id')::bigint)
      END
GROUP BY a.action
ORDER BY total DESC, a.action
LIMIT 200;

-- name: ActivityActorFacets :many
-- Who acted in the reader's scope, most active first.
SELECT a.actor_id::bigint AS actor_id, COALESCE(max(u.name), '')::text AS actor_name, count(*) AS total
FROM activity_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE CASE
        WHEN @system_only::boolean THEN a.tenant_id IS NULL
        ELSE (@all_tenants::boolean OR a.tenant_id = sqlc.narg('tenant_id')::bigint)
      END
  AND a.actor_id IS NOT NULL
GROUP BY a.actor_id
ORDER BY total DESC
LIMIT 200;

-- name: ActivitySubjectFacets :many
SELECT a.subject_type, count(*) AS total FROM activity_log a
WHERE CASE
        WHEN @system_only::boolean THEN a.tenant_id IS NULL
        ELSE (@all_tenants::boolean OR a.tenant_id = sqlc.narg('tenant_id')::bigint)
      END
GROUP BY a.subject_type
ORDER BY total DESC, a.subject_type;

-- name: ActivityLastDay :one
SELECT count(*) FROM activity_log a
WHERE CASE
        WHEN @system_only::boolean THEN a.tenant_id IS NULL
        ELSE (@all_tenants::boolean OR a.tenant_id = sqlc.narg('tenant_id')::bigint)
      END
  AND a.created_at > now() - interval '24 hours';

-- name: PurgeActivity :execrows
DELETE FROM activity_log WHERE created_at < now() - make_interval(days => @days::int);

-- name: RecentLoginFrom :one
-- For the new sign-in alert: whether the user signed in successfully in the
-- last 90 days at all, and from this IP.
SELECT
    count(*) > 0 AS any_login,
    count(*) FILTER (WHERE l.ip = @ip::text) > 0 AS from_ip
FROM activity_log l
WHERE l.actor_id = @user_id::bigint
  AND l.action = 'auth.login'
  AND l.changes->>'status' = 'ok'
  AND l.created_at > now() - interval '90 days';

-- name: ListActivityByActorForPrivacy :many
SELECT id, action, subject_type, subject_id, changes, created_at FROM activity_log
WHERE actor_id = @actor_id
ORDER BY id;

-- name: ListActivityAboutUserForPrivacy :many
SELECT id, action, actor_id, changes, created_at FROM activity_log
WHERE subject_type = 'user' AND subject_id = @user_id
ORDER BY id;

-- name: ScrubActivityActorIP :execrows
-- The rows the user performed stay (the tenant's audit trail); the address
-- they acted from goes.
UPDATE activity_log SET ip = NULL
WHERE actor_id = @actor_id AND ip IS NOT NULL;

-- name: ScrubActivityAboutUser :execrows
-- Rows about the user (created, updated, their logins, act-as on them) keep
-- the action and drop the copied values.
UPDATE activity_log SET changes = '{"erased": true}'::jsonb
WHERE subject_type = 'user' AND subject_id = @user_id;

-- name: ScrubFailedLoginsByEmail :execrows
-- Failed logins are system rows with no subject; they carry the typed e-mail.
UPDATE activity_log SET changes = '{"erased": true}'::jsonb, ip = NULL
WHERE action = 'auth.login' AND lower(changes->>'email') = lower(@email::text);

-- name: ScrubActivityImpersonatorName :execrows
UPDATE activity_log SET impersonator_name = 'Deleted user'
WHERE impersonator_id = @impersonator_id;
