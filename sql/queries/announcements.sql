-- Announcements: one row each; the audience is every organization or the
-- ones in announcement_tenants. "Live" means published and inside its window.

-- name: CreateAnnouncement :one
INSERT INTO announcements (
    title, body, kind, cta_label, cta_url, audience, starts_at, ends_at,
    dismissible, status, channels, created_by, updated_by
)
VALUES (
    @title, sqlc.narg('body'), @kind, sqlc.narg('cta_label'), sqlc.narg('cta_url'), @audience,
    @starts_at, sqlc.narg('ends_at'), @dismissible, @status, @channels::text[], @actor_id, @actor_id
)
RETURNING *;

-- name: UpdateAnnouncement :one
UPDATE announcements
SET title = @title,
    body = sqlc.narg('body'),
    kind = @kind,
    cta_label = sqlc.narg('cta_label'),
    cta_url = sqlc.narg('cta_url'),
    audience = @audience,
    starts_at = @starts_at,
    ends_at = sqlc.narg('ends_at'),
    dismissible = @dismissible,
    status = @status,
    channels = @channels::text[],
    updated_by = @actor_id,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetAnnouncement :one
SELECT * FROM announcements WHERE id = @id;

-- name: DeleteAnnouncement :execrows
DELETE FROM announcements WHERE id = @id;

-- name: ListAnnouncements :many
-- Newest first, with the audience size and unique views, clicks and
-- dismissals.
SELECT a.*,
       (SELECT count(*) FROM announcement_tenants t WHERE t.announcement_id = a.id)::bigint AS tenant_count,
       (SELECT count(*) FROM announcement_events e WHERE e.announcement_id = a.id AND e.type = 'view')::bigint AS views,
       (SELECT count(*) FROM announcement_events e WHERE e.announcement_id = a.id AND e.type = 'click')::bigint AS clicks,
       (SELECT count(*) FROM announcement_events e WHERE e.announcement_id = a.id AND e.type = 'dismiss')::bigint AS dismissals
FROM announcements a
WHERE (sqlc.narg('status')::text IS NULL OR a.status = sqlc.narg('status')::text)
ORDER BY a.created_at DESC, a.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountAnnouncements :one
SELECT count(*) FROM announcements
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: ListAnnouncementTenants :many
SELECT t.id, t.name
FROM announcement_tenants at
JOIN tenants t ON t.id = at.tenant_id
WHERE at.announcement_id = @announcement_id
ORDER BY t.name;

-- name: DeleteAnnouncementTenants :exec
DELETE FROM announcement_tenants WHERE announcement_id = @announcement_id;

-- name: AddAnnouncementTenants :exec
INSERT INTO announcement_tenants (announcement_id, tenant_id)
SELECT @announcement_id, t.id FROM tenants t WHERE t.id = ANY(@tenant_ids::bigint[]) AND t.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: ListLiveAnnouncements :many
-- What a member of the tenant sees now, most urgent first, without the
-- ones they dismissed.
SELECT a.*
FROM announcements a
WHERE a.status = 'published'
  AND a.starts_at <= now()
  AND (a.ends_at IS NULL OR a.ends_at > now())
  AND (a.audience = 'all' OR EXISTS (
      SELECT 1 FROM announcement_tenants t WHERE t.announcement_id = a.id AND t.tenant_id = @tenant_id::bigint))
  AND NOT EXISTS (
      SELECT 1 FROM announcement_events e
      WHERE e.announcement_id = a.id AND e.user_id = @user_id::bigint AND e.type = 'dismiss')
ORDER BY CASE a.kind WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 WHEN 'promotion' THEN 2 ELSE 3 END,
         a.starts_at DESC, a.id DESC
LIMIT 10;

-- name: GetLiveAnnouncementForTenant :one
SELECT a.*
FROM announcements a
WHERE a.id = @id
  AND a.status = 'published'
  AND a.starts_at <= now()
  AND (a.ends_at IS NULL OR a.ends_at > now())
  AND (a.audience = 'all' OR EXISTS (
      SELECT 1 FROM announcement_tenants t WHERE t.announcement_id = a.id AND t.tenant_id = @tenant_id::bigint));

-- name: RecordAnnouncementEvent :exec
-- The first time a person views, clicks or dismisses; repeats are ignored.
INSERT INTO announcement_events (announcement_id, user_id, tenant_id, type)
VALUES (@announcement_id, @user_id, sqlc.narg('tenant_id'), @type)
ON CONFLICT DO NOTHING;

-- name: AnnouncementTotals :one
SELECT
    count(*) FILTER (WHERE type = 'view')::bigint AS views,
    count(*) FILTER (WHERE type = 'click')::bigint AS clicks,
    count(*) FILTER (WHERE type = 'dismiss')::bigint AS dismissals
FROM announcement_events
WHERE announcement_id = @announcement_id;

-- name: ListAnnouncementTenantTotals :many
-- Each organization whose members reacted, most views first, a page at a
-- time, optionally by name.
SELECT e.tenant_id::bigint AS tenant_id, t.name,
       count(*) FILTER (WHERE e.type = 'view')::bigint AS views,
       count(*) FILTER (WHERE e.type = 'click')::bigint AS clicks,
       count(*) FILTER (WHERE e.type = 'dismiss')::bigint AS dismissals
FROM announcement_events e
JOIN tenants t ON t.id = e.tenant_id
WHERE e.announcement_id = @announcement_id
  AND (sqlc.narg('search')::text IS NULL OR t.name ILIKE '%' || sqlc.narg('search')::text || '%')
GROUP BY e.tenant_id, t.name
ORDER BY count(*) FILTER (WHERE e.type = 'view') DESC, t.name, e.tenant_id
LIMIT @page_limit OFFSET @page_offset;

-- name: CountAnnouncementTenantTotals :one
SELECT count(DISTINCT e.tenant_id)::bigint
FROM announcement_events e
JOIN tenants t ON t.id = e.tenant_id
WHERE e.announcement_id = @announcement_id
  AND (sqlc.narg('search')::text IS NULL OR t.name ILIKE '%' || sqlc.narg('search')::text || '%');

-- name: ListAnnouncementsToDeliver :many
-- Live announcements with channels that have not gone out yet.
SELECT * FROM announcements
WHERE status = 'published'
  AND cardinality(channels) > 0
  AND delivered_at IS NULL
  AND starts_at <= now()
  AND (ends_at IS NULL OR ends_at > now())
ORDER BY starts_at, id
LIMIT 50;

-- name: MarkAnnouncementDelivered :execrows
-- Claims the delivery: only the first caller gets a row back.
UPDATE announcements SET delivered_at = now()
WHERE id = @id AND delivered_at IS NULL;

-- name: ListAnnouncementAudienceTenantIDs :many
-- The organizations an announcement reaches: every active one, or the
-- listed ones that are still active.
SELECT t.id FROM tenants t
WHERE t.deleted_at IS NULL AND t.is_active
  AND (@audience::text = 'all' OR EXISTS (
      SELECT 1 FROM announcement_tenants at WHERE at.announcement_id = @announcement_id AND at.tenant_id = t.id))
ORDER BY t.id;

-- name: ListAnnouncementEventsForUser :many
SELECT a.title, e.type, e.created_at
FROM announcement_events e
JOIN announcements a ON a.id = e.announcement_id
WHERE e.user_id = @user_id
ORDER BY e.created_at;

-- name: DeleteAnnouncementEventsForUser :execrows
DELETE FROM announcement_events WHERE user_id = @user_id;

-- name: ListAnnouncementsStartedOrEnded :many
-- Published announcements whose window opened or closed in (since, until]:
-- the scheduler tells their audience's open apps to refetch.
SELECT * FROM announcements
WHERE status = 'published'
  AND ((starts_at > @since AND starts_at <= @until)
    OR (ends_at > @since AND ends_at <= @until))
ORDER BY id;
