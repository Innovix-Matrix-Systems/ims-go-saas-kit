-- name: LockTenantResource :exec
-- Serializes quota-checked creates (users.max, branches.max) per tenant for
-- the rest of the transaction: count + insert run under this lock.
SELECT pg_advisory_xact_lock(hashtextextended(@resource::text || ':' || CAST(@tenant_id AS bigint)::text, 0));
