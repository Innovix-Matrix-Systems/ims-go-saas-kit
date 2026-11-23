-- +goose Up
CREATE TABLE settings (
    id         bigserial PRIMARY KEY,
    key        text NOT NULL,
    tenant_id  bigint REFERENCES tenants (id) ON DELETE CASCADE,
    value      jsonb NOT NULL DEFAULT 'null'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX settings_global_key_unique ON settings (key) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX settings_tenant_key_unique ON settings (tenant_id, key) WHERE tenant_id IS NOT NULL;

-- +goose Down
DROP TABLE settings;
