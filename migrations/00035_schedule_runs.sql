-- +goose Up
-- Scheduler run history: one row per run of a scheduled task,
-- scheduled or run by hand. A run is inserted as 'running' and finished as
-- 'ok' or 'failed'; one the scheduler lost (a crash) becomes 'interrupted'
-- when it starts again. Pruned after SCHEDULE_HISTORY_DAYS.
CREATE TABLE schedule_runs (
    id           bigserial PRIMARY KEY,
    task         text NOT NULL,
    triggered_by text NOT NULL,
    status       text NOT NULL DEFAULT 'running',
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    duration_ms  double precision,
    error        text
);

-- Each task's newest runs and its history (task, id DESC); the history by
-- status (status, id DESC); pruning by age (started_at).
CREATE INDEX schedule_runs_task_idx ON schedule_runs (task, id DESC);
CREATE INDEX schedule_runs_status_idx ON schedule_runs (status, id DESC);
CREATE INDEX schedule_runs_started_at_idx ON schedule_runs (started_at);

-- +goose Down
DROP TABLE schedule_runs;
