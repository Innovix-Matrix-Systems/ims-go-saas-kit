package identity

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// LimitChecker enforces billing plan limits for tenant resources.
type LimitChecker interface {
	// QuotaFor reads the plan limit (before any lock) and returns a check
	// that only compares numbers.
	QuotaFor(ctx context.Context, tenantID int64, feature string) (func(current int64) error, error)
}

// Service implements user business rules.
type Service struct {
	repo       Repository
	roles      access.RoleRepository
	branchRepo branches.Repository
	checker    *authz.Checker
	auth       authn.Authenticator
	cfg        config.Auth
	limits     LimitChecker
	tx         *database.TxManager
	verifier   Verifier
	activity   *activity.Recorder
	twoFactor  *authn.TwoFactor
}

// WithTwoFactor lets admins reset a user's two-factor authentication.
func (s *Service) WithTwoFactor(twoFactor *authn.TwoFactor) *Service {
	s.twoFactor = twoFactor
	return s
}

// WithActivity records user changes in the activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// Verifier sends the e-mail verification link to a newly created user
// (implemented by authn.Recovery).
type Verifier interface {
	SendVerificationTo(ctx context.Context, userID int64) error
	// SendInvitation mails an invitation link (phase 26.5); inviter is the
	// name shown in the mail.
	SendInvitation(ctx context.Context, userID int64, inviter string) error
}

// WithVerifier sends verification mail to users created unverified
// (AUTH_NEW_USERS_VERIFIED=false).
func (s *Service) WithVerifier(verifier Verifier) *Service {
	s.verifier = verifier
	return s
}

// NewService creates the user service.
func NewService(repo Repository, roles access.RoleRepository, branchRepo branches.Repository, checker *authz.Checker, auth authn.Authenticator, cfg config.Auth, limits LimitChecker) *Service {
	return &Service{repo: repo, roles: roles, branchRepo: branchRepo, checker: checker, auth: auth, cfg: cfg, limits: limits}
}

// WithTx enables lock-protected quota checks on create. Without it (unit
// tests with fakes) the limit check still runs, just not atomically.
func (s *Service) WithTx(tx *database.TxManager) *Service {
	s.tx = tx
	return s
}

// List returns users within the effective scope.
func (s *Service) List(ctx context.Context, scope tenancy.Scope, filter ListFilter) ([]User, int64, error) {
	if scope.AllTenants() {
		filter.AllTenants = true
		filter.TenantID = 0
	} else {
		tenantID := scope.EffectiveTenantID()
		if tenantID == 0 {
			return nil, 0, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
		}
		filter.TenantID = tenantID
	}

	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Get returns one user visible in the effective scope.
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*User, error) {
	return s.repo.Get(ctx, scope.Filter(), id)
}

// CreateParams carries user creation input from handlers.
type CreateParams struct {
	TenantID  *int64
	BranchID  *int64
	FirstName *string
	LastName  *string
	UserName  *string
	Name      string
	Email     string
	Password  string
	Phone     *string
	IsActive  *bool
	Roles     []int64
	// SendInvitation creates the user inactive, without a usable password,
	// and mails an invitation: setting a password from it activates the
	// account.
	SendInvitation bool
}

// Create adds a user with role assignments.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateParams) (*User, error) {
	tenantID := scope.EffectiveTenantID()
	if scope.Internal {
		tenantID = 0
		if input.TenantID != nil {
			tenantID = *input.TenantID
		}
	}
	if !scope.Internal && tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	if input.SendInvitation {
		// Nobody knows this password: the invitee sets their own.
		unusable, err := randomPassword()
		if err != nil {
			return nil, err
		}
		inactive := false
		input.Password, input.IsActive = unusable, &inactive
	} else if input.Password == "" {
		return nil, apperr.Validation(map[string][]string{"password": {"The password field is required."}})
	}
	if err := s.validateEmailUnique(ctx, input.Email, 0); err != nil {
		return nil, err
	}
	if err := s.validatePhoneUnique(ctx, input.Phone, 0); err != nil {
		return nil, err
	}
	if scope.Internal && tenantID == 0 {
		if err := s.validateSystemRoles(ctx, input.Roles); err != nil {
			return nil, err
		}
	} else if err := s.validateRoles(ctx, tenantID, input.Roles); err != nil {
		return nil, err
	}

	branchID := input.BranchID
	if err := s.validateBranch(ctx, tenantID, branchID); err != nil {
		return nil, err
	}

	hash, err := s.hashPassword(input.Password)
	if err != nil {
		return nil, err
	}

	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	var tenant *int64
	if tenantID != 0 {
		tenant = &tenantID
	}
	activeBranch := branchID

	var verifiedAt time.Time
	if s.cfg.NewUsersVerified && !input.SendInvitation {
		verifiedAt = time.Now()
	}
	var invitedBy *int64
	if input.SendInvitation {
		invitedBy = &actorID
	}
	var user *User
	err = s.withUserQuota(ctx, tenantID, func(repo Repository) error {
		created, err := repo.Create(ctx, CreateInput{
			TenantID:        tenant,
			BranchID:        branchID,
			ActiveBranchID:  activeBranch,
			FirstName:       input.FirstName,
			LastName:        input.LastName,
			UserName:        input.UserName,
			Name:            input.Name,
			Email:           input.Email,
			Password:        hash,
			Phone:           input.Phone,
			IsActive:        isActive,
			EmailVerifiedAt: verifiedAt,
			ActorID:         &actorID,
			InvitedBy:       invitedBy,
		})
		if err != nil {
			return err
		}
		if len(input.Roles) > 0 {
			if err := repo.AssignRoles(ctx, created.ID, input.Roles); err != nil {
				return err
			}
		}
		user = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.checker.InvalidateUser(ctx, user.ID)
	if input.SendInvitation {
		s.invite(ctx, user, actorID, "user.invited")
	} else if verifiedAt.IsZero() && s.verifier != nil {
		// Best effort: the account exists either way and the user can ask
		// for a new link through POST /email/verification-notification.
		_ = s.verifier.SendVerificationTo(ctx, user.ID)
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: user.TenantIDValue(), ActorID: actorID, Action: "user.created",
		Subject: "user", SubjectID: user.ID,
		Changes: map[string]any{"name": user.Name, "email": user.Email, "roles": input.Roles},
	})
	return user, nil
}

// ResendInvitation mails a new invitation link (the old one stops working).
// It also invites an existing user who has never signed in, such as one
// created with a password nobody used. A user who joined is refused.
func (s *Service) ResendInvitation(ctx context.Context, scope tenancy.Scope, actorID, targetID int64) error {
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return err
	}
	if err := s.guardSuperAdmin(ctx, actorID, target.ID); err != nil {
		return err
	}
	invited, err := s.repo.MarkInvited(ctx, target.ID, &actorID)
	if err != nil {
		return err
	}
	if !invited {
		return apperr.New(apperr.InvalidState, "This user has already joined. Invitations are for accounts that have never been used.")
	}
	s.invite(ctx, target, actorID, "user.invitation_resent")
	return nil
}

// invite mails the invitation and records it. Sending is best effort: the
// user exists either way and the invitation can be sent again.
func (s *Service) invite(ctx context.Context, user *User, actorID int64, action string) {
	if s.verifier != nil {
		inviter := ""
		if actor, err := s.repo.Get(ctx, tenancy.Unscoped(), actorID); err == nil {
			inviter = actor.Name
		}
		_ = s.verifier.SendInvitation(ctx, user.ID, inviter)
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: user.TenantIDValue(), ActorID: actorID, Action: action,
		Subject: "user", SubjectID: user.ID, Changes: map[string]any{"email": user.Email},
	})
}

// withUserQuota runs create under the tenant's users.max plan limit. With a
// TxManager the count and the insert share one transaction holding a
// per-tenant advisory lock, so concurrent creates cannot exceed the quota.
func (s *Service) withUserQuota(ctx context.Context, tenantID int64, create func(repo Repository) error) error {
	// The plan limit is read before the lock; inside it only the count runs,
	// on the locked transaction.
	var allow func(current int64) error // nil: no limit to enforce
	if tenantID != 0 && s.limits != nil {
		quota, err := s.limits.QuotaFor(ctx, tenantID, "users.max")
		if err != nil {
			return err
		}
		allow = quota
	}
	check := func(repo Repository) error {
		if allow == nil {
			return nil
		}
		count, err := repo.Count(ctx, ListFilter{TenantID: tenantID})
		if err != nil {
			return err
		}
		return allow(count)
	}
	if tenantID == 0 || s.tx == nil {
		if err := check(s.repo); err != nil {
			return err
		}
		return create(s.repo)
	}
	return s.tx.WithTenantLock(ctx, "users.max", tenantID, func(q *dbgen.Queries) error {
		repo := NewRepository(q)
		if err := check(repo); err != nil {
			return err
		}
		return create(repo)
	})
}

// UpdateParams carries user update input from handlers.
type UpdateParams struct {
	FirstName *string
	LastName  *string
	Name      string
	Phone     *string
	IsActive  *bool
	BranchID  *int64
	Roles     []int64
	// ExpectedUpdatedAt enables optimistic locking (see UpdateInput).
	ExpectedUpdatedAt *time.Time
}

// Update modifies a user; super admins are protected from non-super actors.
func (s *Service) Update(ctx context.Context, scope tenancy.Scope, actorID, id int64, input UpdateParams) (*User, error) {
	target, err := s.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if input.Roles != nil && target.ID == actorID {
		return nil, apperr.New(apperr.Forbidden, "You cannot change your own roles.")
	}
	if err := s.guardSuperAdmin(ctx, actorID, target.ID); err != nil {
		return nil, err
	}

	if err := s.validatePhoneUnique(ctx, input.Phone, target.ID); err != nil {
		return nil, err
	}
	tenantID := target.TenantIDValue()
	if err := s.validateBranch(ctx, tenantID, input.BranchID); err != nil {
		return nil, err
	}
	if err := s.validateRoles(ctx, tenantID, input.Roles); err != nil {
		return nil, err
	}

	isActive := target.IsActive
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	branchID := input.BranchID
	if branchID == nil {
		branchID = target.BranchID
	}

	updated, err := s.repo.Update(ctx, UpdateInput{
		Tenant:    scope.Filter(),
		ID:        target.ID,
		FirstName: input.FirstName,
		LastName:  input.LastName,
		Name:      input.Name,
		Phone:     input.Phone,
		IsActive:  isActive,
		BranchID:  branchID,
		ActorID:   &actorID,

		ExpectedUpdatedAt: input.ExpectedUpdatedAt,
	})
	if err != nil {
		return nil, err
	}
	if input.Roles != nil {
		if err := s.repo.ClearRoles(ctx, target.ID); err != nil {
			return nil, err
		}
		if err := s.repo.AssignRoles(ctx, target.ID, input.Roles); err != nil {
			return nil, err
		}
	}
	s.checker.InvalidateUser(ctx, target.ID)
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "user.updated",
		Subject: "user", SubjectID: target.ID, Changes: input.changes(),
	})
	return updated, nil
}

// changes lists the fields an update sent, for the activity log.
func (input UpdateParams) changes() map[string]any {
	changes := map[string]any{"name": input.Name}
	if input.FirstName != nil {
		changes["first_name"] = *input.FirstName
	}
	if input.LastName != nil {
		changes["last_name"] = *input.LastName
	}
	if input.Phone != nil {
		changes["phone"] = *input.Phone
	}
	if input.IsActive != nil {
		changes["is_active"] = *input.IsActive
	}
	if input.BranchID != nil {
		changes["branch_id"] = *input.BranchID
	}
	if input.Roles != nil {
		changes["roles"] = input.Roles
	}
	return changes
}

// Delete soft-deletes a user, protecting super admins and self-deletion.
func (s *Service) Delete(ctx context.Context, scope tenancy.Scope, actorID, id int64) error {
	target, err := s.Get(ctx, scope, id)
	if err != nil {
		return err
	}
	if target.ID == actorID {
		return apperr.New(apperr.Forbidden, "You cannot delete your own account.")
	}
	targetIsSuper, err := s.checker.IsSuperAdmin(ctx, target.ID)
	if err != nil {
		return err
	}
	if targetIsSuper {
		return apperr.New(apperr.Forbidden, "The super admin user cannot be deleted.")
	}
	if err := s.repo.Delete(ctx, scope.Filter(), target.ID, &actorID); err != nil {
		return err
	}
	if err := s.auth.RevokeAll(ctx, target.ID); err != nil {
		return err
	}
	s.checker.InvalidateUser(ctx, target.ID)
	s.activity.Record(ctx, activity.Entry{
		TenantID: target.TenantIDValue(), ActorID: actorID, Action: "user.deleted",
		Subject: "user", SubjectID: target.ID,
		Changes: map[string]any{"name": target.Name, "email": target.Email},
	})
	return nil
}

// AssignRoles replaces a user's roles.
func (s *Service) AssignRoles(ctx context.Context, scope tenancy.Scope, actorID, targetID int64, roleIDs []int64) (*User, error) {
	if len(roleIDs) == 0 {
		return nil, apperr.Validation(map[string][]string{
			"roles": {"The roles field must have at least one item."},
		})
	}
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	if target.ID == actorID {
		return nil, apperr.New(apperr.Forbidden, "You cannot change your own roles.")
	}
	if err := s.guardSuperAdmin(ctx, actorID, target.ID); err != nil {
		return nil, err
	}
	if scope.Internal && target.TenantIDValue() == 0 {
		if err := s.validateSystemRoles(ctx, roleIDs); err != nil {
			return nil, err
		}
	} else if err := s.validateRoles(ctx, target.TenantIDValue(), roleIDs); err != nil {
		return nil, err
	}
	if err := s.repo.ClearRoles(ctx, target.ID); err != nil {
		return nil, err
	}
	if err := s.repo.AssignRoles(ctx, target.ID, roleIDs); err != nil {
		return nil, err
	}
	s.checker.InvalidateUser(ctx, target.ID)
	s.activity.Record(ctx, activity.Entry{
		TenantID: target.TenantIDValue(), ActorID: actorID, Action: "user.roles_assigned",
		Subject: "user", SubjectID: target.ID, Changes: map[string]any{"roles": roleIDs},
	})
	return target, nil
}

// GrantRole adds one role without replacing the user's existing roles.
func (s *Service) GrantRole(ctx context.Context, scope tenancy.Scope, targetID, roleID int64) (*User, error) {
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRoleForTarget(ctx, scope, target, roleID); err != nil {
		return nil, err
	}
	if err := s.repo.AddRole(ctx, target.ID, roleID); err != nil {
		return nil, err
	}
	s.checker.InvalidateUser(ctx, target.ID)
	return target, nil
}

// RevokeRole removes one role without touching the user's other roles.
func (s *Service) RevokeRole(ctx context.Context, scope tenancy.Scope, targetID, roleID int64) (*User, error) {
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRoleForTarget(ctx, scope, target, roleID); err != nil {
		return nil, err
	}
	removed, err := s.repo.RemoveRole(ctx, target.ID, roleID)
	if err != nil {
		return nil, err
	}
	if !removed {
		return nil, apperr.Newf(apperr.ResourceNotFound, "User '%d' does not hold role '%d'.", targetID, roleID)
	}
	s.checker.InvalidateUser(ctx, target.ID)
	return target, nil
}

func (s *Service) validateRoleForTarget(ctx context.Context, scope tenancy.Scope, target *User, roleID int64) error {
	if scope.Internal && target.TenantIDValue() == 0 {
		return s.validateSystemRoles(ctx, []int64{roleID})
	}
	return s.validateRoles(ctx, target.TenantIDValue(), []int64{roleID})
}

// ChangePassword sets a new password (admin flow) and revokes active tokens.
func (s *Service) ChangePassword(ctx context.Context, scope tenancy.Scope, actorID, targetID int64, password string) error {
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return err
	}
	if err := s.guardSuperAdmin(ctx, actorID, target.ID); err != nil {
		return err
	}
	hash, err := s.hashPassword(password)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, scope.Filter(), target.ID, hash, &actorID); err != nil {
		return err
	}
	if err := s.auth.RevokeAll(ctx, target.ID); err != nil {
		return err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: target.TenantIDValue(), ActorID: actorID, Action: "user.password_changed",
		Subject: "user", SubjectID: target.ID,
	})
	return nil
}

// ResetTwoFactor turns a user's two-factor authentication off, for a user
// who lost their phone or mailbox and their recovery codes. The user is
// mailed, and the reset is a user.two_factor_reset activity row. Their next
// sign-in needs only the password (or sets two-factor up again when the
// tenant requires it).
func (s *Service) ResetTwoFactor(ctx context.Context, scope tenancy.Scope, actorID, targetID int64) error {
	if s.twoFactor == nil {
		return apperr.New(apperr.OperationNotAllowed, "Two-factor authentication is not configured.")
	}
	target, err := s.Get(ctx, scope, targetID)
	if err != nil {
		return err
	}
	if err := s.guardSuperAdmin(ctx, actorID, target.ID); err != nil {
		return err
	}
	return s.twoFactor.Disable(ctx, target.toRecord(true), true)
}

// ProfileUpdate updates the authenticated user's own profile.
func (s *Service) ProfileUpdate(ctx context.Context, userID int64, input UpdateParams) (*User, error) {
	target, err := s.repo.Get(ctx, tenancy.Unscoped(), userID)
	if err != nil {
		return nil, err
	}
	if err := s.validatePhoneUnique(ctx, input.Phone, target.ID); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, UpdateInput{
		Tenant:    tenancy.Unscoped(),
		ID:        target.ID,
		FirstName: input.FirstName,
		LastName:  input.LastName,
		Name:      input.Name,
		Phone:     input.Phone,
		IsActive:  target.IsActive,
		BranchID:  target.BranchID,
		ActorID:   &userID,
	})
}

// ProfileChangePassword verifies the current password before updating, then
// signs out every other device; the current token keeps working.
func (s *Service) ProfileChangePassword(ctx context.Context, userID, currentTokenID int64, currentPassword, newPassword string) error {
	target, err := s.repo.Get(ctx, tenancy.Unscoped(), userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(target.Password), []byte(currentPassword)) != nil {
		return apperr.Validation(map[string][]string{
			"current_password": {"The provided current password is incorrect."},
		})
	}
	hash, err := s.hashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, tenancy.Unscoped(), userID, hash, &userID); err != nil {
		return err
	}
	return s.auth.RevokeOthers(ctx, userID, currentTokenID)
}

// SwitchBranch changes the user's active branch.
func (s *Service) SwitchBranch(ctx context.Context, scope tenancy.Scope, userID, branchID int64) (*User, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	belongs, err := s.branchRepo.BelongsToTenant(ctx, tenantID, branchID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, apperr.Validation(map[string][]string{
			"branch_id": {"The selected branch is invalid."},
		})
	}
	if err := s.repo.SwitchBranch(ctx, userID, branchID); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, tenancy.Unscoped(), userID)
}

// GetRecord loads a user without scope checks (internal consumers only).
func (s *Service) GetRecord(ctx context.Context, id int64) (*User, error) {
	return s.repo.Get(ctx, tenancy.Unscoped(), id)
}

// guardSuperAdmin is a business invariant, not a permission: only a
// super-admin may modify a super-admin account, whoever the caller is.
func (s *Service) guardSuperAdmin(ctx context.Context, actorID, targetID int64) error {
	targetIsSuper, err := s.checker.IsSuperAdmin(ctx, targetID)
	if err != nil || !targetIsSuper {
		return err
	}
	actorIsSuper, err := s.checker.IsSuperAdmin(ctx, actorID)
	if err != nil {
		return err
	}
	if !actorIsSuper {
		return apperr.New(apperr.Forbidden, "The super admin user cannot be modified.")
	}
	return nil
}

func (s *Service) validateEmailUnique(ctx context.Context, email string, excludeID int64) error {
	exists, err := s.repo.EmailExists(ctx, email, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return apperr.Validation(map[string][]string{"email": {"The email has already been taken."}})
	}
	return nil
}

func (s *Service) validatePhoneUnique(ctx context.Context, phone *string, excludeID int64) error {
	if phone == nil || *phone == "" {
		return nil
	}
	exists, err := s.repo.PhoneExists(ctx, *phone, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return apperr.Validation(map[string][]string{"phone": {"The phone has already been taken."}})
	}
	return nil
}

func (s *Service) validateRoles(ctx context.Context, tenantID int64, roleIDs []int64) error {
	if len(roleIDs) == 0 {
		return nil
	}
	if tenantID == 0 {
		return apperr.Validation(map[string][]string{"roles": {"Roles cannot be assigned without a tenant context."}})
	}
	for _, roleID := range roleIDs {
		exists, err := s.roles.ExistsForTenant(ctx, tenantID, roleID)
		if err != nil {
			return err
		}
		if !exists {
			return apperr.Validation(map[string][]string{"roles": {"One or more selected roles are invalid."}})
		}
	}
	return nil
}

// validateSystemRoles allows only global system roles (tenantless) to be
// assigned to a tenantless user, e.g. the super-admin from the internal API.
func (s *Service) validateSystemRoles(ctx context.Context, roleIDs []int64) error {
	for _, roleID := range roleIDs {
		role, err := s.roles.GetByID(ctx, roleID)
		if err != nil {
			return apperr.Validation(map[string][]string{"roles": {"One or more selected roles are invalid."}})
		}
		if !role.IsSystem || role.TenantID != nil {
			return apperr.Validation(map[string][]string{"roles": {"Only system roles can be assigned to a user without a tenant."}})
		}
	}
	return nil
}

func (s *Service) validateBranch(ctx context.Context, tenantID int64, branchID *int64) error {
	if branchID == nil {
		return nil
	}
	if tenantID == 0 {
		return apperr.Validation(map[string][]string{"branch_id": {"A branch cannot be assigned without a tenant context."}})
	}
	belongs, err := s.branchRepo.BelongsToTenant(ctx, tenantID, *branchID)
	if err != nil {
		return err
	}
	if !belongs {
		return apperr.Validation(map[string][]string{"branch_id": {"The selected branch is invalid."}})
	}
	return nil
}

// hashPassword applies the password policy, then hashes. Every path that sets
// a user's password (create, admin change, own change) goes through it.
func (s *Service) hashPassword(password string) (string, error) {
	if err := authn.ValidateNewPassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cfg.BcryptRounds)
	if err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "hash password", err)
	}
	return string(hash), nil
}
