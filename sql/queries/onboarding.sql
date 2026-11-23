-- name: CreateTenantOnboarding :one
INSERT INTO tenant_onboarding (tenant_id, owner_user_id, source)
VALUES (@tenant_id, @owner_user_id, @source)
RETURNING *;

-- name: GetTenantOnboarding :one
-- The owner is the organization's current owner (ownership can be
-- transferred); the onboarding row remembers who signed up.
SELECT o.tenant_id,
       COALESCE(t.owner_user_id, o.owner_user_id)::bigint AS owner_user_id,
       (t.owner_user_id IS NOT NULL OR o.owner_user_id IS NOT NULL)::boolean AS has_owner,
       o.source, o.completed_at, o.skipped_steps, o.created_at
FROM tenant_onboarding o
JOIN tenants t ON t.id = o.tenant_id
WHERE o.tenant_id = @tenant_id;

-- name: CompleteTenantOnboarding :execrows
UPDATE tenant_onboarding
SET completed_at  = COALESCE(completed_at, now()),
    skipped_steps = @skipped_steps::text[],
    updated_at    = now()
WHERE tenant_id = @tenant_id;

-- name: ListOnboardingOwnedByUser :many
SELECT * FROM tenant_onboarding WHERE owner_user_id = @user_id ORDER BY tenant_id;

-- name: CreateUserAgreement :one
INSERT INTO user_agreements (user_id, tenant_id, document, version, ip_address, user_agent)
VALUES (@user_id, @tenant_id, @document, @version, @ip_address, @user_agent)
RETURNING *;

-- name: ListUserAgreements :many
SELECT * FROM user_agreements WHERE user_id = @user_id ORDER BY id;

-- name: ScrubUserAgreements :execrows
UPDATE user_agreements
SET ip_address = NULL, user_agent = NULL
WHERE user_id = @user_id AND (ip_address IS NOT NULL OR user_agent IS NOT NULL);
