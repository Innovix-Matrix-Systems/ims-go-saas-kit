-- +goose Up
CREATE TABLE roles (
    id          bigserial PRIMARY KEY,
    tenant_id   bigint REFERENCES tenants (id) ON DELETE CASCADE,
    name        text NOT NULL,
    guard_name  text NOT NULL DEFAULT 'sanctum',
    is_system   boolean NOT NULL DEFAULT false,
    description text,
    created_by  bigint,
    updated_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX roles_tenant_id_idx ON roles (tenant_id);
CREATE UNIQUE INDEX roles_system_name_unique ON roles (name, guard_name) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX roles_tenant_name_unique ON roles (tenant_id, name, guard_name) WHERE tenant_id IS NOT NULL;

CREATE TABLE permissions (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL,
    guard_name  text NOT NULL DEFAULT 'sanctum',
    "group"     text NOT NULL DEFAULT '',
    description text,
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX permissions_name_guard_unique ON permissions (name, guard_name);

CREATE TABLE model_has_roles (
    role_id    bigint NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    model_type text NOT NULL,
    model_id   bigint NOT NULL,
    PRIMARY KEY (role_id, model_type, model_id)
);

CREATE INDEX model_has_roles_model_idx ON model_has_roles (model_type, model_id);

CREATE TABLE role_has_permissions (
    role_id       bigint NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission_id bigint NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- +goose Down
DROP TABLE role_has_permissions;
DROP TABLE model_has_roles;
DROP TABLE permissions;
DROP TABLE roles;
