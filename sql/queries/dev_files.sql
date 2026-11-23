-- name: ListDevFiles :many
SELECT
    f.id,
    f.uuid,
    f.name,
    f.file_name,
    f.disk,
    f.path,
    f.mime_type,
    f.size,
    f.type,
    f.owner_type,
    f.owner_id,
    f.collection,
    f.created_by,
    f.created_at,
    f.updated_at,
    u.name AS uploader_name,
    u.email AS uploader_email,
    f.tenant_id,
    t.name AS tenant_name,
    o.name AS owner_name,
    o.email AS owner_email
FROM files f
LEFT JOIN users u ON u.id = f.created_by AND u.deleted_at IS NULL
LEFT JOIN tenants t ON t.id = f.tenant_id AND t.deleted_at IS NULL
LEFT JOIN users o ON f.owner_type = 'user' AND o.id = f.owner_id AND o.deleted_at IS NULL
WHERE (
    @search::text = ''
    OR f.name ILIKE '%' || @search::text || '%'
    OR f.file_name ILIKE '%' || @search::text || '%'
  )
  AND (sqlc.narg('type')::text IS NULL OR f.type = sqlc.narg('type')::text)
  AND (sqlc.narg('disk')::text IS NULL OR f.disk = sqlc.narg('disk')::text)
  AND (sqlc.narg('attached')::boolean IS NULL OR (f.owner_id IS NOT NULL) = sqlc.narg('attached')::boolean)
  AND (sqlc.narg('owner_type')::text IS NULL OR f.owner_type = sqlc.narg('owner_type')::text)
  AND (sqlc.narg('owner_id')::bigint IS NULL OR f.owner_id = sqlc.narg('owner_id')::bigint)
  AND (sqlc.narg('created_by')::bigint IS NULL OR f.created_by = sqlc.narg('created_by')::bigint)
  AND (sqlc.narg('tenant_id')::bigint IS NULL OR f.tenant_id = sqlc.narg('tenant_id')::bigint)
  AND (sqlc.narg('created_from')::timestamptz IS NULL OR f.created_at >= sqlc.narg('created_from')::timestamptz)
  AND (sqlc.narg('created_to')::timestamptz IS NULL OR f.created_at <= sqlc.narg('created_to')::timestamptz)
ORDER BY
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN f.created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN f.created_at END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN lower(f.name) END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN lower(f.name) END DESC,
  CASE WHEN @order_by::text = 'size' AND @order_dir::text = 'asc' THEN f.size END ASC,
  CASE WHEN @order_by::text = 'size' AND @order_dir::text = 'desc' THEN f.size END DESC,
  f.created_at DESC,
  f.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountDevFiles :one
SELECT count(*)
FROM files f
WHERE (
    @search::text = ''
    OR f.name ILIKE '%' || @search::text || '%'
    OR f.file_name ILIKE '%' || @search::text || '%'
  )
  AND (sqlc.narg('type')::text IS NULL OR f.type = sqlc.narg('type')::text)
  AND (sqlc.narg('disk')::text IS NULL OR f.disk = sqlc.narg('disk')::text)
  AND (sqlc.narg('attached')::boolean IS NULL OR (f.owner_id IS NOT NULL) = sqlc.narg('attached')::boolean)
  AND (sqlc.narg('owner_type')::text IS NULL OR f.owner_type = sqlc.narg('owner_type')::text)
  AND (sqlc.narg('owner_id')::bigint IS NULL OR f.owner_id = sqlc.narg('owner_id')::bigint)
  AND (sqlc.narg('created_by')::bigint IS NULL OR f.created_by = sqlc.narg('created_by')::bigint)
  AND (sqlc.narg('tenant_id')::bigint IS NULL OR f.tenant_id = sqlc.narg('tenant_id')::bigint)
  AND (sqlc.narg('created_from')::timestamptz IS NULL OR f.created_at >= sqlc.narg('created_from')::timestamptz)
  AND (sqlc.narg('created_to')::timestamptz IS NULL OR f.created_at <= sqlc.narg('created_to')::timestamptz);

-- name: DevFileStats :many
SELECT
    f.type,
    f.disk,
    (f.owner_id IS NOT NULL)::boolean AS attached,
    count(*)::bigint AS files,
    COALESCE(sum(f.size), 0)::bigint AS total_size
FROM files f
WHERE (sqlc.narg('tenant_id')::bigint IS NULL OR f.tenant_id = sqlc.narg('tenant_id')::bigint)
GROUP BY f.type, f.disk, (f.owner_id IS NOT NULL)
ORDER BY f.type ASC, f.disk ASC;
