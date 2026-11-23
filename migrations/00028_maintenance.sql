-- +goose Up
-- Maintenance mode (phase 24): one row (id = 1) every process reads. The app
-- is down while down_since is set. The bypass secret is stored as its
-- SHA-256 hash, never in clear.
CREATE TABLE maintenance (
    id          smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    down_since  timestamptz,
    message     text NOT NULL DEFAULT '',
    retry_after integer NOT NULL DEFAULT 60,
    secret_hash text,
    by_user_id  bigint REFERENCES users (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO maintenance (id) VALUES (1);

-- +goose Down
DROP TABLE IF EXISTS maintenance;
