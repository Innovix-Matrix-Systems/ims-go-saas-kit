-- name: CacheGet :one
SELECT value, expiration FROM cache
WHERE key = @key;

-- name: CachePut :exec
INSERT INTO cache (key, value, expiration)
VALUES (@key, @value, @expiration)
ON CONFLICT (key) DO UPDATE
SET value      = EXCLUDED.value,
    expiration = EXCLUDED.expiration,
    updated_at = now();

-- name: CachePutIfAbsent :execrows
INSERT INTO cache (key, value, expiration)
VALUES (@key, @value, @expiration)
ON CONFLICT (key) DO UPDATE
SET value      = EXCLUDED.value,
    expiration = EXCLUDED.expiration,
    updated_at = now()
WHERE cache.expiration IS NOT NULL AND cache.expiration <= now();

-- name: CacheDelete :execrows
DELETE FROM cache WHERE key = @key;

-- name: CacheDeleteExpired :execrows
DELETE FROM cache
WHERE expiration IS NOT NULL AND expiration <= now();

-- name: CacheClearPrefix :execrows
DELETE FROM cache WHERE key LIKE @prefix::text;

-- name: CacheIncrement :one
INSERT INTO cache (key, value, expiration)
VALUES (@key, @delta::text, NULL)
ON CONFLICT (key) DO UPDATE
SET value      = ((cache.value)::bigint + (EXCLUDED.value)::bigint)::text,
    updated_at = now()
RETURNING (value)::bigint;
