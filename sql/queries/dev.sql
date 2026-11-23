-- name: InsertDevAction :one
INSERT INTO dev_actions (actor_user_id, action, target, params, status, error_message, duration_ms, ip, user_agent)
VALUES (@actor_user_id, @action, @target, @params, @status, @error_message, @duration_ms,
        sqlc.narg('ip'), sqlc.narg('user_agent'))
RETURNING *;

-- name: ListDevActions :many
SELECT * FROM dev_actions
WHERE (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListDevTenantTargets :many
SELECT
    t.id,
    t.name,
    t.code,
    (
        SELECT count(*)
        FROM feature_flags f
        WHERE f.target_type = 'tenant' AND f.target_id = t.id
    ) AS overrides
FROM tenants t
WHERE t.deleted_at IS NULL
  AND (
    @search::text = ''
    OR t.name ILIKE '%' || @search::text || '%'
    OR t.code ILIKE '%' || @search::text || '%'
  )
ORDER BY t.name ASC, t.id ASC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDevTenantTargets :one
SELECT count(*)
FROM tenants t
WHERE t.deleted_at IS NULL
  AND (
    @search::text = ''
    OR t.name ILIKE '%' || @search::text || '%'
    OR t.code ILIKE '%' || @search::text || '%'
  );

-- name: CountDevTenantTargetsWithOverrides :one
SELECT count(DISTINCT f.target_id)
FROM feature_flags f
JOIN tenants t ON t.id = f.target_id AND t.deleted_at IS NULL
WHERE f.target_type = 'tenant';

-- name: ListDevUserTargets :many
SELECT
    u.id,
    u.name,
    u.email,
    u.tenant_id,
    t.name AS tenant_name,
    (
        SELECT count(*)
        FROM feature_flags f
        WHERE f.target_type = 'user' AND f.target_id = u.id
    ) AS overrides
FROM users u
LEFT JOIN tenants t ON t.id = u.tenant_id AND t.deleted_at IS NULL
WHERE u.deleted_at IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  )
ORDER BY u.name ASC, u.id ASC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDevUserTargets :one
SELECT count(*)
FROM users u
WHERE u.deleted_at IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  );

-- name: CountDevUserTargetsWithOverrides :one
SELECT count(DISTINCT f.target_id)
FROM feature_flags f
JOIN users u ON u.id = f.target_id AND u.deleted_at IS NULL
WHERE f.target_type = 'user';

-- name: ListDevDevelopers :many
SELECT
    u.id,
    u.name,
    u.email,
    u.is_active,
    u.last_login_at,
    u.created_at,
    (
        SELECT count(*)
        FROM personal_access_tokens t
        WHERE t.tokenable_type = 'user'
          AND t.tokenable_id = u.id
          AND (t.expires_at IS NULL OR t.expires_at > now())
    ) AS active_tokens
FROM users u
JOIN model_has_roles mr ON mr.model_type = 'user' AND mr.model_id = u.id
JOIN roles r ON r.id = mr.role_id
WHERE u.deleted_at IS NULL
  AND r.name = @role_name
  AND r.tenant_id IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  )
ORDER BY u.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDevDevelopers :one
SELECT count(*)
FROM users u
JOIN model_has_roles mr ON mr.model_type = 'user' AND mr.model_id = u.id
JOIN roles r ON r.id = mr.role_id
WHERE u.deleted_at IS NULL
  AND r.name = @role_name
  AND r.tenant_id IS NULL
  AND (
    @search::text = ''
    OR u.name ILIKE '%' || @search::text || '%'
    OR u.email ILIKE '%' || @search::text || '%'
  );

-- name: DevInspectorRoles :many
-- Platform roles (no tenant) with their permission and user counts.
SELECT r.id, r.name, r.guard_name, r.is_system,
       (SELECT count(*) FROM role_has_permissions rp WHERE rp.role_id = r.id)::bigint AS permission_count,
       (SELECT count(*) FROM model_has_roles mr WHERE mr.role_id = r.id)::bigint AS user_count
FROM roles r
WHERE r.tenant_id IS NULL
ORDER BY r.id;

-- name: DevInspectorTenantRoleCount :one
SELECT count(*) FROM roles WHERE tenant_id IS NOT NULL;

-- name: DevInspectorPermissions :many
SELECT name, "group", description, is_system FROM permissions ORDER BY "group", name;

-- name: FinishDevAction :exec
UPDATE dev_actions
SET status = @status, error_message = @error_message, duration_ms = @duration_ms, params = @params
WHERE id = @id;

-- name: ListDevCommandRuns :many
-- Command runs (dev_actions rows with action 'command.run'), newest first.
SELECT * FROM dev_actions
WHERE action = 'command.run'
  AND (@target::text = '' OR target = @target::text)
  AND (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @row_limit;

-- name: DevCommandRunSummary :many
-- Last run and run count per command.
SELECT DISTINCT ON (target) target, status, created_at,
       count(*) OVER (PARTITION BY target)::bigint AS runs
FROM dev_actions
WHERE action = 'command.run' AND target IS NOT NULL
ORDER BY target, id DESC;

-- name: GetDevAction :one
SELECT * FROM dev_actions WHERE id = @id;

-- name: DevListTenants :many
-- Tenants with their current subscription (an active/trial one when present,
-- otherwise the latest) and usage counts.
SELECT t.id, t.name, t.code, t.domain, t.email, t.phone, t.is_active, t.created_at,
       coalesce(s.id, 0)::bigint AS subscription_id, coalesce(s.status, '')::text AS subscription_status,
       s.expires_at, s.trial_ends_at,
       p.name AS package_name,
       (SELECT count(*) FROM users u WHERE u.tenant_id = t.id AND u.deleted_at IS NULL)::bigint AS user_count,
       (SELECT count(*) FROM branches b WHERE b.tenant_id = t.id AND b.deleted_at IS NULL)::bigint AS branch_count,
       ((SELECT count(*) FROM settings st WHERE st.target_type = 'tenant' AND st.target_id = t.id)
        + (SELECT count(*) FROM feature_flags ff WHERE ff.target_type = 'tenant' AND ff.target_id = t.id))::bigint AS override_count
FROM tenants t
LEFT JOIN LATERAL (
    SELECT * FROM subscriptions sub
    WHERE sub.tenant_id = t.id
    ORDER BY (sub.status IN ('active', 'trial')) DESC, sub.id DESC
    LIMIT 1
) s ON true
LEFT JOIN packages p ON p.id = s.package_id
WHERE t.deleted_at IS NULL
  AND (@search::text = '' OR t.name ILIKE '%' || @search::text || '%' OR t.code ILIKE '%' || @search::text || '%'
       OR coalesce(t.email, '') ILIKE '%' || @search::text || '%' OR coalesce(t.phone, '') ILIKE '%' || @search::text || '%'
       OR coalesce(t.domain, '') ILIKE '%' || @search::text || '%')
  AND (@active::text = '' OR (@active::text = 'active' AND t.is_active) OR (@active::text = 'inactive' AND NOT t.is_active))
  AND (@subscription::text = ''
       OR (@subscription::text = 'none' AND s.id IS NULL)
       OR s.status = @subscription::text)
  AND (@tenant_id::bigint = 0 OR t.id = @tenant_id::bigint)
ORDER BY t.id
LIMIT @page_limit OFFSET @page_offset;

-- name: DevCountTenants :one
SELECT count(*)
FROM tenants t
LEFT JOIN LATERAL (
    SELECT * FROM subscriptions sub
    WHERE sub.tenant_id = t.id
    ORDER BY (sub.status IN ('active', 'trial')) DESC, sub.id DESC
    LIMIT 1
) s ON true
WHERE t.deleted_at IS NULL
  AND (@search::text = '' OR t.name ILIKE '%' || @search::text || '%' OR t.code ILIKE '%' || @search::text || '%'
       OR coalesce(t.email, '') ILIKE '%' || @search::text || '%' OR coalesce(t.phone, '') ILIKE '%' || @search::text || '%'
       OR coalesce(t.domain, '') ILIKE '%' || @search::text || '%')
  AND (@active::text = '' OR (@active::text = 'active' AND t.is_active) OR (@active::text = 'inactive' AND NOT t.is_active))
  AND (@subscription::text = ''
       OR (@subscription::text = 'none' AND s.id IS NULL)
       OR s.status = @subscription::text);

-- name: DevTenantStorageBytes :one
SELECT coalesce(sum(size), 0)::bigint FROM files WHERE tenant_id = @tenant_id;

-- name: DevTenantSubscriptions :many
SELECT s.id, s.status, s.starts_at, s.trial_ends_at, s.expires_at, s.cancelled_at, s.overrides, s.created_at,
       p.id AS package_id, p.name AS package_name
FROM subscriptions s
JOIN packages p ON p.id = s.package_id
WHERE s.tenant_id = @tenant_id
ORDER BY s.id DESC
LIMIT 20;

-- name: DevPackageFeatures :many
SELECT feature_key, is_enabled, limit_value FROM package_features WHERE package_id = @package_id;

-- name: SearchDevActions :many
-- The audit log, newest first, with the actor's e-mail.
SELECT a.*, u.email AS actor_email, u.name AS actor_name
FROM dev_actions a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE (@action::text = '' OR a.action = @action::text)
  AND (@actor_id::bigint = 0 OR a.actor_user_id = @actor_id::bigint)
  AND (@status::text = '' OR a.status = @status::text)
  AND (sqlc.narg('from_at')::timestamptz IS NULL OR a.created_at >= sqlc.narg('from_at')::timestamptz)
  AND (sqlc.narg('to_at')::timestamptz IS NULL OR a.created_at < sqlc.narg('to_at')::timestamptz)
  AND (@search::text = ''
       OR coalesce(u.email, '') ILIKE '%' || @search::text || '%'
       OR coalesce(a.target, '') ILIKE '%' || @search::text || '%'
       OR coalesce(a.ip, '') ILIKE @search::text || '%'
       OR a.params::text ILIKE '%' || @search::text || '%')
  AND (@before_id::bigint = 0 OR a.id < @before_id::bigint)
ORDER BY a.id DESC
LIMIT @row_limit;

-- name: CountDevActionsFiltered :one
SELECT count(*)
FROM dev_actions a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE (@action::text = '' OR a.action = @action::text)
  AND (@actor_id::bigint = 0 OR a.actor_user_id = @actor_id::bigint)
  AND (@status::text = '' OR a.status = @status::text)
  AND (sqlc.narg('from_at')::timestamptz IS NULL OR a.created_at >= sqlc.narg('from_at')::timestamptz)
  AND (sqlc.narg('to_at')::timestamptz IS NULL OR a.created_at < sqlc.narg('to_at')::timestamptz)
  AND (@search::text = ''
       OR coalesce(u.email, '') ILIKE '%' || @search::text || '%'
       OR coalesce(a.target, '') ILIKE '%' || @search::text || '%'
       OR coalesce(a.ip, '') ILIKE @search::text || '%'
       OR a.params::text ILIKE '%' || @search::text || '%');

-- name: GetDevActionDetail :one
SELECT a.*, u.email AS actor_email, u.name AS actor_name
FROM dev_actions a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE a.id = @id;

-- name: DevActionKinds :many
SELECT action, count(*)::bigint AS total FROM dev_actions GROUP BY action ORDER BY action;

-- name: DevActionActors :many
SELECT a.actor_user_id, u.email, count(*)::bigint AS total
FROM dev_actions a
LEFT JOIN users u ON u.id = a.actor_user_id
GROUP BY a.actor_user_id, u.email
ORDER BY total DESC
LIMIT 100;

-- name: DevActionStats :one
SELECT
    (SELECT count(*) FROM dev_actions WHERE created_at > now() - interval '24 hours')::bigint AS last_day,
    (SELECT count(*) FROM dev_actions
     WHERE action = 'auth.login' AND status <> 'ok' AND created_at > now() - interval '24 hours')::bigint AS failed_sign_ins;

-- name: PruneDevActions :execrows
DELETE FROM dev_actions WHERE created_at < now() - make_interval(days => @retention_days::int);

-- name: ScrubDevActionsClient :execrows
-- Dashboard audit rows stay; the address and browser they came from go.
UPDATE dev_actions SET ip = NULL, user_agent = NULL
WHERE actor_user_id = @actor_user_id AND (ip IS NOT NULL OR user_agent IS NOT NULL);
