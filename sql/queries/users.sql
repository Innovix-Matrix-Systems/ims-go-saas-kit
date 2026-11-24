-- name: GetUser :one
SELECT * FROM users
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint);

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE lower(email) = lower(@email) AND deleted_at IS NULL;

-- name: GetAuthUser :one
-- The user plus whether their tenant may work: true for tenantless platform
-- accounts, false when the tenant is inactive or deleted (authentication).
SELECT sqlc.embed(u),
       coalesce(u.tenant_id IS NULL OR (t.is_active AND t.deleted_at IS NULL), false)::boolean AS tenant_active
FROM users u
LEFT JOIN tenants t ON t.id = u.tenant_id
WHERE u.id = @id AND u.deleted_at IS NULL;

-- name: GetAuthUserByEmail :one
SELECT sqlc.embed(u),
       coalesce(u.tenant_id IS NULL OR (t.is_active AND t.deleted_at IS NULL), false)::boolean AS tenant_active
FROM users u
LEFT JOIN tenants t ON t.id = u.tenant_id
WHERE lower(u.email) = lower(@email) AND u.deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE deleted_at IS NULL
  AND (@all_tenants::boolean OR users.tenant_id = @tenant_id)
  AND (NOT @platform_only::boolean OR users.tenant_id IS NULL)
  AND (@branch_id::bigint = 0 OR users.branch_id = @branch_id::bigint)
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR COALESCE(phone, '') ILIKE '%' || sqlc.narg('search')::text || '%'
  )
  AND (sqlc.narg('is_active')::smallint IS NULL OR is_active = sqlc.narg('is_active')::smallint)
  AND (
    sqlc.narg('role_name')::text IS NULL
    OR EXISTS (
      SELECT 1 FROM model_has_roles mr
      JOIN roles r ON r.id = mr.role_id
      WHERE mr.model_type = 'user' AND mr.model_id = users.id
        AND r.name = sqlc.narg('role_name')::text
    )
  )
  AND (
    @invitation::text = ''
    OR (@invitation::text = 'pending' AND invited_at IS NOT NULL AND invitation_accepted_at IS NULL)
    OR (@invitation::text = 'accepted' AND invitation_accepted_at IS NOT NULL)
  )
ORDER BY
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'asc' THEN id END ASC,
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'desc' THEN id END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN name END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN name END DESC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'asc' THEN lower(email) END ASC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'desc' THEN lower(email) END DESC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN created_at END DESC,
  id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountUsers :one
SELECT count(*) FROM users
WHERE deleted_at IS NULL
  AND (@all_tenants::boolean OR users.tenant_id = @tenant_id)
  AND (NOT @platform_only::boolean OR users.tenant_id IS NULL)
  AND (@branch_id::bigint = 0 OR users.branch_id = @branch_id::bigint)
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR COALESCE(phone, '') ILIKE '%' || sqlc.narg('search')::text || '%'
  )
  AND (sqlc.narg('is_active')::smallint IS NULL OR is_active = sqlc.narg('is_active')::smallint)
  AND (
    sqlc.narg('role_name')::text IS NULL
    OR EXISTS (
      SELECT 1 FROM model_has_roles mr
      JOIN roles r ON r.id = mr.role_id
      WHERE mr.model_type = 'user' AND mr.model_id = users.id
        AND r.name = sqlc.narg('role_name')::text
    )
  )
  AND (
    @invitation::text = ''
    OR (@invitation::text = 'pending' AND invited_at IS NOT NULL AND invitation_accepted_at IS NULL)
    OR (@invitation::text = 'accepted' AND invitation_accepted_at IS NOT NULL)
  );

-- name: CreateUser :one
INSERT INTO users (
    tenant_id, branch_id, active_branch_id, first_name, last_name, user_name,
    name, email, email_verified_at, phone, password, is_active, created_by, updated_by,
    invited_at, invited_by
)
VALUES (
    @tenant_id, @branch_id, @active_branch_id, @first_name, @last_name, @user_name,
    @name, @email, @email_verified_at, @phone, @password, @is_active, @created_by, @updated_by,
    sqlc.narg('invited_at'), sqlc.narg('invited_by')
)
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET first_name  = @first_name,
    last_name   = @last_name,
    name        = @name,
    phone       = @phone,
    is_active   = @is_active,
    branch_id   = @branch_id,
    updated_by  = @updated_by,
    updated_at  = now()
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint)
  -- Optional optimistic lock: when the client sends the updated_at it last
  -- saw, a concurrent change makes this match nothing (409 in the service).
  AND (sqlc.narg('expected_updated_at')::timestamptz IS NULL
       OR updated_at = sqlc.narg('expected_updated_at')::timestamptz)
RETURNING *;

-- name: SoftDeleteUser :execrows
UPDATE users
SET deleted_at = now(), updated_by = @updated_by, updated_at = now()
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint);

-- name: UpdateUserPassword :execrows
UPDATE users
SET password = @password, updated_by = @updated_by, updated_at = now()
WHERE id = @id AND deleted_at IS NULL
  AND (@all_tenants::boolean OR tenant_id = @tenant_id::bigint);

-- name: TouchUserLogin :execrows
UPDATE users
SET last_login_at = now(), last_active_device = @device, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: SwitchUserBranch :execrows
UPDATE users
SET active_branch_id = @branch_id, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: ClearUserActiveBranch :execrows
-- Back to every branch: the member works in their own branch again.
UPDATE users
SET active_branch_id = NULL, updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: EmailExists :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE lower(email) = lower(@email)
      AND deleted_at IS NULL
      AND (@exclude_id::bigint IS NULL OR id <> @exclude_id::bigint)
);

-- name: PhoneExists :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE phone = @phone
      AND deleted_at IS NULL
      AND (@exclude_id::bigint IS NULL OR id <> @exclude_id::bigint)
);

-- name: ListUsersForExport :many
SELECT u.id, u.name, u.email, u.phone, u.is_active, u.branch_id, u.created_at,
       COALESCE(b.name, '')::text AS branch_name,
       COALESCE((
           SELECT string_agg(r.name, ', ' ORDER BY r.name)
           FROM model_has_roles mr
           JOIN roles r ON r.id = mr.role_id
           WHERE mr.model_type = 'user' AND mr.model_id = u.id
       ), '')::text AS role_names
FROM users u
LEFT JOIN branches b ON b.id = u.branch_id
WHERE u.deleted_at IS NULL
  AND (@all_tenants::boolean OR u.tenant_id = @tenant_id::bigint)
  AND (
      sqlc.narg('search')::text IS NULL
      OR u.name ILIKE '%' || sqlc.narg('search') || '%'
      OR u.email ILIKE '%' || sqlc.narg('search') || '%'
      OR u.phone ILIKE '%' || sqlc.narg('search') || '%'
  )
  AND (sqlc.narg('is_active')::boolean IS NULL OR (u.is_active = 1) = sqlc.narg('is_active')::boolean)
  AND (
      sqlc.narg('role_name')::text IS NULL
      OR EXISTS (
          SELECT 1 FROM model_has_roles mr2
          JOIN roles r2 ON r2.id = mr2.role_id
          WHERE mr2.model_type = 'user' AND mr2.model_id = u.id AND r2.name = sqlc.narg('role_name')
      )
  )
ORDER BY
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'asc' THEN u.id END ASC,
  CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'desc' THEN u.id END DESC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'asc' THEN lower(u.name) END ASC,
  CASE WHEN @order_by::text = 'name' AND @order_dir::text = 'desc' THEN lower(u.name) END DESC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'asc' THEN lower(u.email) END ASC,
  CASE WHEN @order_by::text = 'email' AND @order_dir::text = 'desc' THEN lower(u.email) END DESC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN u.created_at END ASC,
  CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN u.created_at END DESC,
  u.id DESC
LIMIT @page_limit OFFSET @page_offset;
-- name: MarkUserEmailVerified :execrows
UPDATE users
SET email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: MarkUserInvited :execrows
-- (Re)sends an invitation: only to a user who has not accepted one and has
-- never signed in.
UPDATE users
SET invited_at = now(), invited_by = sqlc.narg('invited_by'), updated_at = now()
WHERE id = @id
  AND deleted_at IS NULL
  AND invitation_accepted_at IS NULL
  AND last_login_at IS NULL;

-- name: AcceptUserInvitation :execrows
-- The invitee's own password activates the account; the link proved the
-- mailbox, so the address is verified too. Pending invitations only.
UPDATE users
SET password               = @password,
    is_active              = 1,
    email_verified_at      = coalesce(email_verified_at, now()),
    invitation_accepted_at = now(),
    updated_at             = now()
WHERE id = @id
  AND deleted_at IS NULL
  AND invited_at IS NOT NULL
  AND invitation_accepted_at IS NULL;

-- name: GetUserForPrivacy :one
-- Any user row, soft-deleted or erased included: erasure and export run on
-- accounts the normal reads no longer return.
SELECT * FROM users WHERE id = @id;

-- name: EraseUser :execrows
-- Tombstone (docs/design/account-deletion-and-export.md D1): keep the id so
-- created_by-style references stay valid, blank every personal column.
UPDATE users
SET name = 'Deleted user',
    email = 'erased+' || id::text || '@invalid',
    first_name = NULL,
    last_name = NULL,
    user_name = NULL,
    phone = NULL,
    phone_verified_at = NULL,
    email_verified_at = NULL,
    password = @password,
    remember_token = NULL,
    last_active_device = NULL,
    last_login_at = NULL,
    is_active = 0,
    deleted_at = coalesce(deleted_at, now()),
    erased_at = now(),
    updated_at = now()
WHERE id = @id AND erased_at IS NULL;

-- name: CountOtherActiveRoleHolders :one
-- Active users of the tenant, other than the given user, holding the named
-- role (the last-tenant-admin guard).
SELECT count(DISTINCT u.id)::bigint FROM users u
JOIN model_has_roles mr ON mr.model_type = @model_type AND mr.model_id = u.id
JOIN roles r ON r.id = mr.role_id
WHERE r.name = @role_name
  AND u.tenant_id = @tenant_id
  AND u.id <> @user_id
  AND u.is_active = 1
  AND u.deleted_at IS NULL;

-- name: ChangeUserEmail :exec
-- The user confirmed the new address from its mailbox, so it is verified.
UPDATE users
SET email = @email, email_verified_at = now(), updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: UpdateUserLocale :exec
UPDATE users SET locale = sqlc.narg('locale'), updated_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: GetInvitationPreview :one
-- What an invitee sees before choosing a password: their name, the
-- organization and who invited them.
SELECT u.id, u.name, u.email,
       u.invitation_accepted_at,
       COALESCE(t.name, '')::text AS organization_name,
       COALESCE(inviter.name, '')::text AS inviter_name
FROM users u
LEFT JOIN tenants t ON t.id = u.tenant_id
LEFT JOIN users inviter ON inviter.id = u.invited_by
WHERE u.id = @id AND u.deleted_at IS NULL;

-- name: IsTenantOwner :one
-- Whether the user owns a (live) organization.
SELECT EXISTS (
    SELECT 1 FROM tenants WHERE owner_user_id = @user_id AND deleted_at IS NULL
)::boolean;

-- name: ListUsersWithPermission :many
-- Active accounts of one tenant (0: the tenantless platform accounts) that
-- hold a permission through one of their roles, or the Super-Admin role:
-- who to tell about something only they can act on (a payment to review).
SELECT DISTINCT u.id, u.tenant_id, u.name, u.email, u.locale
FROM users u
JOIN model_has_roles mr ON mr.model_type = 'user' AND mr.model_id = u.id
JOIN roles r ON r.id = mr.role_id
WHERE u.deleted_at IS NULL
  AND u.erased_at IS NULL
  AND u.is_active = 1
  AND ((@tenant_id::bigint = 0 AND u.tenant_id IS NULL) OR u.tenant_id = @tenant_id::bigint)
  AND (r.name = @super_admin_role::text OR EXISTS (
      SELECT 1 FROM role_has_permissions rp
      JOIN permissions p ON p.id = rp.permission_id
      WHERE rp.role_id = r.id AND p.name = @permission::text))
ORDER BY u.id;
