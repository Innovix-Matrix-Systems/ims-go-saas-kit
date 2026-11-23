-- +goose Up
CREATE TABLE personal_access_tokens (
    id             bigserial PRIMARY KEY,
    tokenable_type text NOT NULL,
    tokenable_id   bigint NOT NULL,
    name           text NOT NULL,
    device         text NOT NULL DEFAULT '',
    token          char(64) NOT NULL,
    abilities      text,
    last_used_at   timestamptz,
    expires_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX personal_access_tokens_token_unique ON personal_access_tokens (token);
CREATE INDEX personal_access_tokens_tokenable_idx ON personal_access_tokens (tokenable_type, tokenable_id);

-- +goose Down
DROP TABLE personal_access_tokens;
