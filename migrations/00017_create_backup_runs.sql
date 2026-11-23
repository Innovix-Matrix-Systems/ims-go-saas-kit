-- +goose Up
CREATE TABLE backup_runs (
    id               bigserial PRIMARY KEY,
    uuid             uuid NOT NULL UNIQUE,
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'running', 'completed', 'partial', 'failed', 'deleted')),
    trigger_kind     text NOT NULL CHECK (trigger_kind IN ('cli', 'schedule', 'dashboard')),
    actor_user_id    bigint,
    filename         text NOT NULL,
    scope            text NOT NULL DEFAULT 'database'
                     CHECK (scope IN ('database', 'database_files')),
    size_bytes       bigint NOT NULL DEFAULT 0,
    checksum_sha256  text,
    pg_dump_version  text,
    database_name    text,
    destinations     jsonb NOT NULL DEFAULT '[]'::jsonb,
    error            text,
    started_at       timestamptz,
    finished_at      timestamptz,
    deleted_at       timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX backup_runs_created_at_idx ON backup_runs (created_at DESC);
CREATE INDEX backup_runs_status_idx ON backup_runs (status);
CREATE INDEX backup_runs_active_idx ON backup_runs (created_at DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE backup_runs;
