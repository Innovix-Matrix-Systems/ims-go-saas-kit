-- +goose Up
CREATE TABLE packages (
    id            bigserial PRIMARY KEY,
    name          text NOT NULL,
    description   text,
    price         bigint NOT NULL DEFAULT 0,
    billing_cycle text NOT NULL DEFAULT 'monthly',
    trial_days    integer NOT NULL DEFAULT 0,
    is_active     boolean NOT NULL DEFAULT true,
    sort_order    integer NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX packages_name_unique ON packages (lower(name));

CREATE TABLE package_features (
    id          bigserial PRIMARY KEY,
    package_id  bigint NOT NULL REFERENCES packages (id) ON DELETE CASCADE,
    feature_key text NOT NULL,
    is_enabled  boolean NOT NULL DEFAULT false,
    limit_value bigint NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX package_features_key_unique ON package_features (package_id, feature_key);

CREATE TABLE subscriptions (
    id                  bigserial PRIMARY KEY,
    tenant_id           bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    package_id          bigint NOT NULL REFERENCES packages (id) ON DELETE RESTRICT,
    status              text NOT NULL DEFAULT 'pending',
    starts_at           timestamptz,
    trial_ends_at       timestamptz,
    expires_at          timestamptz,
    cancelled_at        timestamptz,
    cancellation_reason text,
    overrides           jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_tenant_idx ON subscriptions (tenant_id);
CREATE INDEX subscriptions_status_idx ON subscriptions (status);
CREATE INDEX subscriptions_expires_idx ON subscriptions (expires_at) WHERE status IN ('trial', 'active');

CREATE TABLE payments (
    id               bigserial PRIMARY KEY,
    tenant_id        bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    subscription_id  bigint REFERENCES subscriptions (id) ON DELETE SET NULL,
    payment_method   text NOT NULL,
    status           text NOT NULL DEFAULT 'pending',
    amount           bigint NOT NULL,
    currency         text NOT NULL DEFAULT 'BDT',
    gateway_ref      text,
    gateway_status   text,
    gateway_response jsonb,
    invoice_number   text NOT NULL,
    paid_at          timestamptz,
    recorded_by      bigint,
    created_by       bigint,
    updated_by       bigint,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX payments_invoice_unique ON payments (invoice_number);
CREATE INDEX payments_tenant_idx ON payments (tenant_id);
CREATE INDEX payments_subscription_idx ON payments (subscription_id);

CREATE TABLE payment_approval_requests (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    subscription_id bigint NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    payment_id      bigint NOT NULL REFERENCES payments (id) ON DELETE CASCADE,
    payment_method  text NOT NULL,
    payment_data    jsonb,
    status          text NOT NULL DEFAULT 'pending',
    reviewer_id     bigint,
    reviewer_note   text,
    reviewed_at     timestamptz,
    created_by      bigint,
    updated_by      bigint,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX payment_approval_requests_tenant_idx ON payment_approval_requests (tenant_id);
CREATE INDEX payment_approval_requests_status_idx ON payment_approval_requests (status);

-- +goose Down
DROP TABLE payment_approval_requests;
DROP TABLE payments;
DROP TABLE subscriptions;
DROP TABLE package_features;
DROP TABLE packages;
