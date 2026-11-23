-- name: CreateFile :one
INSERT INTO files (
    uuid, name, file_name, disk, path, mime_type, size, type,
    owner_type, owner_id, collection, tenant_id, created_by, updated_by
)
VALUES (
    @uuid, @name, @file_name, @disk, @path, @mime_type, @size, @type,
    @owner_type, @owner_id, @collection, @tenant_id, @created_by, @updated_by
)
RETURNING *;

-- name: GetFile :one
SELECT * FROM files WHERE id = @id;

-- Scoped reads: all_tenants is only true for system operators (file.view.all
-- is a system permission). A tenant-bound caller matches their tenant only;
-- a tenantless caller matches global (tenant_id IS NULL) files only. Rows
-- with no tenant are never visible to tenant-bound callers.

-- name: GetFileInTenant :one
SELECT * FROM files
WHERE id = @id
  AND (
      @all_tenants::boolean
      OR tenant_id = @tenant_id
      OR (tenant_id IS NULL AND @tenant_id::bigint = 0)
  );

-- name: GetFileByOwner :one
SELECT * FROM files
WHERE owner_type = @owner_type::text
  AND owner_id = @owner_id::bigint
  AND collection = @collection::text
ORDER BY id DESC
LIMIT 1;

-- name: ListFiles :many
SELECT * FROM files
WHERE (
      @all_tenants::boolean
      OR tenant_id = @tenant_id
      OR (tenant_id IS NULL AND @tenant_id::bigint = 0)
  )
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
  AND (sqlc.narg('attached')::boolean IS NULL OR (owner_id IS NOT NULL) = sqlc.narg('attached')::boolean)
ORDER BY created_at DESC, id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountFiles :one
SELECT count(*) FROM files
WHERE (
      @all_tenants::boolean
      OR tenant_id = @tenant_id
      OR (tenant_id IS NULL AND @tenant_id::bigint = 0)
  )
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
  AND (sqlc.narg('attached')::boolean IS NULL OR (owner_id IS NOT NULL) = sqlc.narg('attached')::boolean);

-- name: DeleteFileInTenant :execrows
DELETE FROM files
WHERE id = @id
  AND (
      @all_tenants::boolean
      OR tenant_id = @tenant_id
      OR (tenant_id IS NULL AND @tenant_id::bigint = 0)
  );

-- name: DeleteFilesByOwner :many
DELETE FROM files
WHERE owner_type = @owner_type::text
  AND owner_id = @owner_id::bigint
  AND collection = @collection::text
RETURNING *;

-- name: ListProfileFilesByUserIDs :many
SELECT * FROM files
WHERE owner_type = 'user' AND collection = 'profile'
  AND owner_id = ANY(@user_ids::bigint[]);

-- name: SumFileBytesForTenant :one
-- Storage used by a tenant (storage.mb plan limit).
SELECT COALESCE(sum(size), 0)::bigint FROM files WHERE tenant_id = @tenant_id::bigint;

-- name: ListFilesOwnedBy :many
SELECT * FROM files
WHERE owner_type = @owner_type::text AND owner_id = @owner_id::bigint
ORDER BY id;

-- name: DeleteFilesOwnedBy :many
DELETE FROM files
WHERE owner_type = @owner_type::text AND owner_id = @owner_id::bigint
RETURNING *;
