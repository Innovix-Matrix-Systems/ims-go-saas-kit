-- Settings are stored against a polymorphic target: global rows have
-- target_type/target_id NULL, tenant rows use target_type = 'tenant'.

-- name: GetSetting :one
SELECT * FROM settings
WHERE key = @key
  AND target_type IS NOT DISTINCT FROM sqlc.narg('target_type')::text
  AND target_id IS NOT DISTINCT FROM sqlc.narg('target_id')::bigint;

-- name: ListGlobalSettings :many
SELECT * FROM settings
WHERE target_type IS NULL AND target_id IS NULL
ORDER BY key;

-- name: ListSettingsByTarget :many
SELECT * FROM settings
WHERE target_type = @target_type AND target_id = @target_id
ORDER BY key;

-- name: InsertGlobalSettingIfAbsent :one
INSERT INTO settings (key, target_type, target_id, group_id, value)
VALUES (@key, NULL, NULL, @group_id, @value)
ON CONFLICT (key) WHERE target_type IS NULL DO NOTHING
RETURNING *;

-- name: UpsertGlobalSetting :one
INSERT INTO settings (key, target_type, target_id, value)
VALUES (@key, NULL, NULL, @value)
ON CONFLICT (key) WHERE target_type IS NULL DO UPDATE
SET value = EXCLUDED.value, updated_at = now()
RETURNING *;

-- name: UpsertTargetSetting :one
INSERT INTO settings (key, target_type, target_id, value)
VALUES (@key, @target_type, @target_id, @value)
ON CONFLICT (target_type, target_id, key) WHERE target_type IS NOT NULL DO UPDATE
SET value = EXCLUDED.value, updated_at = now()
RETURNING *;

-- name: SetSettingGroupIfNull :one
UPDATE settings
SET group_id = @group_id, updated_at = now()
WHERE key = @key AND target_type IS NULL AND group_id IS NULL
RETURNING *;

-- name: SetSettingGroup :one
UPDATE settings
SET group_id = @group_id, updated_at = now()
WHERE key = @key AND target_type IS NULL AND target_id IS NULL
RETURNING *;

-- name: DeleteSetting :execrows
DELETE FROM settings
WHERE key = @key
  AND target_type IS NOT DISTINCT FROM sqlc.narg('target_type')::text
  AND target_id IS NOT DISTINCT FROM sqlc.narg('target_id')::bigint;

-- name: PlaceSetting :execrows
-- Moves a key into a group at a position (global row only).
UPDATE settings
SET group_id = sqlc.narg('group_id'), sort_order = @sort_order, updated_at = now()
WHERE key = @key AND target_type IS NULL AND target_id IS NULL;

-- name: DeleteSettingsByTarget :execrows
DELETE FROM settings WHERE target_type = @target_type AND target_id = @target_id;
