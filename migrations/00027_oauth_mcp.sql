-- +goose Up
-- The OAuth 2.1 authorization server behind the MCP connector.

-- Clients an admin registered by hand. Clients that identify themselves with a
-- Client ID Metadata Document (an https client_id) are fetched and cached,
-- never stored here.
CREATE TABLE oauth_clients (
    id                         bigserial PRIMARY KEY,
    client_id                  text NOT NULL,
    name                       text NOT NULL,
    type                       text NOT NULL CHECK (type IN ('public', 'confidential')),
    secret_hash                char(64),
    previous_secret_hash       char(64),
    previous_secret_expires_at timestamptz,
    redirect_uris              text[] NOT NULL,
    scopes                     text[] NOT NULL,
    tenant_id                  bigint REFERENCES tenants (id) ON DELETE CASCADE,
    is_active                  boolean NOT NULL DEFAULT true,
    created_by                 bigint REFERENCES users (id) ON DELETE SET NULL,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    CHECK ((type = 'confidential') = (secret_hash IS NOT NULL))
);

CREATE UNIQUE INDEX oauth_clients_client_id_unique ON oauth_clients (client_id);

-- One connection: a user allowed a client, with these scopes, in this tenant.
CREATE TABLE oauth_grants (
    id           bigserial PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id    text NOT NULL,
    client_name  text NOT NULL,
    redirect_uri text NOT NULL,
    scopes       text[] NOT NULL,
    tenant_id    bigint REFERENCES tenants (id) ON DELETE CASCADE,
    branch_id    bigint REFERENCES branches (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX oauth_grants_user_idx ON oauth_grants (user_id) WHERE revoked_at IS NULL;
CREATE INDEX oauth_grants_client_idx ON oauth_grants (client_id) WHERE revoked_at IS NULL;

-- Authorization codes: single use, a minute long.
CREATE TABLE oauth_codes (
    code_hash      char(64) PRIMARY KEY,
    grant_id       bigint NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    resource       text NOT NULL,
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_codes_expires_idx ON oauth_codes (expires_at);

-- Refresh tokens, rotated on every use. A rotated one used again revokes the
-- grant (reuse detection).
CREATE TABLE oauth_refresh_tokens (
    id         bigserial PRIMARY KEY,
    token_hash char(64) NOT NULL,
    grant_id   bigint NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    rotated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX oauth_refresh_tokens_hash_unique ON oauth_refresh_tokens (token_hash);
CREATE INDEX oauth_refresh_tokens_grant_idx ON oauth_refresh_tokens (grant_id);

-- MCP access tokens are personal access tokens tied to their grant.
ALTER TABLE personal_access_tokens
    ADD COLUMN oauth_grant_id bigint REFERENCES oauth_grants (id) ON DELETE CASCADE;

CREATE INDEX personal_access_tokens_oauth_grant_idx ON personal_access_tokens (oauth_grant_id)
    WHERE oauth_grant_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS personal_access_tokens_oauth_grant_idx;
ALTER TABLE personal_access_tokens DROP COLUMN IF EXISTS oauth_grant_id;
DROP TABLE IF EXISTS oauth_refresh_tokens;
DROP TABLE IF EXISTS oauth_codes;
DROP TABLE IF EXISTS oauth_grants;
DROP TABLE IF EXISTS oauth_clients;
