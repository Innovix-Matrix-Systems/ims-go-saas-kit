-- Feature flags are separate from settings: global rows have
-- target_type/target_id NULL; tenant rows use 'tenant'; user rows use 'user'.

-- name: GetFeatureFlag :one
SELECT * FROM feature_flags
WHERE key = @key
  AND target_type IS NOT DISTINCT FROM sqlc.narg('target_type')::text
  AND target_id IS NOT DISTINCT FROM sqlc.narg('target_id')::bigint;

-- name: ListGlobalFeatureFlags :many
SELECT * FROM feature_flags
WHERE target_type IS NULL AND target_id IS NULL
ORDER BY key;

-- name: ListFeatureFlagsByTarget :many
SELECT * FROM feature_flags
WHERE target_type = @target_type AND target_id = @target_id
ORDER BY key;

-- name: InsertGlobalFeatureFlagIfAbsent :one
INSERT INTO feature_flags (key, target_type, target_id, value)
VALUES (@key, NULL, NULL, @value)
ON CONFLICT (key) WHERE target_type IS NULL DO NOTHING
RETURNING *;

-- name: UpsertGlobalFeatureFlag :one
INSERT INTO feature_flags (key, target_type, target_id, value, rollout, created_by, updated_by)
VALUES (@key, NULL, NULL, @value, sqlc.narg('rollout')::smallint, @actor_id, @actor_id)
ON CONFLICT (key) WHERE target_type IS NULL DO UPDATE
SET value = EXCLUDED.value, rollout = EXCLUDED.rollout, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: UpsertTargetFeatureFlag :one
INSERT INTO feature_flags (key, target_type, target_id, value, rollout, created_by, updated_by)
VALUES (@key, @target_type, @target_id, @value, sqlc.narg('rollout')::smallint, @actor_id, @actor_id)
ON CONFLICT (target_type, target_id, key) WHERE target_type IS NOT NULL DO UPDATE
SET value = EXCLUDED.value, rollout = EXCLUDED.rollout, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: DeleteFeatureFlag :execrows
DELETE FROM feature_flags
WHERE key = @key
  AND target_type IS NOT DISTINCT FROM sqlc.narg('target_type')::text
  AND target_id IS NOT DISTINCT FROM sqlc.narg('target_id')::bigint;

-- name: DeleteFeatureFlagsByTarget :execrows
DELETE FROM feature_flags WHERE target_type = @target_type AND target_id = @target_id;
