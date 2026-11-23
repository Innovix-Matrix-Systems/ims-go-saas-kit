-- name: GetRole :one
SELECT * FROM roles
WHERE id = @id
  AND (tenant_id = @tenant_id OR (tenant_id IS NULL AND is_system));

-- name: GetRoleByID :one
SELECT * FROM roles
WHERE id = @id;

-- name: GetRoleByName :one
SELECT * FROM roles
WHERE name = @name
  AND (tenant_id = @tenant_id OR (tenant_id IS NULL AND is_system))
ORDER BY tenant_id NULLS LAST
LIMIT 1;

-- name: ListRoles :many
SELECT * FROM roles
WHERE (tenant_id = @tenant_id OR (tenant_id IS NULL AND is_system))
  AND hidden = false
ORDER BY id;

-- name: CreateRole :one
INSERT INTO roles (tenant_id, name, guard_name, is_system, hidden, description, created_by, updated_by)
VALUES (@tenant_id, @name, 'sanctum', @is_system, @hidden, @description, @created_by, @updated_by)
RETURNING *;

-- name: SetRoleHidden :exec
UPDATE roles
SET hidden = @hidden, updated_at = now()
WHERE id = @id;

-- name: UpdateRole :one
UPDATE roles
SET name        = @name,
    description = @description,
    updated_by  = @updated_by,
    updated_at  = now()
WHERE id = @id
RETURNING *;

-- name: DeleteRole :execrows
DELETE FROM roles WHERE id = @id;

-- name: UpdateTenantRole :one
UPDATE roles
SET name        = @name,
    description = @description,
    updated_by  = @updated_by,
    updated_at  = now()
WHERE id = @id AND tenant_id = @tenant_id::bigint AND is_system = false
RETURNING *;

-- name: DeleteTenantRole :execrows
DELETE FROM roles WHERE id = @id AND tenant_id = @tenant_id::bigint AND is_system = false;

-- name: RoleExistsForTenant :one
SELECT EXISTS (
    SELECT 1 FROM roles
    WHERE id = @id AND (tenant_id = @tenant_id OR (tenant_id IS NULL AND is_system))
);

-- name: RoleAssignableToTenant :one
SELECT EXISTS (
    SELECT 1 FROM roles
    WHERE id = @id AND tenant_id = @tenant_id AND is_system = false
);

-- name: ClearRolePermissions :exec
DELETE FROM role_has_permissions WHERE role_id = @role_id;

-- name: AssignRolePermissions :exec
INSERT INTO role_has_permissions (role_id, permission_id)
SELECT @role_id, unnest(@permission_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: RolePermissionIDs :many
SELECT permission_id FROM role_has_permissions WHERE role_id = @role_id ORDER BY permission_id;

-- name: ClearUserRoles :exec
DELETE FROM model_has_roles WHERE model_type = @model_type AND model_id = @model_id;

-- name: AssignUserRoles :exec
INSERT INTO model_has_roles (role_id, model_type, model_id)
SELECT unnest(@role_ids::bigint[]), @model_type, @model_id
ON CONFLICT DO NOTHING;

-- name: UserRoleRefs :many
SELECT r.id, r.name, r.is_system FROM roles r
JOIN model_has_roles mr ON mr.role_id = r.id
WHERE mr.model_type = @model_type AND mr.model_id = @model_id
  AND r.hidden = false
ORDER BY r.id;

-- name: UserRoleRefsForUsers :many
-- Role refs for a whole page of users in one query (no N+1).
SELECT mr.model_id AS user_id, r.id, r.name, r.is_system FROM roles r
JOIN model_has_roles mr ON mr.role_id = r.id
WHERE mr.model_type = @model_type AND mr.model_id = ANY(@user_ids::bigint[])
  AND r.hidden = false
ORDER BY mr.model_id, r.id;

-- name: ListUserIDsWithRole :many
SELECT model_id FROM model_has_roles
WHERE model_type = @model_type AND role_id = @role_id;

-- name: CountUsersWithRole :one
SELECT count(*) FROM model_has_roles
WHERE model_type = @model_type AND role_id = @role_id;

-- name: ListRoleIDsByNames :many
SELECT id, name FROM roles
WHERE name = ANY(@names::text[])
  AND (
    (@tenant_id::bigint <> 0 AND tenant_id = @tenant_id::bigint)
    OR (@tenant_id::bigint = 0 AND tenant_id IS NULL)
  )
ORDER BY id;
-- name: RemoveUserRole :execrows
DELETE FROM model_has_roles
WHERE role_id = @role_id
  AND model_type = @model_type
  AND model_id = @model_id;

-- name: CountRolePermissions :one
SELECT count(*) FROM role_has_permissions WHERE role_id = @role_id;
