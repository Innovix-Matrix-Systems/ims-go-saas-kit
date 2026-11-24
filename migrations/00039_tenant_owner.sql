-- +goose Up
-- The organization's owner: one user who may transfer ownership and cannot
-- be removed, deactivated, demoted or erased while they own it. NULL for
-- organizations without one (created by staff before owners existed).
ALTER TABLE tenants ADD COLUMN owner_user_id bigint REFERENCES users (id) ON DELETE SET NULL;
CREATE INDEX tenants_owner_user_idx ON tenants (owner_user_id) WHERE owner_user_id IS NOT NULL;

-- Organizations created through sign-up already know who owns them.
UPDATE tenants t
SET owner_user_id = o.owner_user_id
FROM tenant_onboarding o
WHERE o.tenant_id = t.id AND o.owner_user_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS tenants_owner_user_idx;
ALTER TABLE tenants DROP COLUMN owner_user_id;
