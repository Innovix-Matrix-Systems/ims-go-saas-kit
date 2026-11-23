-- name: UpsertSystemGroup :one
INSERT INTO settings_groups (slug, name, description, sort_order, is_system)
VALUES (@slug, @name, @description, @sort_order, true)
ON CONFLICT (slug) DO UPDATE
SET name       = EXCLUDED.name,
    description = EXCLUDED.description,
    sort_order  = EXCLUDED.sort_order,
    updated_at  = now()
WHERE settings_groups.is_system
RETURNING *;

-- name: ListSettingsGroups :many
SELECT * FROM settings_groups
ORDER BY sort_order, id;

-- name: GetSettingsGroup :one
SELECT * FROM settings_groups
WHERE id = @id;

-- name: GetSettingsGroupBySlug :one
SELECT * FROM settings_groups
WHERE slug = @slug;

-- name: CreateSettingsGroup :one
INSERT INTO settings_groups (slug, name, description, sort_order, is_system, created_by, updated_by)
VALUES (@slug, @name, @description, @sort_order, false, @actor_id, @actor_id)
RETURNING *;

-- name: UpdateSettingsGroup :one
UPDATE settings_groups
SET slug        = @slug,
    name        = @name,
    description = @description,
    sort_order  = @sort_order,
    updated_by  = @actor_id,
    updated_at  = now()
WHERE id = @id
RETURNING *;

-- name: DeleteSettingsGroup :execrows
DELETE FROM settings_groups
WHERE id = @id;

-- name: ListSettingsGroupsPaginated :many
SELECT * FROM settings_groups
WHERE (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY
  CASE WHEN @order_by::text = 'sort_order' AND @order_dir::text = 'asc' THEN sort_order END ASC,
  CASE WHEN @order_by::text = 'sort_order' AND @order_dir::text = 'desc' THEN sort_order END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN lower(name) END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN lower(name) END DESC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN created_at END DESC,
  sort_order ASC, id ASC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountSettingsGroups :one
SELECT count(*) FROM settings_groups
WHERE (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search')::text || '%');

-- name: ListSettingsGroupsLight :many
SELECT * FROM settings_groups
ORDER BY
  CASE WHEN @order_by::text = 'sort_order' AND @order_dir::text = 'asc' THEN sort_order END ASC,
  CASE WHEN @order_by::text = 'sort_order' AND @order_dir::text = 'desc' THEN sort_order END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN lower(name) END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN lower(name) END DESC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN created_at END DESC,
  sort_order ASC, id ASC;

-- name: SetSettingsGroupSortOrder :execrows
UPDATE settings_groups SET sort_order = @sort_order, updated_at = now() WHERE id = @id;
