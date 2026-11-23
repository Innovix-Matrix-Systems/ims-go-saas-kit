-- name: CreateDelivery :one
INSERT INTO deliveries (tenant_id, user_id, channel, address, template, status, error)
VALUES (sqlc.narg('tenant_id'), sqlc.narg('user_id'), @channel, @address, @template, @status, sqlc.narg('error'))
RETURNING *;

-- name: MarkDeliverySent :exec
UPDATE deliveries
SET status              = 'sent',
    attempts            = attempts + 1,
    error               = NULL,
    provider_message_id = sqlc.narg('provider_message_id'),
    sent_at             = now(),
    updated_at          = now()
WHERE id = @id;

-- name: MarkDeliveryFailed :exec
UPDATE deliveries
SET status     = 'failed',
    attempts   = attempts + 1,
    error      = @error,
    updated_at = now()
WHERE id = @id;

-- name: GetDelivery :one
SELECT * FROM deliveries WHERE id = @id;

-- name: ListDeliveries :many
SELECT * FROM deliveries
WHERE (@channel::text = '' OR channel = @channel::text)
  AND (@status::text = '' OR status = @status::text)
  AND (@template::text = '' OR template = @template::text)
  AND (sqlc.narg('user_id')::bigint IS NULL OR user_id = sqlc.narg('user_id')::bigint)
  AND (sqlc.narg('before_id')::bigint IS NULL OR id < sqlc.narg('before_id')::bigint)
ORDER BY id DESC
LIMIT @row_limit;

-- name: PruneDeliveries :execrows
DELETE FROM deliveries WHERE created_at < @before;

-- name: ListDeliveriesForPrivacy :many
SELECT id, channel, template, status, created_at, sent_at FROM deliveries
WHERE user_id = @user_id
ORDER BY id;

-- name: AnonymizeDeliveriesForUser :execrows
UPDATE deliveries SET address = '', user_id = NULL, updated_at = now()
WHERE user_id = @user_id;
