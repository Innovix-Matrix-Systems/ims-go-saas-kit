-- name: CreateToken :one
INSERT INTO personal_access_tokens (tokenable_type, tokenable_id, name, device, token, abilities, expires_at, ip, user_agent)
VALUES (@tokenable_type, @tokenable_id, @name, @device, @token, @abilities, @expires_at, sqlc.narg('ip'), sqlc.narg('user_agent'))
RETURNING *;

-- name: GetToken :one
SELECT * FROM personal_access_tokens
WHERE id = @id;

-- name: TouchToken :execrows
UPDATE personal_access_tokens
SET last_used_at = @last_used_at, updated_at = now()
WHERE id = @id;

-- name: DeleteTokenByDevice :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = @tokenable_type
  AND tokenable_id = @tokenable_id
  AND device = @device;

-- name: DeleteTokensForUser :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = @tokenable_type
  AND tokenable_id = @tokenable_id;

-- name: SessionTokenStats :one
WITH tokens AS (
    SELECT
        tokenable_id,
        t.last_used_at,
        t.created_at,
        (t.expires_at IS NULL OR t.expires_at > now()) AS active
    FROM personal_access_tokens t
    JOIN users u ON u.id = t.tokenable_id AND u.deleted_at IS NULL
    WHERE t.tokenable_type = 'user'
)
SELECT
    count(DISTINCT tokenable_id) FILTER (WHERE active) AS users,
    count(*) FILTER (WHERE active) AS active_tokens,
    count(*) FILTER (WHERE active AND last_used_at >= now() - interval '24 hours') AS used_24h,
    count(*) FILTER (WHERE active AND (last_used_at IS NULL OR last_used_at < now() - interval '24 hours')) AS idle_tokens,
    count(*) FILTER (WHERE active AND COALESCE(last_used_at, created_at) < now() - interval '30 days') AS inactive_30d
FROM tokens;

-- name: ListSessionUsers :many
SELECT
    u.id AS user_id,
    u.name,
    u.email,
    u.tenant_id,
    count(t.id) FILTER (WHERE t.expires_at IS NULL OR t.expires_at > now()) AS active_tokens,
    count(t.id) AS total_tokens,
    max(t.last_used_at) FILTER (WHERE t.expires_at IS NULL OR t.expires_at > now())::timestamptz AS last_used_at
FROM users u
JOIN personal_access_tokens t
    ON t.tokenable_type = 'user' AND t.tokenable_id = u.id
WHERE u.deleted_at IS NULL
  AND (@before_id::bigint = 0 OR u.id < @before_id::bigint)
  AND (
    sqlc.narg('search')::text IS NULL
    OR u.name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR u.email ILIKE '%' || sqlc.narg('search')::text || '%'
  )
GROUP BY u.id
HAVING count(t.id) FILTER (WHERE t.expires_at IS NULL OR t.expires_at > now()) > 0
ORDER BY u.id DESC
LIMIT @row_limit;

-- name: SessionUserExists :one
SELECT EXISTS (
    SELECT 1 FROM users WHERE id = @id AND deleted_at IS NULL
);

-- name: ListSessionTokens :many
SELECT * FROM personal_access_tokens
WHERE tokenable_type = 'user'
  AND tokenable_id = @user_id
ORDER BY last_used_at DESC NULLS LAST, id DESC;

-- name: DeleteSessionToken :execrows
DELETE FROM personal_access_tokens
WHERE id = @id AND tokenable_type = 'user';

-- name: DeleteSessionUserTokens :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = 'user'
  AND tokenable_id = @user_id;

-- name: DeleteAllSessionTokensExcept :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = 'user'
  AND id <> @keep_id;

-- name: CountPrunableSessionTokens :one
SELECT count(*) FROM personal_access_tokens
WHERE tokenable_type = 'user'
  AND (
    (expires_at IS NOT NULL AND expires_at < now())
    OR COALESCE(last_used_at, created_at) < now() - (sqlc.arg(idle_days)::int * interval '1 day')
  );

-- name: DeletePrunableSessionTokens :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = 'user'
  AND (
    (expires_at IS NOT NULL AND expires_at < now())
    OR COALESCE(last_used_at, created_at) < now() - (sqlc.arg(idle_days)::int * interval '1 day')
  );

-- name: DeleteOtherTokensForUser :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = @tokenable_type
  AND tokenable_id = @tokenable_id
  AND id <> @keep_id;

-- name: ListUserSessions :many
-- The user's own sign-ins (session tokens; MCP connections are listed with
-- the user's connections), newest activity first.
SELECT * FROM personal_access_tokens
WHERE tokenable_type = 'user'
  AND tokenable_id = @user_id
  AND oauth_grant_id IS NULL
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY last_used_at DESC NULLS LAST, id DESC;

-- name: DeleteUserSession :one
DELETE FROM personal_access_tokens
WHERE id = @id
  AND tokenable_type = 'user'
  AND tokenable_id = @user_id
  AND oauth_grant_id IS NULL
RETURNING device;

-- name: DeleteExpiredTokens :execrows
DELETE FROM personal_access_tokens
WHERE expires_at IS NOT NULL AND expires_at < now();

-- name: DeleteIdleSessionTokens :execrows
DELETE FROM personal_access_tokens
WHERE tokenable_type = 'user'
  AND oauth_grant_id IS NULL
  AND COALESCE(last_used_at, created_at) < now() - (sqlc.arg(idle_days)::int * interval '1 day');

-- name: ListUserTokensForPrivacy :many
SELECT id, name, device, ip, user_agent, last_used_at, expires_at, created_at FROM personal_access_tokens
WHERE tokenable_type = 'user' AND tokenable_id = @user_id
ORDER BY id;
