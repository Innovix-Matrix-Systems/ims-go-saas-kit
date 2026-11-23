-- +goose Up
-- A payment nobody confirmed can be withdrawn by the organization; it ends
-- cancelled (not failed: nothing went wrong with it).
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_status_check;
ALTER TABLE payments ADD CONSTRAINT payments_status_check
    CHECK (status IN ('pending', 'completed', 'failed', 'refunded', 'cancelled')) NOT VALID;

-- +goose Down
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_status_check;
ALTER TABLE payments ADD CONSTRAINT payments_status_check
    CHECK (status IN ('pending', 'completed', 'failed', 'refunded')) NOT VALID;
