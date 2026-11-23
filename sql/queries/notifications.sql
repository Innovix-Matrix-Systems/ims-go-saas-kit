-- name: CreateNotification :one
INSERT INTO notifications (
    tenant_id, event_type, priority, title, body, data, group_key, created_by, expires_at, channels
)
VALUES (
    @tenant_id, @event_type, @priority, @title, @body, @data, @group_key, @created_by, @expires_at,
    @channels::text[]
)
RETURNING *;

-- name: GetNotification :one
SELECT * FROM notifications WHERE id = @id;

-- Scoped admin reads (fail-closed): all_tenants is only true for internal
-- scopes; a tenant-bound caller matches their tenant only. Global
-- notifications (tenant_id IS NULL) are invisible to tenant-bound callers.

-- name: GetNotificationInTenant :one
SELECT * FROM notifications
WHERE id = @id
  AND (@all_tenants::boolean OR tenant_id = @tenant_id);

-- name: UpdateNotificationInTenant :one
UPDATE notifications
SET event_type = @event_type,
    priority   = @priority,
    title      = @title,
    body       = @body,
    data       = @data,
    group_key  = @group_key,
    expires_at = @expires_at,
    updated_by = @updated_by,
    updated_at = now()
WHERE id = @id
  AND (@all_tenants::boolean OR tenant_id = @tenant_id)
RETURNING *;

-- name: DeleteNotificationInTenant :execrows
DELETE FROM notifications
WHERE id = @id
  AND (@all_tenants::boolean OR tenant_id = @tenant_id);

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND (@search::text IS NULL OR title ILIKE '%' || @search || '%')
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority')::text)
  AND (sqlc.narg('event_type')::text IS NULL OR event_type = sqlc.narg('event_type')::text)
ORDER BY
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN created_at END DESC,
  CASE WHEN @order_by::text = 'title' AND @order_dir::text = 'asc' THEN lower(title) END ASC,
  CASE WHEN @order_by::text = 'title' AND @order_dir::text = 'desc' THEN lower(title) END DESC,
  CASE WHEN @order_by::text = 'priority' AND @order_dir::text = 'asc' THEN priority END ASC,
  CASE WHEN @order_by::text = 'priority' AND @order_dir::text = 'desc' THEN priority END DESC,
  id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountNotifications :one
SELECT count(*) FROM notifications
WHERE (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND (@search::text IS NULL OR title ILIKE '%' || @search || '%')
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority')::text)
  AND (sqlc.narg('event_type')::text IS NULL OR event_type = sqlc.narg('event_type')::text);

-- name: CreateNotificationTarget :one
INSERT INTO notification_targets (tenant_id, notification_id, target_type, target_id)
VALUES (@tenant_id, @notification_id, @target_type, @target_id)
RETURNING *;

-- name: DeleteNotificationTargets :execrows
DELETE FROM notification_targets WHERE notification_id = @notification_id;

-- name: ListNotificationTargets :many
SELECT * FROM notification_targets WHERE notification_id = @notification_id ORDER BY id;

-- name: ListUserIDsByIDs :many
SELECT id FROM users WHERE deleted_at IS NULL AND id = ANY(@ids::bigint[]);

-- name: ListUserIDsByBranch :many
SELECT id FROM users
WHERE deleted_at IS NULL
  AND branch_id = @branch_id
  AND (@all_tenants::boolean OR tenant_id = @tenant_id);

-- name: ListUserIDsByTenant :many
SELECT id FROM users WHERE deleted_at IS NULL AND tenant_id = @tenant_id;

-- name: ListGlobalUserIDs :many
SELECT id FROM users WHERE deleted_at IS NULL AND tenant_id IS NULL;

-- name: ListUserIDsByRoleIDs :many
SELECT DISTINCT u.id
FROM users u
JOIN model_has_roles mhr ON mhr.model_type = 'user' AND mhr.model_id = u.id
WHERE u.deleted_at IS NULL
  AND mhr.role_id = ANY(@role_ids::bigint[])
  AND (@all_tenants::boolean OR u.tenant_id = @tenant_id);

-- name: InsertNotificationDeliveries :many
INSERT INTO notification_deliveries (tenant_id, notification_id, user_id, status)
SELECT @tenant_id, @notification_id, unnest(@user_ids::bigint[]), 'pending'
ON CONFLICT (notification_id, user_id) DO NOTHING
RETURNING user_id;

-- name: ListUnreadDeliveryUserIDs :many
SELECT d.user_id
FROM notification_deliveries d
WHERE d.notification_id = @notification_id
  AND NOT EXISTS (
      SELECT 1 FROM notification_reads r
      WHERE r.notification_id = d.notification_id AND r.user_id = d.user_id
  );

-- name: InsertNotificationRead :execrows
INSERT INTO notification_reads (tenant_id, branch_id, notification_id, user_id, read_at)
VALUES (@tenant_id, @branch_id, @notification_id, @user_id, @read_at)
ON CONFLICT (notification_id, user_id) DO NOTHING;

-- name: NotificationVisibleToUser :one
SELECT EXISTS (
    SELECT 1
    FROM notifications n
    WHERE n.id = @notification_id
      AND (@all_tenants::boolean OR n.tenant_id = @tenant_id)
      AND EXISTS (
          SELECT 1
          FROM notification_targets t
          WHERE t.notification_id = n.id
            AND (
                (t.target_type = 'USER' AND t.target_id = @user_id::bigint)
                OR (t.target_type = 'ROLE' AND t.target_id = ANY(@role_ids::bigint[]))
                OR (t.target_type = 'BRANCH' AND @branch_id::bigint > 0 AND t.target_id = @branch_id::bigint)
                OR (t.target_type = 'TENANT' AND t.target_id IS NULL
                    -- An organization-wide notice reaches that organization's members only,
                    -- never a staff account reading across organizations.
                    AND NOT @is_non_tenant::boolean AND n.tenant_id = @tenant_id)
                OR (t.target_type = 'GLOBAL' AND t.target_id IS NULL AND @is_non_tenant::boolean)
            )
      )
) AS visible;

-- name: MarkAllNotificationsRead :execrows
INSERT INTO notification_reads (tenant_id, branch_id, notification_id, user_id, read_at)
SELECT @tenant_id, @branch_id, n.id, @user_id, @read_at
FROM notifications n
WHERE (n.expires_at IS NULL OR n.expires_at > @read_at)
  AND (@all_tenants::boolean OR n.tenant_id = @tenant_id)
  AND EXISTS (
      SELECT 1
      FROM notification_targets t
      WHERE t.notification_id = n.id
        AND (
            (t.target_type = 'USER' AND t.target_id = @user_id::bigint)
            OR (t.target_type = 'ROLE' AND t.target_id = ANY(@role_ids::bigint[]))
            OR (t.target_type = 'BRANCH' AND @branch_id::bigint > 0 AND t.target_id = @branch_id::bigint)
            OR (t.target_type = 'TENANT' AND t.target_id IS NULL
                -- An organization-wide notice reaches that organization's members only,
                -- never a staff account reading across organizations.
                AND NOT @is_non_tenant::boolean AND n.tenant_id = @tenant_id)
            OR (t.target_type = 'GLOBAL' AND t.target_id IS NULL AND @is_non_tenant::boolean)
        )
  )
ON CONFLICT (notification_id, user_id) DO NOTHING;

-- name: ListNotificationFeed :many
SELECT n.id, n.event_type, n.priority, n.title, n.body, n.data, n.group_key, n.created_at,
       (r.id IS NOT NULL)::boolean AS is_read
FROM notifications n
LEFT JOIN notification_reads r ON r.notification_id = n.id AND r.user_id = @user_id
WHERE (@all_tenants::boolean OR n.tenant_id = @tenant_id)
  AND (n.expires_at IS NULL OR n.expires_at > now())
  AND (@unread_only::boolean = false OR r.id IS NULL)
  AND (sqlc.narg('priority')::text IS NULL OR n.priority = sqlc.narg('priority')::text)
  AND (@read_all_before::bigint = 0 OR n.created_at > to_timestamp(@read_all_before::bigint))
  AND EXISTS (
      SELECT 1
      FROM notification_targets t
      WHERE t.notification_id = n.id
        AND (
            (t.target_type = 'USER' AND t.target_id = @user_id::bigint)
            OR (t.target_type = 'ROLE' AND t.target_id = ANY(@role_ids::bigint[]))
            OR (t.target_type = 'BRANCH' AND @branch_id::bigint > 0 AND t.target_id = @branch_id::bigint)
            OR (t.target_type = 'TENANT' AND t.target_id IS NULL
                -- An organization-wide notice reaches that organization's members only,
                -- never a staff account reading across organizations.
                AND NOT @is_non_tenant::boolean AND n.tenant_id = @tenant_id)
            OR (t.target_type = 'GLOBAL' AND t.target_id IS NULL AND @is_non_tenant::boolean)
        )
  )
  AND (
      sqlc.narg('cursor_created_at')::timestamptz IS NULL
      OR (n.created_at, n.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint)
  )
ORDER BY n.created_at DESC, n.id DESC
LIMIT @page_limit;

-- name: ListNotificationFeedBackward :many
SELECT n.id, n.event_type, n.priority, n.title, n.body, n.data, n.group_key, n.created_at,
       (r.id IS NOT NULL)::boolean AS is_read
FROM notifications n
LEFT JOIN notification_reads r ON r.notification_id = n.id AND r.user_id = @user_id
WHERE (@all_tenants::boolean OR n.tenant_id = @tenant_id)
  AND (n.expires_at IS NULL OR n.expires_at > now())
  AND (@unread_only::boolean = false OR r.id IS NULL)
  AND (sqlc.narg('priority')::text IS NULL OR n.priority = sqlc.narg('priority')::text)
  AND (@read_all_before::bigint = 0 OR n.created_at > to_timestamp(@read_all_before::bigint))
  AND EXISTS (
      SELECT 1
      FROM notification_targets t
      WHERE t.notification_id = n.id
        AND (
            (t.target_type = 'USER' AND t.target_id = @user_id::bigint)
            OR (t.target_type = 'ROLE' AND t.target_id = ANY(@role_ids::bigint[]))
            OR (t.target_type = 'BRANCH' AND @branch_id::bigint > 0 AND t.target_id = @branch_id::bigint)
            OR (t.target_type = 'TENANT' AND t.target_id IS NULL
                -- An organization-wide notice reaches that organization's members only,
                -- never a staff account reading across organizations.
                AND NOT @is_non_tenant::boolean AND n.tenant_id = @tenant_id)
            OR (t.target_type = 'GLOBAL' AND t.target_id IS NULL AND @is_non_tenant::boolean)
        )
  )
  AND (n.created_at, n.id) > (@cursor_created_at::timestamptz, @cursor_id::bigint)
ORDER BY n.created_at ASC, n.id ASC
LIMIT @page_limit;

-- name: CountUnreadNotificationsForUser :one
SELECT count(*)
FROM notification_deliveries d
JOIN notifications n ON n.id = d.notification_id
WHERE d.user_id = @user_id
  AND (@all_tenants::boolean OR n.tenant_id = @tenant_id)
  AND (n.expires_at IS NULL OR n.expires_at > now())
  AND NOT EXISTS (
      SELECT 1 FROM notification_reads r
      WHERE r.notification_id = d.notification_id AND r.user_id = d.user_id
  );

-- name: GetUserNotificationScope :one
SELECT COALESCE(branch_id, 0)::bigint AS branch_id, COALESCE(tenant_id, 0)::bigint AS tenant_id
FROM users
WHERE id = @user_id AND deleted_at IS NULL;

-- name: ListUserRoleIDs :many
SELECT role_id FROM model_has_roles
WHERE model_type = 'user' AND model_id = @user_id
ORDER BY role_id;

-- name: UserExistsInTenant :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE id = @id AND deleted_at IS NULL
      AND (@tenant_id::bigint = 0 OR tenant_id = @tenant_id::bigint)
);

-- name: BranchExistsInTenant :one
SELECT EXISTS (
    SELECT 1 FROM branches
    WHERE id = @id AND deleted_at IS NULL
      AND (@tenant_id::bigint = 0 OR tenant_id = @tenant_id::bigint)
);

-- name: PurgeExpiredNotifications :execrows
-- Retention: notifications expired for more than a week are invisible in
-- every feed query already; their targets/reads/deliveries cascade.
DELETE FROM notifications
WHERE expires_at IS NOT NULL AND expires_at < now() - interval '7 days';

-- name: ListNotificationRecipients :many
-- Who a notification is delivered to outside the feed: addresses only when
-- verified (an unverified address may not be theirs).
SELECT id, tenant_id, name,
       CASE WHEN email_verified_at IS NOT NULL THEN email ELSE '' END::text AS email,
       CASE WHEN phone_verified_at IS NOT NULL THEN coalesce(phone, '') ELSE '' END::text AS phone
FROM users
WHERE id = ANY(@ids::bigint[]) AND deleted_at IS NULL AND is_active = 1;

-- name: ListDisabledNotificationChannels :many
-- The users (of @ids) who turned a channel off for an event type.
SELECT user_id, channel FROM notification_preferences
WHERE event_type = @event_type AND user_id = ANY(@ids::bigint[]) AND NOT enabled;

-- name: ListNotificationPreferences :many
SELECT * FROM notification_preferences WHERE user_id = @user_id;

-- name: UpsertNotificationPreference :exec
INSERT INTO notification_preferences (user_id, event_type, channel, enabled)
VALUES (@user_id, @event_type, @channel, @enabled)
ON CONFLICT (user_id, event_type, channel)
DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now();

-- name: ListNotificationsForPrivacy :many
-- Notifications the user received (fanned out to them, or addressed to them
-- by id), with their read state.
SELECT n.id, n.event_type, n.title, n.body, n.created_at, nr.read_at
FROM notifications n
LEFT JOIN notification_reads nr ON nr.notification_id = n.id AND nr.user_id = @user_id::bigint
WHERE EXISTS (SELECT 1 FROM notification_deliveries nd WHERE nd.notification_id = n.id AND nd.user_id = @user_id::bigint)
   OR EXISTS (SELECT 1 FROM notification_targets nt WHERE nt.notification_id = n.id AND nt.target_type = 'USER' AND nt.target_id = @user_id::bigint)
ORDER BY n.id;

-- name: DeleteNotificationDeliveriesForUser :execrows
DELETE FROM notification_deliveries WHERE user_id = @user_id;

-- name: DeleteNotificationReadsForUser :execrows
DELETE FROM notification_reads WHERE user_id = @user_id;

-- name: DeleteNotificationTargetsForUser :execrows
DELETE FROM notification_targets WHERE target_type = 'USER' AND target_id = @user_id;

-- name: DeleteNotificationPreferencesForUser :execrows
DELETE FROM notification_preferences WHERE user_id = @user_id;
