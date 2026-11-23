-- name: CreateWebhookEndpoint :one
INSERT INTO webhook_endpoints (tenant_id, url, description, events, secret, created_by, updated_by)
VALUES (@tenant_id, @url, @description, @events::text[], @secret, @actor_id, @actor_id)
RETURNING *;

-- name: GetWebhookEndpoint :one
SELECT * FROM webhook_endpoints
WHERE id = @id AND (@all_tenants::boolean OR tenant_id = @tenant_id);

-- name: ListWebhookEndpoints :many
SELECT * FROM webhook_endpoints
WHERE (@all_tenants::boolean OR tenant_id = @tenant_id)
ORDER BY id DESC;

-- name: UpdateWebhookEndpoint :one
-- Re-activating clears the failure count and the reason it was disabled.
UPDATE webhook_endpoints
SET url                  = @url,
    description          = @description,
    events               = @events::text[],
    is_active            = @is_active,
    consecutive_failures = CASE WHEN @is_active AND NOT is_active THEN 0 ELSE consecutive_failures END,
    disabled_reason      = CASE WHEN @is_active THEN NULL ELSE disabled_reason END,
    updated_by           = @actor_id,
    updated_at           = now()
WHERE id = @id AND (@all_tenants::boolean OR tenant_id = @tenant_id)
RETURNING *;

-- name: RotateWebhookSecret :one
UPDATE webhook_endpoints
SET previous_secret            = secret,
    previous_secret_expires_at = @previous_expires_at,
    secret                     = @secret,
    updated_by                 = @actor_id,
    updated_at                 = now()
WHERE id = @id AND (@all_tenants::boolean OR tenant_id = @tenant_id)
RETURNING *;

-- name: DeleteWebhookEndpoint :execrows
DELETE FROM webhook_endpoints
WHERE id = @id AND (@all_tenants::boolean OR tenant_id = @tenant_id);

-- name: ListSubscribedWebhookEndpoints :many
-- Active endpoints of a tenant that want an event ('*' = every event).
SELECT * FROM webhook_endpoints
WHERE tenant_id = @tenant_id AND is_active
  AND (@event_type::text = ANY(events) OR '*' = ANY(events));

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (tenant_id, endpoint_id, event_id, event_type, payload)
VALUES (@tenant_id, @endpoint_id, @event_id, @event_type, @payload)
RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries
WHERE id = @id AND (@all_tenants::boolean OR tenant_id = @tenant_id);

-- name: ListWebhookDeliveries :many
SELECT * FROM webhook_deliveries
WHERE endpoint_id = @endpoint_id
  AND (@all_tenants::boolean OR tenant_id = @tenant_id)
  AND (@status::text = '' OR status = @status::text)
  AND (sqlc.narg('before_id')::bigint IS NULL OR id < sqlc.narg('before_id')::bigint)
ORDER BY id DESC
LIMIT @row_limit;

-- name: RecordWebhookAttempt :one
UPDATE webhook_deliveries
SET status          = @status,
    attempts        = attempts + 1,
    response_status = sqlc.narg('response_status'),
    response_body   = sqlc.narg('response_body'),
    duration_ms     = @duration_ms,
    error           = sqlc.narg('error'),
    next_attempt_at = sqlc.narg('next_attempt_at'),
    delivered_at    = CASE WHEN @status = 'succeeded' THEN now() ELSE delivered_at END
WHERE id = @id
RETURNING *;

-- name: ResetWebhookFailures :exec
UPDATE webhook_endpoints SET consecutive_failures = 0 WHERE id = @id AND consecutive_failures <> 0;

-- name: CountWebhookFailure :one
-- One more abandoned delivery; the endpoint is disabled at @disable_after.
UPDATE webhook_endpoints
SET consecutive_failures = consecutive_failures + 1,
    is_active            = CASE WHEN consecutive_failures + 1 >= @disable_after THEN false ELSE is_active END,
    disabled_reason      = CASE WHEN consecutive_failures + 1 >= @disable_after
                                THEN @reason::text ELSE disabled_reason END,
    updated_at           = now()
WHERE id = @id
RETURNING *;

-- name: PruneWebhookDeliveries :execrows
DELETE FROM webhook_deliveries WHERE created_at < @before;

-- name: ScrubWebhookPayloadsAboutUser :execrows
-- Stored payloads copy the activity entry's changes; keep the event, drop the values.
UPDATE webhook_deliveries
SET payload = jsonb_set(payload, '{data,changes}', '{"erased": true}'::jsonb)
WHERE payload->'data'->>'subject' = 'user'
  AND payload->'data'->>'subject_id' = CAST(@user_id AS bigint)::text;
