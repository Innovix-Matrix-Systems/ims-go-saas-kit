-- +goose Up
-- A member who has not switched branch has no active branch: they work in
-- their own, and one who may switch sees every branch. Accounts used to be
-- created "switched" to their own branch; that is the same as not switched.
UPDATE users SET active_branch_id = NULL WHERE active_branch_id = branch_id;

-- +goose Down
UPDATE users SET active_branch_id = branch_id WHERE active_branch_id IS NULL AND branch_id IS NOT NULL;
