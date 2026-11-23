-- +goose Up
-- Phase 21: a captured request made while acting as a user (X-Acting-User)
-- records who was really calling. user_id is then the user acted as.
ALTER TABLE dev_requests ADD COLUMN impersonator_id bigint;
CREATE INDEX dev_requests_impersonator_idx ON dev_requests (impersonator_id, created_at DESC)
    WHERE impersonator_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS dev_requests_impersonator_idx;
ALTER TABLE dev_requests DROP COLUMN IF EXISTS impersonator_id;
