-- +goose Up
CREATE TABLE tenants (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL,
    code       text NOT NULL,
    slug       text,
    domain     text,
    phone      text,
    email      text,
    is_active  boolean NOT NULL DEFAULT true,
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE UNIQUE INDEX tenants_code_unique ON tenants (code) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX tenants_slug_unique ON tenants (slug) WHERE deleted_at IS NULL AND slug IS NOT NULL;
CREATE UNIQUE INDEX tenants_domain_unique ON tenants (domain) WHERE deleted_at IS NULL AND domain IS NOT NULL;

-- +goose Down
DROP TABLE tenants;
