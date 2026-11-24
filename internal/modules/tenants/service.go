package tenants

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"time"

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

// BranchProvisioner provisions/removes the branches belonging to a tenant.
type BranchProvisioner interface {
	CreateDefaultForTenant(ctx context.Context, tenantID int64) error
	DeleteForTenant(ctx context.Context, tenantID int64) error
}

// RoleProvisioner provisions the default roles for a new tenant.
type RoleProvisioner interface {
	CreateDefaultRolesForTenant(ctx context.Context, tenantID int64) error
}

// Service implements tenant business rules.
type Service struct {
	repo      Repository
	branches  BranchProvisioner
	roles     RoleProvisioner
	tx        *database.TxManager
	cache     cache.Store
	cfg       config.Tenancy
	activity  *activity.Recorder
	ownership *OwnershipCollaborators
}

// NewService creates the tenant service.
func NewService(repo Repository, branches BranchProvisioner, roles RoleProvisioner, tx *database.TxManager, store cache.Store, cfg config.Tenancy) *Service {
	return &Service{repo: repo, branches: branches, roles: roles, tx: tx, cache: store, cfg: cfg}
}

// WithActivity records tenant suspensions and reactivations in the activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// List returns tenants with pagination.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]Tenant, int64, error) {
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

// Get returns one tenant.
func (s *Service) Get(ctx context.Context, id int64) (*Tenant, error) {
	return s.repo.Get(ctx, id)
}

// Create adds a tenant and provisions its default branch and roles.
func (s *Service) Create(ctx context.Context, actorID int64, input CreateInput) (_ *Tenant, err error) {
	ctx, end := tracing.Begin(ctx, "tenants.create")
	defer end(&err)
	input.Code = generateCode(input.Name, actorID)
	input.ActorID = &actorID

	tenant, err := s.repo.Create(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := s.branches.CreateDefaultForTenant(ctx, tenant.ID); err != nil {
		return nil, err
	}
	if err := s.roles.CreateDefaultRolesForTenant(ctx, tenant.ID); err != nil {
		return nil, err
	}
	return tenant, nil
}

// Update modifies a tenant. Setting is_active to false suspends it: its users
// are refused from their next request (authentication reads the tenant's
// state on every request), and setting it back reactivates it.
func (s *Service) Update(ctx context.Context, actorID, id int64, input UpdateInput) (_ *Tenant, err error) {
	ctx, end := tracing.Begin(ctx, "tenants.update", attribute.Int64("tenant.id", id))
	defer end(&err)
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	input.ID = id
	input.ActorID = &actorID
	tenant, err := s.repo.Update(ctx, input)
	if err != nil {
		return nil, err
	}
	tenancy.InvalidateTenantCode(ctx, s.cache, tenant.Code)
	if current.IsActive != tenant.IsActive {
		s.recordSuspension(ctx, actorID, tenant)
	}
	return tenant, nil
}

// recordSuspension writes tenant.suspended / tenant.reactivated as a system
// row (no tenant): the platform did it to the tenant.
func (s *Service) recordSuspension(ctx context.Context, actorID int64, tenant *Tenant) {
	action := activity.ActionTenantReactivated
	if !tenant.IsActive {
		action = activity.ActionTenantSuspended
	}
	s.activity.Record(ctx, activity.Entry{
		ActorID: actorID, Action: action, Subject: "tenant", SubjectID: tenant.ID,
		Changes: map[string]any{"name": tenant.Name, "code": tenant.Code, "is_active": tenant.IsActive},
	})
}

// Delete soft-deletes a tenant and its branches in one transaction; the seeded
// default tenant is protected. Cached tenant-existence is invalidated so
// impersonation against the deleted tenant fails immediately.
func (s *Service) Delete(ctx context.Context, actorID, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "tenants.delete", attribute.Int64("tenant.id", id))
	defer end(&err)
	if id == s.cfg.DefaultTenantID {
		return apperr.New(apperr.OperationNotAllowed, "The default tenant cannot be deleted.")
	}
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		if _, err := q.SoftDeleteBranchesForTenant(ctx, id); err != nil {
			return err
		}
		affected, err := q.SoftDeleteTenant(ctx, dbgen.SoftDeleteTenantParams{
			ID:        id,
			UpdatedBy: &actorID,
		})
		if err != nil {
			return err
		}
		if affected == 0 {
			return apperr.Newf(apperr.ResourceNotFound, "Tenant with ID '%d' was not found.", id)
		}
		return nil
	})
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return appErr
		}
		return database.MapError("tenants.delete", err)
	}
	tenancy.InvalidateTenantCheck(ctx, s.cache, id)
	tenancy.InvalidateTenantCode(ctx, s.cache, current.Code)
	return nil
}

func generateCode(name string, actorID int64) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s::%d::%d", name, actorID, time.Now().UnixNano())))
	return fmt.Sprintf("%x", sum)
}
