-- +goose Up
-- Phase 21: act-as-user. Entries written while someone acts as a user carry
-- the person really calling. impersonator_name is a snapshot, so the record
-- still names them after their account is deleted (the id is then NULL).
-- Rows with tenant_id IS NULL are the system audit log; the partial index
-- serves its "system only" view.
ALTER TABLE activity_log
    ADD COLUMN impersonator_id   bigint REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN impersonator_name text;

CREATE INDEX activity_log_impersonator_idx ON activity_log (impersonator_id, created_at DESC)
    WHERE impersonator_id IS NOT NULL;
CREATE INDEX activity_log_system_feed_idx ON activity_log (created_at DESC, id DESC)
    WHERE tenant_id IS NULL;

-- +goose Down
DROP INDEX IF EXISTS activity_log_system_feed_idx;
DROP INDEX IF EXISTS activity_log_impersonator_idx;
ALTER TABLE activity_log
    DROP COLUMN IF EXISTS impersonator_name,
    DROP COLUMN IF EXISTS impersonator_id;
