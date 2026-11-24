-- +goose Up
-- A tenant-scoped record of who did what (like
-- spatie/laravel-activitylog). tenant_id is NULL for platform-level actions.
CREATE TABLE activity_log (
    id           bigserial PRIMARY KEY,
    tenant_id    bigint REFERENCES tenants (id) ON DELETE CASCADE,
    actor_id     bigint REFERENCES users (id) ON DELETE SET NULL,
    action       text NOT NULL,
    subject_type text NOT NULL,
    subject_id   bigint,
    changes      jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip           text,
    request_id   text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX activity_log_tenant_feed_idx ON activity_log (tenant_id, created_at DESC, id DESC);
CREATE INDEX activity_log_subject_idx ON activity_log (subject_type, subject_id);
CREATE INDEX activity_log_created_idx ON activity_log (created_at);

-- +goose Down
DROP TABLE IF EXISTS activity_log;
