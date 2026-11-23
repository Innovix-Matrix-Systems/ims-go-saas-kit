-- +goose Up
-- Phase 11: settings become polymorphic-target + grouped, and feature flags get
-- their own table. The phase-3 tenant_id column is migrated to target_type/
-- target_id so branch/user targets can be enabled later without a migration.

CREATE TABLE settings_groups (
    id          bigserial PRIMARY KEY,
    slug        text NOT NULL,
    name        text NOT NULL,
    description text,
    sort_order  integer NOT NULL DEFAULT 0,
    is_system   boolean NOT NULL DEFAULT false,
    created_by  bigint,
    updated_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX settings_groups_slug_unique ON settings_groups (slug);

ALTER TABLE settings
    ADD COLUMN target_type text,
    ADD COLUMN target_id   bigint,
    ADD COLUMN group_id    bigint REFERENCES settings_groups (id) ON DELETE SET NULL;

-- Backfill the phase-3 rows: tenant overrides become target_type = 'tenant'.
UPDATE settings SET target_type = 'tenant', target_id = tenant_id WHERE tenant_id IS NOT NULL;

-- Existing indexes reference tenant_id; replace them with target-aware ones.
DROP INDEX settings_global_key_unique;
DROP INDEX settings_tenant_key_unique;
ALTER TABLE settings DROP COLUMN tenant_id;

CREATE UNIQUE INDEX settings_global_key_unique ON settings (key)
    WHERE target_type IS NULL;
CREATE UNIQUE INDEX settings_target_key_unique ON settings (target_type, target_id, key)
    WHERE target_type IS NOT NULL;

CREATE TABLE feature_flags (
    id          bigserial PRIMARY KEY,
    key         text NOT NULL,
    target_type text,
    target_id   bigint,
    value       boolean NOT NULL,
    created_by  bigint,
    updated_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX feature_flags_global_key_unique ON feature_flags (key)
    WHERE target_type IS NULL;
CREATE UNIQUE INDEX feature_flags_target_key_unique ON feature_flags (target_type, target_id, key)
    WHERE target_type IS NOT NULL;

-- +goose Down
DROP TABLE feature_flags;

ALTER TABLE settings ADD COLUMN tenant_id bigint REFERENCES tenants (id) ON DELETE CASCADE;
UPDATE settings SET tenant_id = target_id WHERE target_type = 'tenant';
-- Only global + tenant targets existed before this phase; drop anything else.
DELETE FROM settings WHERE target_type IS NOT NULL AND target_type <> 'tenant';

DROP INDEX settings_global_key_unique;
DROP INDEX settings_target_key_unique;
ALTER TABLE settings DROP COLUMN target_type;
ALTER TABLE settings DROP COLUMN target_id;
ALTER TABLE settings DROP COLUMN group_id;

CREATE UNIQUE INDEX settings_global_key_unique ON settings (key) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX settings_tenant_key_unique ON settings (tenant_id, key) WHERE tenant_id IS NOT NULL;

DROP TABLE settings_groups;
