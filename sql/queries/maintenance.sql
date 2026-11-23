-- name: GetMaintenance :one
-- The maintenance row; no row means the app is up.
SELECT id, down_since, message, retry_after, secret_hash, by_user_id, updated_at
FROM maintenance
WHERE id = 1;

-- name: SetMaintenanceDown :one
-- Takes the app down (or updates the message/retry/secret while down; the
-- original down_since is kept).
INSERT INTO maintenance (id, down_since, message, retry_after, secret_hash, by_user_id, updated_at)
VALUES (1, now(), @message::text, @retry_after::integer, sqlc.narg('secret_hash')::text, sqlc.narg('by_user_id')::bigint, now())
ON CONFLICT (id) DO UPDATE
SET down_since  = coalesce(maintenance.down_since, now()),
    message     = EXCLUDED.message,
    retry_after = EXCLUDED.retry_after,
    secret_hash = EXCLUDED.secret_hash,
    by_user_id  = EXCLUDED.by_user_id,
    updated_at  = now()
RETURNING id, down_since, message, retry_after, secret_hash, by_user_id, updated_at;

-- name: SetMaintenanceUp :execrows
-- Brings the app back up. Returns 0 when it was not down.
UPDATE maintenance
SET down_since  = NULL,
    secret_hash = NULL,
    by_user_id  = sqlc.narg('by_user_id')::bigint,
    updated_at  = now()
WHERE id = 1 AND down_since IS NOT NULL;
