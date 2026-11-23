-- +goose Up
CREATE TABLE notifications (
    id         bigserial PRIMARY KEY,
    tenant_id  bigint REFERENCES tenants (id),
    event_type text NOT NULL,
    priority   text NOT NULL DEFAULT 'INFO',
    title      text NOT NULL,
    body       text,
    data       jsonb,
    group_key  text,
    created_by bigint,
    updated_by bigint,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_tenant_idx ON notifications (tenant_id);
CREATE INDEX notifications_created_idx ON notifications (created_at);
CREATE INDEX notifications_tenant_created_idx ON notifications (tenant_id, created_at);
CREATE INDEX notifications_tenant_expires_idx ON notifications (tenant_id, expires_at);

CREATE TABLE notification_targets (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint REFERENCES tenants (id),
    notification_id bigint NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    target_type     text NOT NULL,
    target_id       bigint,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notification_targets_tenant_idx ON notification_targets (tenant_id);
CREATE INDEX notification_targets_notification_idx ON notification_targets (notification_id);
CREATE INDEX notification_targets_lookup_idx ON notification_targets (tenant_id, target_type, target_id);
CREATE INDEX notification_targets_type_idx ON notification_targets (target_type, target_id);

CREATE TABLE notification_reads (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint REFERENCES tenants (id),
    branch_id       bigint REFERENCES branches (id),
    notification_id bigint NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    user_id         bigint NOT NULL REFERENCES users (id),
    read_at         timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX notification_reads_unique ON notification_reads (notification_id, user_id);
CREATE INDEX notification_reads_tenant_user_idx ON notification_reads (tenant_id, user_id);
CREATE INDEX notification_reads_branch_user_idx ON notification_reads (tenant_id, branch_id, user_id);
CREATE INDEX notification_reads_user_idx ON notification_reads (user_id, notification_id);

CREATE TABLE notification_deliveries (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint REFERENCES tenants (id),
    notification_id bigint NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    user_id         bigint NOT NULL REFERENCES users (id),
    status          text NOT NULL DEFAULT 'pending',
    delivered_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX notification_deliveries_unique ON notification_deliveries (notification_id, user_id);
CREATE INDEX notification_deliveries_tenant_status_idx ON notification_deliveries (tenant_id, status);
CREATE INDEX notification_deliveries_notification_status_idx ON notification_deliveries (notification_id, status);
CREATE INDEX notification_deliveries_user_status_idx ON notification_deliveries (user_id, status);

-- +goose Down
DROP TABLE notification_deliveries;
DROP TABLE notification_reads;
DROP TABLE notification_targets;
DROP TABLE notifications;
