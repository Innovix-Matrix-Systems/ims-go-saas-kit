-- +goose Up

-- One row per organization that went through sign-up (or was created by staff
-- for an owner): who owns it and how far the first-run setup got.
CREATE TABLE tenant_onboarding (
    tenant_id     bigint PRIMARY KEY REFERENCES tenants (id) ON DELETE CASCADE,
    owner_user_id bigint REFERENCES users (id) ON DELETE SET NULL,
    source        text        NOT NULL DEFAULT 'signup',
    completed_at  timestamptz,
    skipped_steps text[]      NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_onboarding_source_check CHECK (source IN ('signup', 'staff'))
);

CREATE INDEX tenant_onboarding_owner_idx ON tenant_onboarding (owner_user_id);

-- Which version of which document (terms, privacy policy) a user accepted, and
-- when: evidence of consent. The IP and browser are personal data and are
-- cleared when the user is erased; the acceptance itself stays.
CREATE TABLE user_agreements (
    id          bigserial PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    tenant_id   bigint      REFERENCES tenants (id) ON DELETE CASCADE,
    document    text        NOT NULL,
    version     text        NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT now(),
    ip_address  text,
    user_agent  text
);

CREATE INDEX user_agreements_user_idx ON user_agreements (user_id);

-- +goose Down
DROP TABLE user_agreements;
DROP TABLE tenant_onboarding;
