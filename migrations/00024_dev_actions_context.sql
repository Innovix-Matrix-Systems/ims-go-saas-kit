-- +goose Up
-- The dashboard audit log records where an action came from (IP and browser)
-- and is filtered by action, person, result and time.
ALTER TABLE dev_actions ADD COLUMN ip text;
ALTER TABLE dev_actions ADD COLUMN user_agent text;
CREATE INDEX dev_actions_action_idx ON dev_actions (action, id DESC);

-- +goose Down
DROP INDEX IF EXISTS dev_actions_action_idx;
ALTER TABLE dev_actions DROP COLUMN IF EXISTS user_agent;
ALTER TABLE dev_actions DROP COLUMN IF EXISTS ip;
