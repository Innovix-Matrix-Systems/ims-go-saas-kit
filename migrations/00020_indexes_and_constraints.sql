-- +goose Up
-- Phase 16: indexes for the hot list/search paths and DB-level status rules.
--
-- Migration convention (M41): keep migrations short-locking. Plain CREATE INDEX
-- is fine at starter scale; on a large live table write a separate goose
-- "NO TRANSACTION" migration using CREATE INDEX CONCURRENTLY, and
-- put `SET lock_timeout = '5s';` first so a blocked DDL fails fast instead of
-- queueing every query behind it.
SET lock_timeout = '10s';

-- M33: tenant lists always filter soft deletes.
CREATE INDEX IF NOT EXISTS users_tenant_active_idx ON users (tenant_id, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS branches_tenant_active_idx ON branches (tenant_id, id) WHERE deleted_at IS NULL;

-- M34: ILIKE '%term%' search is not indexed here on purpose: trigram indexes
-- need the pg_trgm extension (privileges on managed Postgres, one schema per
-- database). When a tenant's lists get large, add in your own migration:
--   CREATE EXTENSION IF NOT EXISTS pg_trgm;
--   CREATE INDEX users_name_trgm_idx ON users USING gin (name gin_trgm_ops);

-- M35: the notification feed orders by (created_at DESC, id DESC) per tenant.
CREATE INDEX IF NOT EXISTS notifications_feed_idx ON notifications (tenant_id, created_at DESC, id DESC);

-- M36: remaining hot predicates.
CREATE INDEX IF NOT EXISTS subscriptions_tenant_status_idx ON subscriptions (tenant_id, status);
CREATE INDEX IF NOT EXISTS personal_access_tokens_expires_idx ON personal_access_tokens (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS files_created_idx ON files (created_at DESC);
CREATE INDEX IF NOT EXISTS dev_requests_tenant_idx ON dev_requests (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS dev_requests_user_idx ON dev_requests (user_id, created_at DESC);

-- M39: status columns only take known values. NOT VALID enforces the rule
-- for every new write without failing on any legacy row; run
-- `ALTER TABLE ... VALIDATE CONSTRAINT ...` once old data is clean.
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_status_check
    CHECK (status IN ('trial', 'pending', 'active', 'past_due', 'cancelled', 'expired')) NOT VALID;
ALTER TABLE payments ADD CONSTRAINT payments_status_check
    CHECK (status IN ('pending', 'completed', 'failed', 'refunded')) NOT VALID;
ALTER TABLE payment_approval_requests ADD CONSTRAINT payment_approval_requests_status_check
    CHECK (status IN ('pending', 'approved', 'rejected')) NOT VALID;
ALTER TABLE notifications ADD CONSTRAINT notifications_priority_check
    CHECK (priority IN ('INFO', 'WARNING', 'CRITICAL')) NOT VALID;
ALTER TABLE data_processing_jobs ADD CONSTRAINT data_processing_jobs_status_check
    CHECK (status IN ('pending', 'processing', 'completed', 'failed')) NOT VALID;

-- +goose Down
ALTER TABLE data_processing_jobs DROP CONSTRAINT IF EXISTS data_processing_jobs_status_check;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_priority_check;
ALTER TABLE payment_approval_requests DROP CONSTRAINT IF EXISTS payment_approval_requests_status_check;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_status_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_status_check;
DROP INDEX IF EXISTS dev_requests_user_idx;
DROP INDEX IF EXISTS dev_requests_tenant_idx;
DROP INDEX IF EXISTS files_created_idx;
DROP INDEX IF EXISTS personal_access_tokens_expires_idx;
DROP INDEX IF EXISTS subscriptions_tenant_status_idx;
DROP INDEX IF EXISTS notifications_feed_idx;
DROP INDEX IF EXISTS branches_tenant_active_idx;
DROP INDEX IF EXISTS users_tenant_active_idx;
