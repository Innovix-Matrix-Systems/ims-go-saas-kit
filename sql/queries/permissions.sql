-- name: ListPermissions :many
SELECT * FROM permissions
ORDER BY "group", name;

-- name: GetPermission :one
SELECT * FROM permissions WHERE id = @id;

-- name: GetPermissionsByIDs :many
SELECT * FROM permissions WHERE id = ANY(@ids::bigint[]) ORDER BY id;

-- name: GetPermissionsByNames :many
SELECT * FROM permissions WHERE name = ANY(@names::text[]) ORDER BY id;

-- name: CreatePermission :one
INSERT INTO permissions (name, guard_name, "group", description, is_system)
VALUES (@name, 'sanctum', @group_name, @description, @is_system)
RETURNING *;

-- name: DeletePermission :execrows
DELETE FROM permissions WHERE id = @id AND is_system = false;

-- name: UpsertPermissions :execrows
INSERT INTO permissions (name, guard_name, "group", description, is_system)
SELECT unnest(@names::text[]), 'sanctum', unnest(@groups::text[]),
       unnest(@descriptions::text[]), unnest(@is_system_flags::boolean[])
ON CONFLICT (name, guard_name) DO UPDATE
SET "group"     = EXCLUDED."group",
    description = EXCLUDED.description,
    -- The catalog decides whether a permission is system-only, so a
    -- permission moved between system and tenant follows on the next sync.
    is_system   = EXCLUDED.is_system,
    updated_at  = now();

-- name: DeleteStaleSystemPermissions :execrows
DELETE FROM permissions
WHERE is_system = true
  AND name != ALL(@keep_names::text[]);

-- name: UserPermissionNames :many
SELECT DISTINCT p.name FROM permissions p
JOIN role_has_permissions rp ON rp.permission_id = p.id
JOIN model_has_roles mr ON mr.role_id = rp.role_id
WHERE mr.model_type = @model_type AND mr.model_id = @model_id
ORDER BY p.name;

-- name: UserPermissions :many
SELECT DISTINCT p.* FROM permissions p
JOIN role_has_permissions rp ON rp.permission_id = p.id
JOIN model_has_roles mr ON mr.role_id = rp.role_id
WHERE mr.model_type = @model_type AND mr.model_id = @model_id
ORDER BY p.id;

-- name: UserHasRole :one
SELECT EXISTS (
    SELECT 1 FROM model_has_roles mr
    JOIN roles r ON r.id = mr.role_id
    WHERE mr.model_type = @model_type AND mr.model_id = @model_id AND r.name = @role_name
      AND r.is_system = true AND r.tenant_id IS NULL
);

-- name: UserRoleNames :many
SELECT r.name FROM roles r
JOIN model_has_roles mr ON mr.role_id = r.id
WHERE mr.model_type = @model_type AND mr.model_id = @model_id
ORDER BY r.name;

-- name: UserIDsWithPermission :many
SELECT DISTINCT mr.model_id FROM model_has_roles mr
JOIN role_has_permissions rp ON rp.role_id = mr.role_id
WHERE mr.model_type = @model_type AND rp.permission_id = @permission_id;
