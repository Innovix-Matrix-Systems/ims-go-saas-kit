-- name: ListPackages :many
SELECT * FROM packages ORDER BY sort_order, id;

-- name: ListActivePackages :many
SELECT * FROM packages WHERE is_active ORDER BY sort_order, id;

-- name: GetPackage :one
SELECT * FROM packages WHERE id = @id;

-- name: CreatePackage :one
INSERT INTO packages (name, description, price, billing_cycle, yearly_price, trial_days, is_active, sort_order)
VALUES (@name, @description, @price, @billing_cycle, sqlc.narg('yearly_price'), @trial_days, @is_active, @sort_order)
RETURNING *;

-- name: UpdatePackage :one
UPDATE packages
SET name          = @name,
    description   = @description,
    price         = @price,
    billing_cycle = @billing_cycle,
    yearly_price  = sqlc.narg('yearly_price'),
    trial_days    = @trial_days,
    is_active     = @is_active,
    sort_order    = @sort_order,
    updated_at    = now()
WHERE id = @id
RETURNING *;

-- name: DeletePackage :execrows
DELETE FROM packages WHERE id = @id;

-- name: ListPackageFeatures :many
SELECT * FROM package_features WHERE package_id = @package_id ORDER BY id;

-- name: ListFeaturesForPackages :many
SELECT * FROM package_features WHERE package_id = ANY(@package_ids::bigint[]) ORDER BY id;

-- name: UpsertPackageFeature :one
INSERT INTO package_features (package_id, feature_key, is_enabled, limit_value)
VALUES (@package_id, @feature_key, @is_enabled, @limit_value)
ON CONFLICT (package_id, feature_key) DO UPDATE
SET is_enabled  = EXCLUDED.is_enabled,
    limit_value = EXCLUDED.limit_value,
    updated_at  = now()
RETURNING *;

-- name: DeletePackageFeatures :exec
DELETE FROM package_features WHERE package_id = @package_id;

-- name: GetActiveSubscriptionForTenant :one
-- The plan in force: active or trial, or a scheduled one whose turn has come
-- (the expiry task makes it active; until then it already counts).
SELECT * FROM subscriptions
WHERE tenant_id = @tenant_id
  AND (status IN ('active', 'trial') OR (status = 'scheduled' AND starts_at <= now()))
  AND expires_at > now()
ORDER BY id DESC
LIMIT 1;

-- name: GetLapsedSubscriptionForTenant :one
-- The paid plan that lapsed most recently while its grace period runs: it
-- expired after @since (now minus BILLING_GRACE_DAYS). Callers use it only
-- when nothing is current (docs/guides/billing.md#grace-and-read-only).
SELECT * FROM subscriptions
WHERE tenant_id = @tenant_id
  AND status IN ('active', 'expired', 'past_due')
  AND expires_at <= now()
  AND expires_at > @since
ORDER BY expires_at DESC, id DESC
LIMIT 1;

-- name: TenantHadSubscription :one
-- Whether the tenant ever had a plan that started: one that never did
-- cannot lapse into read-only.
SELECT EXISTS (
    SELECT 1 FROM subscriptions
    WHERE tenant_id = @tenant_id
      AND status IN ('active', 'trial', 'expired', 'past_due', 'cancelled')
      AND starts_at <= now()
)::bool AS had;

-- name: GetLatestSubscriptionForTenant :one
SELECT * FROM subscriptions WHERE tenant_id = @tenant_id ORDER BY id DESC LIMIT 1;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = @id;

-- name: ListSubscriptions :many
SELECT * FROM subscriptions
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountSubscriptions :one
SELECT count(*) FROM subscriptions
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: ListStaffSubscriptions :many
-- The staff subscriptions panel (docs/guides/billing.md#the-staff-subscriptions-panel):
-- rows with their organization and plan, their standing and a view. The
-- standing follows service.standingOf: the plan in force per organization
-- (as GetActiveSubscriptionForTenant picks it), else the paid plan that
-- lapsed after @grace_since (in grace), else read-only when a plan ever
-- started. Deleted organizations are left out. CountStaffSubscriptions
-- counts the same rows; StaffSubscriptionCounts counts each view. The three
-- repeat the same CTEs (sqlc cannot share them): keep them in step.
WITH subs AS (
    SELECT s.* FROM subscriptions s
    JOIN tenants t ON t.id = s.tenant_id AND t.deleted_at IS NULL
),
in_force AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE (status IN ('active', 'trial') OR (status = 'scheduled' AND starts_at <= now()))
      AND expires_at > now()
    ORDER BY tenant_id, id DESC
),
lapsed AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE status IN ('active', 'expired', 'past_due') AND expires_at <= now() AND expires_at > @grace_since
      AND tenant_id NOT IN (SELECT tenant_id FROM in_force)
    ORDER BY tenant_id, expires_at DESC, id DESC
),
read_only AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE status IN ('active', 'trial', 'expired', 'past_due', 'cancelled') AND starts_at <= now()
      AND tenant_id NOT IN (SELECT tenant_id FROM in_force)
      AND tenant_id NOT IN (SELECT tenant_id FROM lapsed)
    ORDER BY tenant_id, expires_at DESC NULLS LAST, id DESC
)
SELECT s.id, s.tenant_id, s.package_id, s.status, s.starts_at, s.trial_ends_at, s.expires_at,
       s.cancelled_at, s.cancellation_reason, s.overrides, s.created_at, s.updated_at,
       s.next_package_id, s.cancel_at_period_end, s.billing_cycle, s.next_billing_cycle,
       t.name AS tenant_name, t.code AS tenant_code,
       k.name AS package_name, k.price AS package_price, k.billing_cycle AS package_billing_cycle,
       k.yearly_price AS package_yearly_price,
       (l.id IS NOT NULL)::bool AS in_grace, (r.id IS NOT NULL)::bool AS read_only
FROM subs s
JOIN tenants t ON t.id = s.tenant_id
JOIN packages k ON k.id = s.package_id
LEFT JOIN in_force f ON f.id = s.id
LEFT JOIN lapsed l ON l.id = s.id
LEFT JOIN read_only r ON r.id = s.id
WHERE (CASE @view::text
        WHEN 'current' THEN f.id IS NOT NULL OR l.id IS NOT NULL OR r.id IS NOT NULL
        WHEN 'active' THEN f.id IS NOT NULL AND s.status <> 'trial'
        WHEN 'trial' THEN f.id IS NOT NULL AND s.status = 'trial'
        WHEN 'grace' THEN l.id IS NOT NULL
        WHEN 'read_only' THEN r.id IS NOT NULL
        WHEN 'pending' THEN s.status = 'pending'
        WHEN 'scheduled' THEN s.status = 'scheduled' AND s.starts_at > now()
        ELSE true END)
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status')::text)
  AND (sqlc.narg('search')::text IS NULL
       OR t.name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR t.code ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('package_id')::bigint IS NULL OR s.package_id = sqlc.narg('package_id')::bigint)
  AND (sqlc.narg('billing_cycle')::text IS NULL
       OR COALESCE(s.billing_cycle, k.billing_cycle) = sqlc.narg('billing_cycle')::text)
  AND (sqlc.narg('expires_before')::timestamptz IS NULL
       OR (s.expires_at IS NOT NULL AND s.expires_at <= sqlc.narg('expires_before')::timestamptz))
ORDER BY
    CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'asc' THEN s.id END ASC,
    CASE WHEN @order_by::text = 'id' AND @order_dir::text = 'desc' THEN s.id END DESC,
    CASE WHEN @order_by::text = 'expires_at' AND @order_dir::text = 'asc' THEN s.expires_at END ASC NULLS LAST,
    CASE WHEN @order_by::text = 'expires_at' AND @order_dir::text = 'desc' THEN s.expires_at END DESC NULLS LAST,
    CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'asc' THEN s.created_at END ASC,
    CASE WHEN @order_by::text = 'created_at' AND @order_dir::text = 'desc' THEN s.created_at END DESC,
    s.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountStaffSubscriptions :one
-- The number of rows ListStaffSubscriptions matches (same CTEs and filters).
WITH subs AS (
    SELECT s.* FROM subscriptions s
    JOIN tenants t ON t.id = s.tenant_id AND t.deleted_at IS NULL
),
in_force AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE (status IN ('active', 'trial') OR (status = 'scheduled' AND starts_at <= now()))
      AND expires_at > now()
    ORDER BY tenant_id, id DESC
),
lapsed AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE status IN ('active', 'expired', 'past_due') AND expires_at <= now() AND expires_at > @grace_since
      AND tenant_id NOT IN (SELECT tenant_id FROM in_force)
    ORDER BY tenant_id, expires_at DESC, id DESC
),
read_only AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE status IN ('active', 'trial', 'expired', 'past_due', 'cancelled') AND starts_at <= now()
      AND tenant_id NOT IN (SELECT tenant_id FROM in_force)
      AND tenant_id NOT IN (SELECT tenant_id FROM lapsed)
    ORDER BY tenant_id, expires_at DESC NULLS LAST, id DESC
)
SELECT count(*)
FROM subs s
JOIN tenants t ON t.id = s.tenant_id
JOIN packages k ON k.id = s.package_id
LEFT JOIN in_force f ON f.id = s.id
LEFT JOIN lapsed l ON l.id = s.id
LEFT JOIN read_only r ON r.id = s.id
WHERE (CASE @view::text
        WHEN 'current' THEN f.id IS NOT NULL OR l.id IS NOT NULL OR r.id IS NOT NULL
        WHEN 'active' THEN f.id IS NOT NULL AND s.status <> 'trial'
        WHEN 'trial' THEN f.id IS NOT NULL AND s.status = 'trial'
        WHEN 'grace' THEN l.id IS NOT NULL
        WHEN 'read_only' THEN r.id IS NOT NULL
        WHEN 'pending' THEN s.status = 'pending'
        WHEN 'scheduled' THEN s.status = 'scheduled' AND s.starts_at > now()
        ELSE true END)
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status')::text)
  AND (sqlc.narg('search')::text IS NULL
       OR t.name ILIKE '%' || sqlc.narg('search')::text || '%'
       OR t.code ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('package_id')::bigint IS NULL OR s.package_id = sqlc.narg('package_id')::bigint)
  AND (sqlc.narg('billing_cycle')::text IS NULL
       OR COALESCE(s.billing_cycle, k.billing_cycle) = sqlc.narg('billing_cycle')::text)
  AND (sqlc.narg('expires_before')::timestamptz IS NULL
       OR (s.expires_at IS NOT NULL AND s.expires_at <= sqlc.narg('expires_before')::timestamptz));

-- name: StaffSubscriptionCounts :one
-- A count per view of ListStaffSubscriptions (the tabs of the staff panel);
-- the same standing rules.
WITH subs AS (
    SELECT s.* FROM subscriptions s
    JOIN tenants t ON t.id = s.tenant_id AND t.deleted_at IS NULL
),
in_force AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE (status IN ('active', 'trial') OR (status = 'scheduled' AND starts_at <= now()))
      AND expires_at > now()
    ORDER BY tenant_id, id DESC
),
lapsed AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE status IN ('active', 'expired', 'past_due') AND expires_at <= now() AND expires_at > @grace_since
      AND tenant_id NOT IN (SELECT tenant_id FROM in_force)
    ORDER BY tenant_id, expires_at DESC, id DESC
),
read_only AS (
    SELECT DISTINCT ON (tenant_id) id, tenant_id FROM subs
    WHERE status IN ('active', 'trial', 'expired', 'past_due', 'cancelled') AND starts_at <= now()
      AND tenant_id NOT IN (SELECT tenant_id FROM in_force)
      AND tenant_id NOT IN (SELECT tenant_id FROM lapsed)
    ORDER BY tenant_id, expires_at DESC NULLS LAST, id DESC
)
SELECT
    (SELECT count(*) FROM in_force f JOIN subs s ON s.id = f.id WHERE s.status <> 'trial') AS active,
    (SELECT count(*) FROM in_force f JOIN subs s ON s.id = f.id WHERE s.status = 'trial') AS trial,
    (SELECT count(*) FROM lapsed) AS grace,
    (SELECT count(*) FROM read_only) AS read_only,
    (SELECT count(*) FROM subs WHERE status = 'pending') AS pending,
    (SELECT count(*) FROM subs WHERE status = 'scheduled' AND starts_at > now()) AS scheduled,
    (SELECT count(*) FROM subs) AS all_rows;

-- name: CountSubscriptionsForTenant :one
SELECT count(*) FROM subscriptions WHERE tenant_id = @tenant_id;

-- name: CreateSubscription :one
INSERT INTO subscriptions (tenant_id, package_id, status, starts_at, trial_ends_at, expires_at, billing_cycle)
VALUES (@tenant_id, @package_id, @status, @starts_at, @trial_ends_at, @expires_at, sqlc.narg('billing_cycle'))
RETURNING *;

-- name: UpdateSubscriptionStatus :one
UPDATE subscriptions
SET status              = @status,
    cancelled_at        = @cancelled_at,
    cancellation_reason = @cancellation_reason,
    updated_at          = now()
WHERE id = @id
RETURNING *;

-- name: ActivateSubscription :one
UPDATE subscriptions
SET status              = 'active',
    starts_at           = COALESCE(sqlc.narg('starts_at')::timestamptz, starts_at, now()),
    expires_at          = @expires_at,
    cancelled_at        = NULL,
    cancellation_reason = NULL,
    updated_at          = now()
-- A cancelled subscription is final: a stale or replayed callback must not
-- bring it back (M8).
WHERE id = @id AND status IN ('pending', 'scheduled', 'trial', 'active', 'expired')
RETURNING *;

-- name: ScheduleSubscription :one
-- A paid renewal (or a free plan) waits for the current period to end.
UPDATE subscriptions
SET status     = 'scheduled',
    starts_at  = @starts_at,
    expires_at = @expires_at,
    overrides  = @overrides,
    updated_at = now()
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: GetScheduledSubscriptionForTenant :one
SELECT * FROM subscriptions WHERE tenant_id = @tenant_id AND status = 'scheduled' LIMIT 1;

-- name: CancelScheduledSubscriptions :many
UPDATE subscriptions
SET status              = 'cancelled',
    cancelled_at        = now(),
    cancellation_reason = @reason,
    updated_at          = now()
WHERE tenant_id = @tenant_id AND status = 'scheduled'
RETURNING *;

-- name: SetSubscriptionPlanEnd :one
-- What happens when the period ends: renew onto another plan
-- (next_package_id) or not at all (cancel_at_period_end).
UPDATE subscriptions
SET next_package_id      = sqlc.narg('next_package_id'),
    next_billing_cycle   = sqlc.narg('next_billing_cycle'),
    cancel_at_period_end = @cancel_at_period_end,
    updated_at           = now()
WHERE id = @id
RETURNING *;

-- name: ActivateDueScheduledSubscriptions :execrows
-- Run after ExpireSubscriptions: a scheduled plan whose turn has come starts.
UPDATE subscriptions
SET status     = 'active',
    updated_at = now()
WHERE status = 'scheduled'
  AND starts_at <= now()
  AND (sqlc.narg('tenant_id')::bigint IS NULL OR tenant_id = sqlc.narg('tenant_id')::bigint)
  AND NOT EXISTS (
    SELECT 1 FROM subscriptions a
    WHERE a.tenant_id = subscriptions.tenant_id AND a.status IN ('active', 'trial') AND a.expires_at > now()
  );

-- name: SetSubscriptionOverrides :one
UPDATE subscriptions
SET overrides  = @overrides,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeactivateActiveSubscriptions :exec
UPDATE subscriptions
SET status              = 'cancelled',
    cancelled_at        = now(),
    cancellation_reason = 'Replaced by a new subscription.',
    updated_at          = now()
WHERE tenant_id = @tenant_id AND status IN ('active', 'trial');

-- name: ExpireSubscriptions :execrows
UPDATE subscriptions
SET status     = 'expired',
    updated_at = now()
WHERE status IN ('active', 'trial')
  AND expires_at IS NOT NULL
  AND expires_at < now();

-- name: SuspendLapsedTenants :many
-- Suspends active tenants whose subscription expired before @expired_before
-- and that have nothing current or in progress (BILLING_SUSPEND_ON_EXPIRY).
UPDATE tenants t
SET is_active  = false,
    updated_at = now()
WHERE t.is_active
  AND t.deleted_at IS NULL
  AND t.id <> @keep_tenant_id
  AND EXISTS (
    SELECT 1 FROM subscriptions s
    WHERE s.tenant_id = t.id AND s.status = 'expired' AND s.expires_at < @expired_before
  )
  AND NOT EXISTS (
    SELECT 1 FROM subscriptions s
    WHERE s.tenant_id = t.id AND s.status IN ('active', 'trial', 'pending', 'scheduled', 'past_due')
  )
RETURNING t.id, t.name, t.code;

-- name: GetExpiringSubscriptions :many
-- Subscriptions to remind about: not those the organization chose not to
-- renew, nor those whose next period is already paid (scheduled).
SELECT s.* FROM subscriptions s
WHERE s.status IN ('active', 'trial')
  AND s.expires_at IS NOT NULL
  AND s.expires_at <= @before
  AND NOT s.cancel_at_period_end
  AND NOT EXISTS (SELECT 1 FROM subscriptions n WHERE n.tenant_id = s.tenant_id AND n.status = 'scheduled')
ORDER BY s.expires_at;

-- name: CreatePayment :one
INSERT INTO payments (tenant_id, subscription_id, payment_method, status, amount, currency, invoice_number, created_by,
                      kind, lines, gross)
VALUES (@tenant_id, @subscription_id, @payment_method, @status, @amount, @currency, @invoice_number, @created_by,
        @kind, @lines, @gross)
RETURNING *;

-- name: SetPaymentQuote :one
-- Freezes the checkout quote on the payment: why, the invoice lines, the
-- value bought, the account credit reserved and the plan it replaces.
UPDATE payments
SET kind                     = @kind,
    lines                    = @lines,
    gross                    = @gross,
    balance_applied          = @balance_applied,
    replaces_subscription_id = sqlc.narg('replaces_subscription_id'),
    updated_at               = now()
WHERE id = @id
RETURNING *;

-- name: GetPendingPaymentForTenant :one
-- Any checkout of the organization still waiting (one at a time).
SELECT * FROM payments WHERE tenant_id = @tenant_id AND status = 'pending' ORDER BY id DESC LIMIT 1;

-- name: SubscriptionPaidValue :one
-- What the organization paid for a subscription's period: the value bought
-- (gross) less what was given back.
SELECT COALESCE(sum(gross - refunded_amount), 0)::bigint AS paid
FROM payments
WHERE subscription_id = @subscription_id AND status IN ('completed', 'refunded');

-- name: RefundPayment :one
UPDATE payments
SET refunded_amount = refunded_amount + @refund::bigint,
    status          = CASE WHEN refunded_amount + @refund::bigint >= amount THEN 'refunded' ELSE status END,
    updated_at      = now()
WHERE id = @id
  AND status = 'completed'
  AND refunded_amount + @refund::bigint <= amount
RETURNING *;

-- name: CreditBalance :one
SELECT COALESCE(sum(amount), 0)::bigint AS balance
FROM billing_credits
WHERE tenant_id = @tenant_id AND currency = @currency;

-- name: InsertCredit :one
INSERT INTO billing_credits (tenant_id, amount, currency, reason, payment_id, subscription_id, created_by)
VALUES (@tenant_id, @amount, @currency, @reason, @payment_id, @subscription_id, @created_by)
RETURNING *;

-- name: ListCreditsForTenant :many
SELECT * FROM billing_credits
WHERE tenant_id = @tenant_id
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountCreditsForTenant :one
SELECT count(*) FROM billing_credits WHERE tenant_id = @tenant_id;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = @id;

-- name: GetPaymentByGatewayRef :one
SELECT * FROM payments WHERE gateway_ref = @gateway_ref;

-- name: GetPendingCheckout :one
-- The in-flight checkout for a tenant + package + method: a pending
-- subscription whose payment is still pending (subscribe idempotency, H11).
SELECT p.* FROM payments p
JOIN subscriptions s ON s.id = p.subscription_id
WHERE s.tenant_id = @tenant_id
  AND s.package_id = @package_id
  AND s.status = 'pending'
  AND p.status = 'pending'
  AND p.payment_method = @payment_method
ORDER BY p.id DESC
LIMIT 1;

-- name: UpdatePaymentGateway :execrows
UPDATE payments
SET gateway_ref      = COALESCE(sqlc.narg('gateway_ref')::text, gateway_ref),
    gateway_status   = COALESCE(sqlc.narg('gateway_status')::text, gateway_status),
    gateway_response = @gateway_response,
    updated_at       = now()
WHERE id = @id;

-- name: UpdatePaymentStatus :one
UPDATE payments
SET status         = @status,
    paid_at        = CASE WHEN @status::text = 'completed' THEN COALESCE(paid_at, now()) ELSE paid_at END,
    recorded_by    = COALESCE(@recorded_by, recorded_by),
    gateway_status = COALESCE(sqlc.narg('gateway_status')::text, gateway_status),
    updated_at     = now()
-- Allowed transitions only (M8): pending -> anything, failed -> completed (a
-- late genuine success), completed -> refunded, or a no-op repeat.
WHERE id = @id
  AND (
      status = 'pending'
      OR status = @status::text
      OR (status = 'failed' AND @status::text = 'completed')
      OR (status = 'completed' AND @status::text = 'refunded')
  )
RETURNING *;

-- name: ListPaymentsForTenant :many
SELECT * FROM payments
WHERE tenant_id = @tenant_id
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPaymentsForTenant :one
SELECT count(*) FROM payments WHERE tenant_id = @tenant_id;

-- name: ListPayments :many
-- Every organization's payments (staff), optionally one organization's.
SELECT * FROM payments
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('tenant_id')::bigint IS NULL OR tenant_id = sqlc.narg('tenant_id')::bigint)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPayments :one
SELECT count(*) FROM payments
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('tenant_id')::bigint IS NULL OR tenant_id = sqlc.narg('tenant_id')::bigint);

-- name: CreatePaymentApproval :one
INSERT INTO payment_approval_requests (tenant_id, subscription_id, payment_id, payment_method, payment_data, status, created_by)
VALUES (@tenant_id, @subscription_id, @payment_id, @payment_method, @payment_data, @status, @created_by)
RETURNING *;

-- name: GetPaymentApproval :one
SELECT * FROM payment_approval_requests WHERE id = @id;

-- name: ListPaymentApprovals :many
SELECT * FROM payment_approval_requests
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPaymentApprovals :one
SELECT count(*) FROM payment_approval_requests
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: UpdatePaymentApproval :one
UPDATE payment_approval_requests
SET status        = @status,
    reviewer_id   = @reviewer_id,
    reviewer_note = @reviewer_note,
    reviewed_at   = now(),
    updated_by    = @updated_by,
    updated_at    = now()
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: TenantUsage :one
-- What a tenant uses of its plan's limits (users.max, branches.max,
-- storage.mb): the same rows the limit checks count.
SELECT
    (SELECT count(*) FROM users u WHERE u.tenant_id = @tenant_id::bigint AND u.deleted_at IS NULL)::bigint AS users,
    (SELECT count(*) FROM branches b WHERE b.tenant_id = @tenant_id::bigint AND b.deleted_at IS NULL)::bigint AS branches,
    (SELECT COALESCE(sum(f.size), 0) FROM files f WHERE f.tenant_id = @tenant_id::bigint)::bigint AS storage_bytes;

-- name: ApprovalDetailsByIDs :many
-- What a reviewer needs beside each approval: who pays, how much, for which
-- plan, and the invoice.
SELECT a.id, a.tenant_id, COALESCE(t.name, '')::text AS tenant_name, p.amount, p.currency, p.invoice_number,
       COALESCE(k.name, '')::text AS package_name, p.kind, p.lines, p.replaces_subscription_id
FROM payment_approval_requests a
JOIN payments p ON p.id = a.payment_id
LEFT JOIN tenants t ON t.id = a.tenant_id
LEFT JOIN subscriptions s ON s.id = a.subscription_id
LEFT JOIN packages k ON k.id = s.package_id
WHERE a.id = ANY(@ids::bigint[]);

-- name: PendingApprovalForPayment :one
SELECT * FROM payment_approval_requests
WHERE payment_id = @payment_id AND status = 'pending'
LIMIT 1;

-- name: InvoiceForPayment :one
-- Everything an invoice shows for one of a tenant's payments.
SELECT p.id, p.invoice_number, p.status, p.payment_method, p.amount, p.currency, p.paid_at, p.created_at,
       p.kind, p.lines, p.refunded_amount,
       COALESCE(p.gateway_ref, '')::text AS gateway_ref,
       t.name AS tenant_name, COALESCE(t.email, '')::text AS tenant_email, COALESCE(t.phone, '')::text AS tenant_phone,
       COALESCE(k.name, '')::text AS package_name, COALESCE(s.billing_cycle, k.billing_cycle, '')::text AS billing_cycle,
       s.starts_at, s.expires_at
FROM payments p
JOIN tenants t ON t.id = p.tenant_id
LEFT JOIN subscriptions s ON s.id = p.subscription_id
LEFT JOIN packages k ON k.id = s.package_id
WHERE p.id = @payment_id AND p.tenant_id = @tenant_id;
