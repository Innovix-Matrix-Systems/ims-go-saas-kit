-- +goose Up
-- Account erasure and personal-data export (docs/design/account-deletion-and-export.md).
-- An erased user keeps its row as a tombstone: personal columns are blanked and
-- erased_at is set, so created_by-style references stay valid.
ALTER TABLE users ADD COLUMN erased_at timestamptz;

-- The ledger of erasure and export requests. It never stores personal data
-- (report holds counts only), so it outlives the erasure and drives the
-- replay after a backup restore.
CREATE TABLE privacy_requests (
    id                bigserial PRIMARY KEY,
    tenant_id         bigint REFERENCES tenants (id) ON DELETE CASCADE,
    user_id           bigint NOT NULL REFERENCES users (id),
    type              text NOT NULL CHECK (type IN ('erasure', 'export')),
    status            text NOT NULL CHECK (status IN ('scheduled', 'processing', 'completed', 'cancelled', 'failed')),
    source            text NOT NULL CHECK (source IN ('self', 'admin', 'cli', 'retention')),
    requested_by      bigint REFERENCES users (id) ON DELETE SET NULL,
    scheduled_for     timestamptz,
    processing_job_id bigint REFERENCES data_processing_jobs (id) ON DELETE SET NULL,
    report            jsonb NOT NULL DEFAULT '{}'::jsonb,
    error             text,
    completed_at      timestamptz,
    cancelled_at      timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX privacy_requests_open_erasure_unique ON privacy_requests (user_id)
    WHERE type = 'erasure' AND status IN ('scheduled', 'processing');
CREATE INDEX privacy_requests_due_idx ON privacy_requests (scheduled_for)
    WHERE type = 'erasure' AND status = 'scheduled';
CREATE INDEX privacy_requests_user_idx ON privacy_requests (user_id, created_at DESC);
CREATE INDEX privacy_requests_tenant_idx ON privacy_requests (tenant_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS privacy_requests;
ALTER TABLE users DROP COLUMN IF EXISTS erased_at;
