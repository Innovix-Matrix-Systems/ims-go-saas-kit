-- Dashboard aggregates. Daily counts are bucketed in the viewer's time zone
-- (@tz, an IANA name checked by the service) and cover [@since, now); the
-- service splits them into the current and the previous period.

-- name: DashboardTenantMembers :one
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE is_active = 1 AND (invited_at IS NULL OR invitation_accepted_at IS NOT NULL))::bigint AS active,
    count(*) FILTER (WHERE invited_at IS NOT NULL AND invitation_accepted_at IS NULL)::bigint AS pending_invitations,
    count(*) FILTER (WHERE last_login_at >= @active_since::timestamptz)::bigint AS signed_in_recently
FROM users
WHERE tenant_id = @tenant_id::bigint AND deleted_at IS NULL;

-- name: DashboardTenantMembersByDay :many
SELECT to_char(created_at AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day, count(*)::bigint AS count
FROM users
WHERE tenant_id = @tenant_id::bigint AND deleted_at IS NULL AND created_at >= @since::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: DashboardTenantActivityByDay :many
SELECT to_char(created_at AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day, count(*)::bigint AS count
FROM activity_log
WHERE tenant_id = @tenant_id::bigint AND created_at >= @since::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: DashboardTenantTopActions :many
SELECT action, count(*)::bigint AS count
FROM activity_log
WHERE tenant_id = @tenant_id::bigint AND created_at >= @since::timestamptz
GROUP BY action
ORDER BY count(*) DESC, action
LIMIT @top::int;

-- name: DashboardTenantTopActors :many
SELECT a.actor_id::bigint AS actor_id, u.name AS name, count(*)::bigint AS count
FROM activity_log a
JOIN users u ON u.id = a.actor_id
WHERE a.tenant_id = @tenant_id::bigint AND a.created_at >= @since::timestamptz AND a.actor_id IS NOT NULL
GROUP BY a.actor_id, u.name
ORDER BY count(*) DESC, u.name
LIMIT @top::int;

-- name: DashboardTenantFiles :one
SELECT count(*)::bigint AS files, COALESCE(sum(size), 0)::bigint AS bytes
FROM files
WHERE tenant_id = @tenant_id::bigint;

-- name: DashboardTenantFilesByType :many
SELECT type, count(*)::bigint AS files, COALESCE(sum(size), 0)::bigint AS bytes
FROM files
WHERE tenant_id = @tenant_id::bigint
GROUP BY type
ORDER BY sum(size) DESC NULLS LAST, type;

-- name: DashboardTenantBranches :one
SELECT count(*)::bigint FROM branches WHERE tenant_id = @tenant_id::bigint AND deleted_at IS NULL;

-- name: DashboardTenantRoles :one
SELECT count(*)::bigint FROM roles WHERE tenant_id = @tenant_id::bigint;

-- name: DashboardPlatformTenants :one
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE is_active)::bigint AS active
FROM tenants
WHERE deleted_at IS NULL;

-- name: DashboardPlatformTenantsByDay :many
SELECT to_char(created_at AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day, count(*)::bigint AS count
FROM tenants
WHERE deleted_at IS NULL AND created_at >= @since::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: DashboardPlatformUsers :one
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE last_login_at >= @active_since::timestamptz)::bigint AS signed_in_recently
FROM users
WHERE tenant_id IS NOT NULL AND deleted_at IS NULL;

-- name: DashboardPlatformUsersByDay :many
SELECT to_char(created_at AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day, count(*)::bigint AS count
FROM users
WHERE tenant_id IS NOT NULL AND deleted_at IS NULL AND created_at >= @since::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: DashboardPlatformActivityByDay :many
SELECT to_char(created_at AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day, count(*)::bigint AS count
FROM activity_log
WHERE created_at >= @since::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: DashboardPlatformRevenueByDay :many
-- Completed payments by the day they were paid, per currency.
SELECT to_char(COALESCE(paid_at, created_at) AT TIME ZONE @tz::text, 'YYYY-MM-DD')::text AS day,
       currency, COALESCE(sum(amount), 0)::bigint AS amount, count(*)::bigint AS count
FROM payments
WHERE status = 'completed' AND COALESCE(paid_at, created_at) >= @since::timestamptz
GROUP BY 1, currency
ORDER BY 1, currency;

-- name: DashboardPlatformPendingPayments :many
SELECT currency, count(*)::bigint AS count, COALESCE(sum(amount), 0)::bigint AS amount
FROM payments
WHERE status = 'pending'
GROUP BY currency
ORDER BY count(*) DESC, currency;

-- name: DashboardPlatformPlans :many
-- Organizations on each plan (their current subscription).
SELECT p.id AS package_id, p.name, count(DISTINCT s.tenant_id)::bigint AS organizations
FROM subscriptions s
JOIN packages p ON p.id = s.package_id
JOIN tenants t ON t.id = s.tenant_id AND t.deleted_at IS NULL
WHERE s.status IN ('trial', 'active', 'past_due')
GROUP BY p.id, p.name
ORDER BY count(DISTINCT s.tenant_id) DESC, p.name;

-- name: DashboardPlatformRecentTenants :many
SELECT t.id, t.name, t.code, t.is_active, t.created_at,
       (SELECT count(*) FROM users u WHERE u.tenant_id = t.id AND u.deleted_at IS NULL)::bigint AS members
FROM tenants t
WHERE t.deleted_at IS NULL
ORDER BY t.created_at DESC, t.id DESC
LIMIT @top::int;
