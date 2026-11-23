package branches

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// UserChecker verifies user existence inside a tenant (manager validation).
type UserChecker interface {
	ExistsInTenant(ctx context.Context, tenantID, userID int64) (bool, error)
}

// LimitChecker enforces billing plan limits for tenant resources.
type LimitChecker interface {
	// QuotaFor reads the plan limit (before any lock) and returns a check
	// that only compares numbers.
	QuotaFor(ctx context.Context, tenantID int64, feature string) (func(current int64) error, error)
}

// Service implements branch business rules.
type Service struct {
	repo     Repository
	users    UserChecker
	cache    cache.Store
	cfg      config.Tenancy
	limits   LimitChecker
	tx       *database.TxManager
	activity *activity.Recorder
}

// WithActivity records branch changes in the activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

func (input CreateParams) changes() map[string]any {
	changes := map[string]any{"name": input.Name}
	for key, value := range map[string]*string{
		"address": input.Address, "description": input.Description, "phone": input.Phone, "email": input.Email,
	} {
		if value != nil {
			changes[key] = *value
		}
	}
	if input.ManagerID != nil {
		changes["manager_id"] = *input.ManagerID
	}
	return changes
}

// NewService creates the branch service.
func NewService(repo Repository, users UserChecker, store cache.Store, cfg config.Tenancy, limits LimitChecker) *Service {
	return &Service{repo: repo, users: users, cache: store, cfg: cfg, limits: limits}
}

// WithTx enables lock-protected quota checks on create (branches.max).
func (s *Service) WithTx(tx *database.TxManager) *Service {
	s.tx = tx
	return s
}

// List returns branches for the effective tenant with pagination.
func (s *Service) List(ctx context.Context, scope tenancy.Scope, filter ListFilter) ([]Branch, int64, error) {
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

// Get returns one branch inside the effective tenant (or any tenant for a
// global read).
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*Branch, error) {
	if scope.AllTenants() {
		return s.repo.Get(ctx, 0, id, true)
	}
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	return s.repo.Get(ctx, tenantID, id, false)
}

// CreateParams carries branch creation input from handlers.
type CreateParams struct {
	Name        string
	Address     *string
	Description *string
	Phone       *string
	Email       *string
	ManagerID   *int64
}

// Create adds a branch to the effective tenant.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateParams) (*Branch, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	// An example span (phase 26.9): the request's trace shows this step and
	// the queries under it. A no-op while tracing is off.
	ctx, span := tracing.Start(ctx, "branches.create", attribute.Int64("tenant.id", tenantID))
	defer span.End()
	if err := s.validateManager(ctx, tenantID, input.ManagerID); err != nil {
		return nil, err
	}
	var branch *Branch
	err := s.withBranchQuota(ctx, tenantID, func(repo Repository) error {
		created, err := repo.Create(ctx, CreateInput{
			TenantID:    tenantID,
			Name:        input.Name,
			Address:     input.Address,
			Description: input.Description,
			Phone:       input.Phone,
			Email:       input.Email,
			ManagerID:   input.ManagerID,
			ActorID:     &actorID,
		})
		branch = created
		return err
	})
	if err != nil {
		return nil, tracing.Fail(ctx, err)
	}
	span.SetAttributes(attribute.Int64("branch.id", branch.ID))
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "branch.created",
		Subject: "branch", SubjectID: branch.ID, Changes: input.changes(),
	})
	return branch, nil
}

// withBranchQuota runs create under the tenant's branches.max limit; with a
// TxManager the count and insert share a per-tenant advisory lock.
func (s *Service) withBranchQuota(ctx context.Context, tenantID int64, create func(repo Repository) error) error {
	// The plan limit is read before the lock; inside it only the count runs,
	// on the locked transaction.
	var allow func(current int64) error // nil: no limit to enforce
	if s.limits != nil {
		quota, err := s.limits.QuotaFor(ctx, tenantID, "branches.max")
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
	if s.tx == nil {
		if err := check(s.repo); err != nil {
			return err
		}
		return create(s.repo)
	}
	return s.tx.WithTenantLock(ctx, "branches.max", tenantID, func(q *dbgen.Queries) error {
		repo := NewRepository(q)
		if err := check(repo); err != nil {
			return err
		}
		return create(repo)
	})
}

// Update modifies a branch inside the effective tenant.
func (s *Service) Update(ctx context.Context, scope tenancy.Scope, actorID, id int64, input CreateParams) (*Branch, error) {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	ctx, span := tracing.Start(ctx, "branches.update",
		attribute.Int64("tenant.id", tenantID), attribute.Int64("branch.id", id))
	defer span.End()
	if err := s.validateManager(ctx, tenantID, input.ManagerID); err != nil {
		return nil, err
	}
	branch, err := s.repo.Update(ctx, UpdateInput{
		ID:          id,
		TenantID:    tenantID,
		Name:        input.Name,
		Address:     input.Address,
		Description: input.Description,
		Phone:       input.Phone,
		Email:       input.Email,
		ManagerID:   input.ManagerID,
		ActorID:     &actorID,
	})
	if err != nil {
		return nil, tracing.Fail(ctx, err)
	}
	tenancy.InvalidateBranchCheck(ctx, s.cache, tenantID, id)
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "branch.updated",
		Subject: "branch", SubjectID: id, Changes: input.changes(),
	})
	return branch, nil
}

// Delete soft-deletes a branch, protecting the configured default branch.
func (s *Service) Delete(ctx context.Context, scope tenancy.Scope, actorID, id int64) error {
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	if id == s.cfg.DefaultBranchID {
		return apperr.New(apperr.OperationNotAllowed, "The default branch cannot be deleted.")
	}
	if err := s.repo.Delete(ctx, tenantID, id, &actorID); err != nil {
		return err
	}
	tenancy.InvalidateBranchCheck(ctx, s.cache, tenantID, id)
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "branch.deleted", Subject: "branch", SubjectID: id,
	})
	return nil
}

// CreateDefaultForTenant provisions a default branch for a new tenant using an
// auto-assigned id (the fixed DEFAULT_BRANCH_ID is reserved for the seeded
// default tenant).
func (s *Service) CreateDefaultForTenant(ctx context.Context, tenantID int64) error {
	branch, err := s.repo.Create(ctx, CreateInput{
		TenantID: tenantID,
		Name:     s.cfg.DefaultBranchName,
	})
	if err != nil {
		return err
	}
	tenancy.InvalidateBranchCheck(ctx, s.cache, tenantID, branch.ID)
	return nil
}

// DeleteForTenant soft-deletes every branch of a tenant.
func (s *Service) DeleteForTenant(ctx context.Context, tenantID int64) error {
	return s.repo.SoftDeleteForTenant(ctx, tenantID)
}

func (s *Service) validateManager(ctx context.Context, tenantID int64, managerID *int64) error {
	if managerID == nil {
		return nil
	}
	exists, err := s.users.ExistsInTenant(ctx, tenantID, *managerID)
	if err != nil {
		return err
	}
	if !exists {
		return apperr.Validation(map[string][]string{
			"manager_id": {"The selected manager does not belong to the tenant."},
		})
	}
	return nil
}
