-- name: GetBranch :one
SELECT * FROM branches
WHERE id = @id
  AND (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND deleted_at IS NULL;

-- name: ListBranches :many
SELECT * FROM branches
WHERE (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND deleted_at IS NULL
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR COALESCE(address, '') ILIKE '%' || sqlc.narg('search')::text || '%'
  )
ORDER BY
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'asc' THEN id END ASC,
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'desc' THEN id END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN name END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN name END DESC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN created_at END DESC,
  id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountBranches :one
SELECT count(*) FROM branches
WHERE (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND deleted_at IS NULL
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR COALESCE(address, '') ILIKE '%' || sqlc.narg('search')::text || '%'
  );

-- name: CreateBranch :one
INSERT INTO branches (tenant_id, name, address, description, phone, email, manager_id, created_by, updated_by)
VALUES (@tenant_id, @name, @address, @description, @phone, @email, @manager_id, @created_by, @updated_by)
RETURNING *;

-- name: CreateBranchWithID :one
INSERT INTO branches (id, tenant_id, name, address, description, phone, email, manager_id, created_by, updated_by)
VALUES (@id, @tenant_id, @name, @address, @description, @phone, @email, @manager_id, @created_by, @updated_by)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: UpdateBranch :one
UPDATE branches
SET name        = @name,
    address     = @address,
    description = @description,
    phone       = @phone,
    email       = @email,
    manager_id  = @manager_id,
    updated_by  = @updated_by,
    updated_at  = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteBranch :execrows
UPDATE branches
SET deleted_at = now(), updated_by = @updated_by, updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: SoftDeleteBranchesForTenant :execrows
UPDATE branches
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: BranchBelongsToTenant :one
SELECT EXISTS (
    SELECT 1 FROM branches
    WHERE id = @branch_id AND tenant_id = @tenant_id AND deleted_at IS NULL
);

-- name: SyncBranchIDSequence :exec
SELECT setval(pg_get_serial_sequence('branches', 'id'), (SELECT COALESCE(MAX(id), 1) FROM branches));

-- name: FirstBranchOfTenant :one
-- The branch public tenant routes use when the client sends no X-Branch-Id.
SELECT id, name FROM branches
WHERE tenant_id = @tenant_id AND deleted_at IS NULL
ORDER BY id
LIMIT 1;

-- name: GetTenantBranchName :one
SELECT name FROM branches
WHERE id = @branch_id AND tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: CountTenantBranches :one
SELECT count(*) FROM branches WHERE tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: CountBranchMembers :one
SELECT count(*) FROM users
WHERE tenant_id = @tenant_id AND branch_id = @branch_id AND deleted_at IS NULL;

-- name: ClearActiveBranch :execrows
UPDATE users SET active_branch_id = NULL, updated_at = now()
WHERE tenant_id = @tenant_id AND active_branch_id = @branch_id;
