-- The OAuth authorization server behind the MCP connector.

-- ── Registered clients ─────────────────────────────────────────────

-- name: CreateOAuthClient :one
INSERT INTO oauth_clients (client_id, name, type, secret_hash, redirect_uris, scopes, tenant_id, is_active, created_by)
VALUES (@client_id, @name, @type, sqlc.narg('secret_hash'), @redirect_uris::text[], @scopes::text[],
        sqlc.narg('tenant_id'), @is_active, sqlc.narg('created_by'))
RETURNING *;

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients WHERE id = @id;

-- name: GetOAuthClientByClientID :one
SELECT * FROM oauth_clients WHERE client_id = @client_id;

-- name: ListOAuthClients :many
SELECT * FROM oauth_clients
WHERE (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR client_id ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY id DESC
LIMIT @row_limit OFFSET @row_offset;

-- name: CountOAuthClients :one
SELECT count(*) FROM oauth_clients
WHERE (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR client_id ILIKE '%' || sqlc.narg('search')::text || '%');

-- name: UpdateOAuthClient :one
UPDATE oauth_clients
SET name = @name, redirect_uris = @redirect_uris::text[], scopes = @scopes::text[],
    tenant_id = sqlc.narg('tenant_id'), is_active = @is_active, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RotateOAuthClientSecret :one
UPDATE oauth_clients
SET previous_secret_hash = secret_hash,
    previous_secret_expires_at = @previous_expires_at,
    secret_hash = @secret_hash,
    updated_at = now()
WHERE id = @id AND type = 'confidential'
RETURNING *;

-- name: DeleteOAuthClient :execrows
DELETE FROM oauth_clients WHERE id = @id;

-- ── Grants (connections) ───────────────────────────────────────────

-- name: CreateOAuthGrant :one
INSERT INTO oauth_grants (user_id, client_id, client_name, redirect_uri, scopes, tenant_id, branch_id)
VALUES (@user_id, @client_id, @client_name, @redirect_uri, @scopes::text[], sqlc.narg('tenant_id'), sqlc.narg('branch_id'))
RETURNING *;

-- name: GetOAuthGrant :one
SELECT * FROM oauth_grants WHERE id = @id;

-- name: GetOAuthGrantByToken :one
SELECT g.* FROM oauth_grants g
JOIN personal_access_tokens t ON t.oauth_grant_id = g.id
WHERE t.id = @token_id;

-- name: ListUserOAuthGrants :many
SELECT * FROM oauth_grants
WHERE user_id = @user_id AND revoked_at IS NULL
ORDER BY COALESCE(last_used_at, created_at) DESC, id DESC;

-- name: TouchOAuthGrant :exec
UPDATE oauth_grants SET last_used_at = now()
WHERE id = @id AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: RevokeOAuthGrant :one
UPDATE oauth_grants SET revoked_at = now()
WHERE id = @id AND revoked_at IS NULL
RETURNING *;

-- name: RevokeUserOAuthGrants :many
UPDATE oauth_grants SET revoked_at = now()
WHERE user_id = @user_id AND revoked_at IS NULL
RETURNING id;

-- name: RevokeClientOAuthGrants :many
UPDATE oauth_grants SET revoked_at = now()
WHERE client_id = @client_id AND revoked_at IS NULL
RETURNING id;

-- name: DeleteOAuthGrantTokens :execrows
DELETE FROM personal_access_tokens WHERE oauth_grant_id = ANY(@grant_ids::bigint[]);

-- name: DeleteOAuthGrantRefreshTokens :execrows
DELETE FROM oauth_refresh_tokens WHERE grant_id = ANY(@grant_ids::bigint[]);

-- ── Codes ──────────────────────────────────────────────────────────

-- name: CreateOAuthCode :exec
INSERT INTO oauth_codes (code_hash, grant_id, redirect_uri, code_challenge, resource, expires_at)
VALUES (@code_hash, @grant_id, @redirect_uri, @code_challenge, @resource, @expires_at);

-- name: UseOAuthCode :one
-- Marks the code used and returns it with the time it was first used: a code
-- seen twice is answered by revoking its grant.
WITH previous AS (
    SELECT used_at AS first_used_at FROM oauth_codes WHERE code_hash = @code_hash
)
UPDATE oauth_codes c
SET used_at = COALESCE(c.used_at, now())
FROM previous
WHERE c.code_hash = @code_hash
RETURNING c.*, previous.first_used_at;

-- name: DeleteExpiredOAuthCodes :execrows
DELETE FROM oauth_codes WHERE expires_at < now() - interval '1 hour';

-- ── Refresh tokens ─────────────────────────────────────────────────

-- name: CreateOAuthRefreshToken :one
INSERT INTO oauth_refresh_tokens (token_hash, grant_id, expires_at)
VALUES (@token_hash, @grant_id, @expires_at)
RETURNING *;

-- name: RotateOAuthRefreshToken :one
-- Marks the token rotated and returns it with the time it was first rotated:
-- a rotated token presented again is reuse.
WITH previous AS (
    SELECT rotated_at AS first_rotated_at FROM oauth_refresh_tokens WHERE token_hash = @token_hash
)
UPDATE oauth_refresh_tokens r
SET rotated_at = COALESCE(r.rotated_at, now())
FROM previous
WHERE r.token_hash = @token_hash
RETURNING r.*, previous.first_rotated_at;

-- name: DeleteExpiredOAuthRefreshTokens :execrows
DELETE FROM oauth_refresh_tokens
WHERE expires_at < now() OR rotated_at < now() - interval '1 day';

-- ── Access tokens ──────────────────────────────────────────────────

-- name: CreateOAuthAccessToken :one
INSERT INTO personal_access_tokens (tokenable_type, tokenable_id, name, device, token, abilities, expires_at, oauth_grant_id)
VALUES ('user', @user_id, 'mcp', @device, @token, @abilities, @expires_at, @grant_id)
RETURNING *;

-- ── Consent page ───────────────────────────────────────────────────

-- name: ListOAuthConsentTenants :many
-- The tenants a platform account can pick on the consent page.
SELECT id, name, code FROM tenants
WHERE deleted_at IS NULL AND is_active
ORDER BY name, id
LIMIT 500;

-- name: ListOAuthGrantsForPrivacy :many
SELECT id, client_id, client_name, redirect_uri, scopes, created_at, last_used_at, revoked_at FROM oauth_grants
WHERE user_id = @user_id
ORDER BY id;

-- name: DeleteOAuthGrantsForUser :execrows
-- Codes, refresh tokens and MCP access tokens go with the grants (cascade).
DELETE FROM oauth_grants WHERE user_id = @user_id;
