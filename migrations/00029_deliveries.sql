-- +goose Up
-- Delivery log (phase 26.2): one row per message per channel (mail, sms,
-- push). It never stores a body or template data, so one-time codes never
-- reach the database; the address is masked.
CREATE TABLE deliveries (
    id                  bigserial PRIMARY KEY,
    tenant_id           bigint REFERENCES tenants (id) ON DELETE CASCADE,
    user_id             bigint REFERENCES users (id) ON DELETE SET NULL,
    channel             text NOT NULL,
    address             text NOT NULL DEFAULT '',
    template            text NOT NULL DEFAULT '',
    status              text NOT NULL DEFAULT 'queued',
    attempts            integer NOT NULL DEFAULT 0,
    error               text,
    provider_message_id text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    sent_at             timestamptz
);

CREATE INDEX deliveries_created_idx ON deliveries (created_at);
CREATE INDEX deliveries_tenant_created_idx ON deliveries (tenant_id, created_at);
CREATE INDEX deliveries_user_idx ON deliveries (user_id);

-- +goose Down
DROP TABLE IF EXISTS deliveries;
