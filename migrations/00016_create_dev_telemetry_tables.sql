-- +goose Up
CREATE TABLE dev_requests (
    id             bigserial PRIMARY KEY,
    request_id     text NOT NULL,
    method         text NOT NULL,
    path           text NOT NULL,
    route          text NOT NULL DEFAULT '',
    status         integer NOT NULL,
    duration_ms    double precision NOT NULL,
    request_bytes  bigint NOT NULL DEFAULT 0,
    response_bytes bigint NOT NULL DEFAULT 0,
    user_id        bigint,
    tenant_id      bigint,
    branch_id      bigint,
    ip             text,
    user_agent     text,
    headers        jsonb,
    request_body   jsonb,
    response_body  jsonb,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dev_requests_created_at_idx ON dev_requests (created_at DESC, id DESC);
CREATE INDEX dev_requests_request_id_idx ON dev_requests (request_id);

CREATE TABLE dev_queries (
    id          bigserial PRIMARY KEY,
    request_id  text,
    sql         text NOT NULL,
    args        jsonb,
    duration_ms double precision NOT NULL,
    row_count   bigint NOT NULL DEFAULT 0,
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dev_queries_request_id_idx ON dev_queries (request_id);
CREATE INDEX dev_queries_created_at_idx ON dev_queries (created_at DESC, id DESC);

CREATE TABLE dev_cache_events (
    id          bigserial PRIMARY KEY,
    request_id  text,
    operation   text NOT NULL,
    key         text NOT NULL,
    hit         boolean,
    duration_ms double precision NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dev_cache_events_request_id_idx ON dev_cache_events (request_id);
CREATE INDEX dev_cache_events_created_at_idx ON dev_cache_events (created_at DESC, id DESC);

CREATE TABLE dev_queue_events (
    id          bigserial PRIMARY KEY,
    request_id  text,
    event       text NOT NULL,
    queue       text NOT NULL,
    kind        text NOT NULL,
    job_id      bigint,
    duration_ms double precision NOT NULL DEFAULT 0,
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dev_queue_events_request_id_idx ON dev_queue_events (request_id);
CREATE INDEX dev_queue_events_created_at_idx ON dev_queue_events (created_at DESC, id DESC);

CREATE TABLE dev_logs (
    id         bigserial PRIMARY KEY,
    request_id text,
    level      text NOT NULL,
    message    text NOT NULL,
    attributes jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dev_logs_request_id_idx ON dev_logs (request_id);
CREATE INDEX dev_logs_created_at_idx ON dev_logs (created_at DESC, id DESC);
CREATE INDEX dev_logs_level_idx ON dev_logs (level);

CREATE TABLE dev_exceptions (
    id         bigserial PRIMARY KEY,
    request_id text,
    class      text NOT NULL,
    message    text NOT NULL,
    stack      text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dev_exceptions_request_id_idx ON dev_exceptions (request_id);
CREATE INDEX dev_exceptions_created_at_idx ON dev_exceptions (created_at DESC, id DESC);

-- +goose Down
DROP TABLE dev_exceptions;
DROP TABLE dev_logs;
DROP TABLE dev_queue_events;
DROP TABLE dev_cache_events;
DROP TABLE dev_queries;
DROP TABLE dev_requests;
