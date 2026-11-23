-- +goose Up
CREATE TABLE users (
    id                 bigserial PRIMARY KEY,
    tenant_id          bigint REFERENCES tenants (id) ON DELETE CASCADE,
    branch_id          bigint REFERENCES branches (id) ON DELETE SET NULL,
    active_branch_id   bigint REFERENCES branches (id) ON DELETE SET NULL,
    first_name         text,
    last_name          text,
    user_name          text,
    name               text NOT NULL,
    email              text NOT NULL,
    email_verified_at  timestamptz,
    phone              text,
    phone_verified_at  timestamptz,
    password           text NOT NULL,
    is_active          smallint NOT NULL DEFAULT 1,
    last_login_at      timestamptz,
    last_active_device text,
    remember_token     text,
    created_by         bigint,
    updated_by         bigint,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz
);

CREATE INDEX users_tenant_id_idx ON users (tenant_id);
CREATE INDEX users_branch_id_idx ON users (branch_id);
CREATE UNIQUE INDEX users_email_unique ON users (lower(email)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_phone_unique ON users (phone) WHERE deleted_at IS NULL AND phone IS NOT NULL;

-- +goose Down
DROP TABLE users;
