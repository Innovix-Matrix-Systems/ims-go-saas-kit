-- +goose Up
-- Plan changes, renewals, credits and refunds (docs/guides/billing.md#changing-plans).

-- A subscription can wait for its turn ('scheduled': a renewal paid early or
-- a switch to a free plan at the end of the period). One per organization.
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_status_check;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_status_check
    CHECK (status IN ('trial', 'pending', 'scheduled', 'active', 'past_due', 'cancelled', 'expired')) NOT VALID;
CREATE UNIQUE INDEX subscriptions_one_scheduled ON subscriptions (tenant_id) WHERE status = 'scheduled';

-- next_package_id: the plan the organization moves to when it renews (a
-- downgrade chosen before the end of the period). cancel_at_period_end: the
-- organization does not want to renew; it keeps the plan until it expires.
ALTER TABLE subscriptions ADD COLUMN next_package_id bigint REFERENCES packages (id) ON DELETE SET NULL;
ALTER TABLE subscriptions ADD COLUMN cancel_at_period_end boolean NOT NULL DEFAULT false;

-- kind: why the payment was made. lines: the invoice lines, frozen at
-- checkout. gross: the value of the period bought (the plan price before
-- credits, or what staff agreed), the base of the unused-time credit.
-- balance_applied: account credit reserved by this payment (given back when
-- it fails or is withdrawn). refunded_amount: money given back so far.
ALTER TABLE payments ADD COLUMN kind text NOT NULL DEFAULT 'new';
ALTER TABLE payments ADD COLUMN lines jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE payments ADD COLUMN gross bigint NOT NULL DEFAULT 0;
ALTER TABLE payments ADD COLUMN balance_applied bigint NOT NULL DEFAULT 0;
ALTER TABLE payments ADD COLUMN replaces_subscription_id bigint REFERENCES subscriptions (id) ON DELETE SET NULL;
ALTER TABLE payments ADD COLUMN refunded_amount bigint NOT NULL DEFAULT 0;
UPDATE payments SET gross = amount;
ALTER TABLE payments ADD CONSTRAINT payments_kind_check
    CHECK (kind IN ('new', 'renew', 'upgrade', 'downgrade', 'staff')) NOT VALID;
ALTER TABLE payments ADD CONSTRAINT payments_refunded_amount_check
    CHECK (refunded_amount >= 0 AND refunded_amount <= amount) NOT VALID;

-- The organization's account credit: a signed ledger, the balance is the sum.
CREATE TABLE billing_credits (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    amount          bigint NOT NULL CHECK (amount <> 0),
    currency        text NOT NULL,
    reason          text NOT NULL,
    payment_id      bigint REFERENCES payments (id) ON DELETE SET NULL,
    subscription_id bigint REFERENCES subscriptions (id) ON DELETE SET NULL,
    created_by      bigint,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX billing_credits_tenant_idx ON billing_credits (tenant_id, currency);

-- +goose Down
DROP TABLE billing_credits;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_refunded_amount_check;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_kind_check;
ALTER TABLE payments DROP COLUMN refunded_amount;
ALTER TABLE payments DROP COLUMN replaces_subscription_id;
ALTER TABLE payments DROP COLUMN balance_applied;
ALTER TABLE payments DROP COLUMN gross;
ALTER TABLE payments DROP COLUMN lines;
ALTER TABLE payments DROP COLUMN kind;
ALTER TABLE subscriptions DROP COLUMN cancel_at_period_end;
ALTER TABLE subscriptions DROP COLUMN next_package_id;
DROP INDEX IF EXISTS subscriptions_one_scheduled;
UPDATE subscriptions SET status = 'cancelled' WHERE status = 'scheduled';
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_status_check;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_status_check
    CHECK (status IN ('trial', 'pending', 'active', 'past_due', 'cancelled', 'expired')) NOT VALID;
