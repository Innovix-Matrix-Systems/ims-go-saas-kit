-- +goose Up
CREATE TABLE cache (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    expiration timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX cache_expiration_idx ON cache (expiration) WHERE expiration IS NOT NULL;

-- +goose Down
DROP TABLE cache;
