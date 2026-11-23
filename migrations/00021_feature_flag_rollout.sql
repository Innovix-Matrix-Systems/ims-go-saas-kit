-- +goose Up
-- Percentage rollout: an enabled flag row may carry a rollout percentage
-- (0-100). NULL means everyone (100%). Each user (or tenant) lands in a fixed
-- bucket per flag, so raising 20% -> 60% keeps the first 20% enabled.
ALTER TABLE feature_flags ADD COLUMN rollout smallint;
ALTER TABLE feature_flags ADD CONSTRAINT feature_flags_rollout_range
    CHECK (rollout IS NULL OR (rollout BETWEEN 0 AND 100 AND value));

-- +goose Down
ALTER TABLE feature_flags DROP CONSTRAINT IF EXISTS feature_flags_rollout_range;
ALTER TABLE feature_flags DROP COLUMN IF EXISTS rollout;
