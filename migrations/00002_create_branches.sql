-- +goose Up
CREATE TABLE branches (
    id          bigserial PRIMARY KEY,
    tenant_id   bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name        text NOT NULL,
    address     text,
    description text,
    phone       text,
    email       text,
    manager_id  bigint,
    created_by  bigint,
    updated_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

CREATE INDEX branches_tenant_id_idx ON branches (tenant_id);
CREATE UNIQUE INDEX branches_tenant_name_unique ON branches (tenant_id, name) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE branches;
