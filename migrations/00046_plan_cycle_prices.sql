-- +goose Up
-- Monthly and yearly on one plan (docs/guides/billing.md#monthly-and-yearly).

-- yearly_price: a monthly plan can also be bought yearly at this price; NULL
-- means monthly only.
ALTER TABLE packages ADD COLUMN yearly_price bigint;
ALTER TABLE packages ADD CONSTRAINT packages_yearly_price_check
    CHECK (yearly_price IS NULL OR (yearly_price >= 0 AND billing_cycle = 'monthly'));

-- billing_cycle: the cycle the organization chose; NULL is the plan's own
-- cycle, so existing rows keep their meaning. next_billing_cycle goes with
-- next_package_id: the cycle of a change that waits for the period end.
ALTER TABLE subscriptions ADD COLUMN billing_cycle text;
ALTER TABLE subscriptions ADD COLUMN next_billing_cycle text;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_billing_cycle_check
    CHECK (billing_cycle IS NULL OR billing_cycle IN ('monthly', 'yearly', 'one_time'));
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_next_billing_cycle_check
    CHECK (next_billing_cycle IS NULL OR next_billing_cycle IN ('monthly', 'yearly', 'one_time'));

-- +goose Down
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_next_billing_cycle_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_billing_cycle_check;
ALTER TABLE subscriptions DROP COLUMN next_billing_cycle;
ALTER TABLE subscriptions DROP COLUMN billing_cycle;
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_yearly_price_check;
ALTER TABLE packages DROP COLUMN yearly_price;
