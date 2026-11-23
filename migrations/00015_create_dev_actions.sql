-- +goose Up
CREATE TABLE dev_actions (
    id            bigserial PRIMARY KEY,
    actor_user_id bigint NOT NULL,
    action        text NOT NULL,
    target        text,
    params        jsonb NOT NULL DEFAULT '{}'::jsonb,
    status        text NOT NULL,
    error_message text,
    duration_ms   integer NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dev_actions_created_at_idx ON dev_actions (created_at DESC, id DESC);
CREATE INDEX dev_actions_actor_idx ON dev_actions (actor_user_id);

-- +goose Down
DROP TABLE dev_actions;
