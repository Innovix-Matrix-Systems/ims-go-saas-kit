-- name: GetTenant :one
SELECT * FROM tenants
WHERE id = @id AND deleted_at IS NULL;

-- name: GetTenantByCode :one
SELECT * FROM tenants
WHERE code = @code AND deleted_at IS NULL;

-- name: ListTenants :many
SELECT * FROM tenants
WHERE deleted_at IS NULL
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR code ILIKE '%' || sqlc.narg('search')::text || '%'
  )
ORDER BY id
LIMIT @page_limit OFFSET @page_offset;

-- name: ListTenantsByIDs :many
SELECT id, name, code FROM tenants
WHERE id = ANY(@ids::bigint[])
  AND deleted_at IS NULL
ORDER BY id;

-- name: CountTenants :one
SELECT count(*) FROM tenants
WHERE deleted_at IS NULL
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR code ILIKE '%' || sqlc.narg('search')::text || '%'
  );

-- name: CreateTenant :one
INSERT INTO tenants (name, code, slug, domain, phone, email, is_active, created_by, updated_by)
VALUES (@name, @code, @slug, @domain, @phone, @email, @is_active, @created_by, @updated_by)
RETURNING *;

-- name: CreateTenantWithID :one
INSERT INTO tenants (id, name, code, slug, domain, phone, email, is_active, created_by, updated_by)
VALUES (@id, @name, @code, @slug, @domain, @phone, @email, @is_active, @created_by, @updated_by)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: UpdateTenant :one
UPDATE tenants
SET name       = @name,
    slug       = @slug,
    domain     = @domain,
    phone      = @phone,
    email      = @email,
    is_active  = @is_active,
    updated_by = @updated_by,
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteTenant :execrows
UPDATE tenants
SET deleted_at = now(),
    updated_by = @updated_by,
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SyncTenantIDSequence :exec
SELECT setval(pg_get_serial_sequence('tenants', 'id'), (SELECT COALESCE(MAX(id), 1) FROM tenants));

-- name: SetTenantOwner :exec
UPDATE tenants SET owner_user_id = @owner_user_id, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: GetTenantOwner :one
-- The organization's owner, for showing on the organization page.
SELECT u.id, u.name, u.email
FROM tenants t
JOIN users u ON u.id = t.owner_user_id AND u.deleted_at IS NULL
WHERE t.id = @tenant_id AND t.deleted_at IS NULL;

-- name: ListActiveTenantIDs :many
-- Every organization that is live (a staff broadcast reaches these).
SELECT id FROM tenants WHERE deleted_at IS NULL AND is_active ORDER BY id;
