-- +goose Up
-- Outbound webhooks (phase 26.8): a tenant subscribes URLs to events from the
-- activity log; each event is POSTed, signed (Standard Webhooks), retried and
-- logged. Secrets are encrypted with APP_KEY; previous_secret keeps signing
-- for a day after a rotation.
CREATE TABLE webhook_endpoints (
    id                         bigserial PRIMARY KEY,
    tenant_id                  bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    url                        text NOT NULL,
    description                text NOT NULL DEFAULT '',
    events                     text[] NOT NULL,
    secret                     text NOT NULL,
    previous_secret            text,
    previous_secret_expires_at timestamptz,
    is_active                  boolean NOT NULL DEFAULT true,
    consecutive_failures       integer NOT NULL DEFAULT 0,
    disabled_reason            text,
    created_by                 bigint,
    updated_by                 bigint,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX webhook_endpoints_tenant_idx ON webhook_endpoints (tenant_id);

CREATE TABLE webhook_deliveries (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    endpoint_id     bigint NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event_id        text NOT NULL,
    event_type      text NOT NULL,
    payload         jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'pending',
    attempts        integer NOT NULL DEFAULT 0,
    response_status integer,
    response_body   text,
    duration_ms     integer,
    error           text,
    next_attempt_at timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    delivered_at    timestamptz
);

CREATE INDEX webhook_deliveries_endpoint_idx ON webhook_deliveries (endpoint_id, id DESC);
CREATE INDEX webhook_deliveries_created_idx ON webhook_deliveries (created_at);

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_endpoints;
