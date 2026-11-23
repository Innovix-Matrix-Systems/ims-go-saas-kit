-- +goose Up
CREATE TABLE data_processing_jobs (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint REFERENCES tenants (id) ON DELETE CASCADE,
    branch_id       bigint,
    job_id          text,
    type            text NOT NULL,
    status          text NOT NULL DEFAULT 'pending',
    entity_type     text,
    filters         jsonb,
    total_items     integer NOT NULL DEFAULT 0,
    processed_items integer NOT NULL DEFAULT 0,
    success_count   integer NOT NULL DEFAULT 0,
    error_count     integer NOT NULL DEFAULT 0,
    errors          jsonb,
    error_message   text,
    file_name       text,
    file_disk       text,
    file_path       text,
    file_size       bigint,
    file_mime       text,
    user_id         bigint,
    started_at      timestamptz,
    completed_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX data_processing_jobs_tenant_idx ON data_processing_jobs (tenant_id);
CREATE INDEX data_processing_jobs_status_idx ON data_processing_jobs (status);
CREATE INDEX data_processing_jobs_completed_idx ON data_processing_jobs (completed_at) WHERE completed_at IS NOT NULL;

-- +goose Down
DROP TABLE data_processing_jobs;
