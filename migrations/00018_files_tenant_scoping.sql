-- +goose Up
-- Files gain explicit tenant scoping. The
-- column is nullable: rows that cannot be attributed to a tenant (uploads by
-- tenantless platform accounts) stay global and are only visible to holders
-- of the system-only file.view.all permission.
ALTER TABLE files ADD COLUMN tenant_id bigint REFERENCES tenants (id) ON DELETE CASCADE;

-- Backfill from the owning user's tenant (avatars and attachments).
UPDATE files f
SET tenant_id = u.tenant_id
FROM users u
WHERE f.owner_type = 'user'
  AND f.owner_id = u.id
  AND f.tenant_id IS NULL
  AND u.tenant_id IS NOT NULL;

-- Backfill the remainder from the creator's tenant (standalone library files).
UPDATE files f
SET tenant_id = u.tenant_id
FROM users u
WHERE f.created_by = u.id
  AND f.tenant_id IS NULL
  AND u.tenant_id IS NOT NULL;

CREATE INDEX files_tenant_created_idx ON files (tenant_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS files_tenant_created_idx;
ALTER TABLE files DROP COLUMN IF EXISTS tenant_id;
