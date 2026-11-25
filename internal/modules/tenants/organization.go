package tenants

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// The organization is the caller's own tenant, seen from inside it: its
// members read the profile, admins edit it, and its owner can hand ownership
// to another admin.

// OwnershipCollaborators are what an ownership transfer checks.
type OwnershipCollaborators struct {
	// Passwords confirms the current owner's password.
	Passwords interface {
		ConfirmPasswordOnly(ctx context.Context, userID int64, password string) error
	}
	// Members tells whether a user is an active member of the tenant.
	Members interface {
		ActiveMember(ctx context.Context, tenantID, userID int64) (bool, error)
	}
	// Checker tells whether the new owner may manage the organization.
	Checker *authz.Checker
}

// WithOwnership enables ownership transfer.
func (s *Service) WithOwnership(collaborators OwnershipCollaborators) *Service {
	s.ownership = &collaborators
	return s
}

// OrganizationUpdate is what an organization's admins may change about it.
type OrganizationUpdate struct {
	Name  string
	Phone *string
	Email *string
}

func organizationID(scope tenancy.Scope) (int64, error) {
	id := scope.EffectiveTenantID()
	if id == 0 {
		return 0, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	return id, nil
}

// Organization returns the caller's organization and its owner.
func (s *Service) Organization(ctx context.Context, scope tenancy.Scope) (*Tenant, *Owner, error) {
	id, err := organizationID(scope)
	if err != nil {
		return nil, nil, err
	}
	tenant, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.repo.Owner(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return tenant, owner, nil
}

// UpdateOrganization changes the organization's name and contact details.
// Its code, slug, domain and status stay with platform staff.
func (s *Service) UpdateOrganization(ctx context.Context, scope tenancy.Scope, actorID int64, input OrganizationUpdate) (_ *Tenant, _ *Owner, err error) {
	ctx, end := tracing.Begin(ctx, "tenants.update_organization",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
	)
	defer end(&err)
	current, _, err := s.Organization(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	name := strings.TrimSpace(input.Name)
	if len([]rune(name)) < 2 {
		return nil, nil, apperr.Validation(map[string][]string{"name": {"The name must be at least 2 characters."}})
	}
	updated, err := s.repo.Update(ctx, UpdateInput{
		ID: current.ID, Name: name, Slug: current.Slug, Domain: current.Domain,
		Phone: input.Phone, Email: input.Email, IsActive: current.IsActive, ActorID: &actorID,
	})
	if err != nil {
		return nil, nil, err
	}
	tenancy.InvalidateTenantCode(ctx, s.cache, updated.Code)
	s.activity.Record(ctx, activity.Entry{
		TenantID: updated.ID, ActorID: actorID, Action: "organization.updated", Subject: "tenant", SubjectID: updated.ID,
		Changes: map[string]any{"name": updated.Name, "phone": updated.Phone, "email": updated.Email},
	})
	owner, err := s.repo.Owner(ctx, updated.ID)
	if err != nil {
		return nil, nil, err
	}
	return updated, owner, nil
}

// SetOwner records a tenant's owner (sign-up, staff-created organizations).
func (s *Service) SetOwner(ctx context.Context, tenantID, userID int64) (err error) {
	ctx, end := tracing.Begin(ctx, "tenants.set_owner",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("user.id", userID),
	)
	defer end(&err)
	return s.repo.SetOwner(ctx, tenantID, userID)
}

// TransferOwnership hands the organization to another active member who
// can manage it. Only the current owner may do it, with their password.
func (s *Service) TransferOwnership(ctx context.Context, scope tenancy.Scope, actorID, newOwnerID int64, password string) (_ *Tenant, _ *Owner, err error) {
	ctx, end := tracing.Begin(ctx, "tenants.transfer_ownership",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("user.id", newOwnerID),
	)
	defer end(&err)
	if s.ownership == nil {
		return nil, nil, apperr.New(apperr.OperationNotAllowed, "Ownership transfer is not available.")
	}
	tenant, owner, err := s.Organization(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	if owner == nil || owner.ID != actorID {
		return nil, nil, apperr.New(apperr.Forbidden, "Only the organization's owner can transfer ownership.")
	}
	if newOwnerID == actorID {
		return nil, nil, apperr.Validation(map[string][]string{"user_id": {"You already own this organization."}})
	}
	if err := s.ownership.Passwords.ConfirmPasswordOnly(ctx, actorID, password); err != nil {
		return nil, nil, err
	}
	member, err := s.ownership.Members.ActiveMember(ctx, tenant.ID, newOwnerID)
	if err != nil {
		return nil, nil, err
	}
	if !member {
		return nil, nil, apperr.Validation(map[string][]string{"user_id": {"The new owner must be an active member of this organization."}})
	}
	if can, err := s.ownership.Checker.Can(ctx, newOwnerID, authz.PermOrganizationUpdate); err != nil {
		return nil, nil, err
	} else if !can {
		return nil, nil, apperr.Validation(map[string][]string{"user_id": {"The new owner must be an administrator of this organization."}})
	}
	if err := s.repo.SetOwner(ctx, tenant.ID, newOwnerID); err != nil {
		return nil, nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenant.ID, ActorID: actorID, Action: "organization.ownership_transferred", Subject: "tenant", SubjectID: tenant.ID,
		Changes: map[string]any{"from_user_id": actorID, "to_user_id": newOwnerID},
	})
	newOwner, err := s.repo.Owner(ctx, tenant.ID)
	if err != nil {
		return nil, nil, err
	}
	return tenant, newOwner, nil
}
