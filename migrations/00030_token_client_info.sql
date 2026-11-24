-- +goose Up
-- Self-service sessions: where and with what each token was
-- issued, so users can recognise and revoke their own sessions.
ALTER TABLE personal_access_tokens
    ADD COLUMN ip         text,
    ADD COLUMN user_agent text;

-- The new sign-in alert asks "has this user signed in from this IP lately?".
CREATE INDEX activity_log_actor_action_idx ON activity_log (actor_id, action, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS activity_log_actor_action_idx;
ALTER TABLE personal_access_tokens
    DROP COLUMN IF EXISTS user_agent,
    DROP COLUMN IF EXISTS ip;
