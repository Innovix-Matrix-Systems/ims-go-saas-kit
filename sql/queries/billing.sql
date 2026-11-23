-- name: ListPackages :many
SELECT * FROM packages ORDER BY sort_order, id;

-- name: ListActivePackages :many
SELECT * FROM packages WHERE is_active ORDER BY sort_order, id;

-- name: GetPackage :one
SELECT * FROM packages WHERE id = @id;

-- name: CreatePackage :one
INSERT INTO packages (name, description, price, billing_cycle, trial_days, is_active, sort_order)
VALUES (@name, @description, @price, @billing_cycle, @trial_days, @is_active, @sort_order)
RETURNING *;

-- name: UpdatePackage :one
UPDATE packages
SET name          = @name,
    description   = @description,
    price         = @price,
    billing_cycle = @billing_cycle,
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
SELECT * FROM subscriptions
WHERE tenant_id = @tenant_id
  AND status IN ('active', 'trial')
  AND expires_at > now()
ORDER BY id DESC
LIMIT 1;

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

-- name: CountSubscriptionsForTenant :one
SELECT count(*) FROM subscriptions WHERE tenant_id = @tenant_id;

-- name: CreateSubscription :one
INSERT INTO subscriptions (tenant_id, package_id, status, starts_at, trial_ends_at, expires_at)
VALUES (@tenant_id, @package_id, @status, @starts_at, @trial_ends_at, @expires_at)
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
    starts_at           = COALESCE(starts_at, now()),
    expires_at          = @expires_at,
    cancelled_at        = NULL,
    cancellation_reason = NULL,
    updated_at          = now()
-- A cancelled subscription is final: a stale or replayed callback must not
-- bring it back (M8).
WHERE id = @id AND status IN ('pending', 'trial', 'active', 'expired')
RETURNING *;

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
    WHERE s.tenant_id = t.id AND s.status IN ('active', 'trial', 'pending', 'past_due')
  )
RETURNING t.id, t.name, t.code;

-- name: GetExpiringSubscriptions :many
SELECT * FROM subscriptions
WHERE status IN ('active', 'trial')
  AND expires_at IS NOT NULL
  AND expires_at <= @before
ORDER BY expires_at;

-- name: CreatePayment :one
INSERT INTO payments (tenant_id, subscription_id, payment_method, status, amount, currency, invoice_number, created_by)
VALUES (@tenant_id, @subscription_id, @payment_method, @status, @amount, @currency, @invoice_number, @created_by)
RETURNING *;

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
SELECT * FROM payments
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPayments :one
SELECT count(*) FROM payments
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

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
SELECT a.id, COALESCE(t.name, '')::text AS tenant_name, p.amount, p.currency, p.invoice_number,
       COALESCE(k.name, '')::text AS package_name
FROM payment_approval_requests a
JOIN payments p ON p.id = a.payment_id
LEFT JOIN tenants t ON t.id = a.tenant_id
LEFT JOIN subscriptions s ON s.id = a.subscription_id
LEFT JOIN packages k ON k.id = s.package_id
WHERE a.id = ANY(@ids::bigint[]);
