package branches

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// Repository is the persistence contract for branches.
type Repository interface {
	Get(ctx context.Context, tenantID, id int64, allTenants bool) (*Branch, error)
	List(ctx context.Context, filter ListFilter) ([]Branch, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, input CreateInput) (*Branch, error)
	CreateWithID(ctx context.Context, input CreateInput) (*Branch, error)
	Update(ctx context.Context, input UpdateInput) (*Branch, error)
	Delete(ctx context.Context, tenantID, id int64, actorID *int64) error
	CountInTenant(ctx context.Context, tenantID int64) (int64, error)
	CountMembers(ctx context.Context, tenantID, id int64) (int64, error)
	BelongsToTenant(ctx context.Context, tenantID, id int64) (bool, error)
	SoftDeleteForTenant(ctx context.Context, tenantID int64) error
	SyncIDSequence(ctx context.Context) error
}

// ListFilter carries branch list filters.
type ListFilter struct {
	TenantID   int64
	AllTenants bool
	Search     string
	OrderBy    string
	OrderDir   string
	Limit      int32
	Offset     int32
}

// CreateInput carries branch creation input.
type CreateInput struct {
	ID          int64
	TenantID    int64
	Name        string
	Address     *string
	Description *string
	Phone       *string
	Email       *string
	ManagerID   *int64
	ActorID     *int64
}

// UpdateInput carries branch update input.
type UpdateInput struct {
	ID          int64
	TenantID    int64
	Name        string
	Address     *string
	Description *string
	Phone       *string
	Email       *string
	ManagerID   *int64
	ActorID     *int64
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed branch repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Get(ctx context.Context, tenantID, id int64, allTenants bool) (*Branch, error) {
	row, err := r.queries.GetBranch(ctx, dbgen.GetBranchParams{ID: id, TenantID: tenantID, AllTenants: allTenants})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Branch with ID '%d' was not found.", id)
		}
		return nil, database.MapError("branches.get", err)
	}
	branch := mapBranch(row)
	return &branch, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]Branch, error) {
	rows, err := r.queries.ListBranches(ctx, dbgen.ListBranchesParams{
		TenantID:   filter.TenantID,
		AllTenants: filter.AllTenants,
		Search:     optionalString(filter.Search),
		OrderBy:    filter.OrderBy,
		OrderDir:   filter.OrderDir,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("branches.list", err)
	}
	items := make([]Branch, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapBranch(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.CountBranches(ctx, dbgen.CountBranchesParams{
		TenantID:   filter.TenantID,
		AllTenants: filter.AllTenants,
		Search:     optionalString(filter.Search),
	})
	if err != nil {
		return 0, database.MapError("branches.count", err)
	}
	return total, nil
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*Branch, error) {
	row, err := r.queries.CreateBranch(ctx, dbgen.CreateBranchParams{
		TenantID:    input.TenantID,
		Name:        input.Name,
		Address:     input.Address,
		Description: input.Description,
		Phone:       input.Phone,
		Email:       input.Email,
		ManagerID:   input.ManagerID,
		CreatedBy:   input.ActorID,
		UpdatedBy:   input.ActorID,
	})
	if err != nil {
		return nil, database.MapError("branches.create", err)
	}
	branch := mapBranch(row)
	return &branch, nil
}

func (r *repository) CreateWithID(ctx context.Context, input CreateInput) (*Branch, error) {
	row, err := r.queries.CreateBranchWithID(ctx, dbgen.CreateBranchWithIDParams{
		ID:          input.ID,
		TenantID:    input.TenantID,
		Name:        input.Name,
		Address:     input.Address,
		Description: input.Description,
		Phone:       input.Phone,
		Email:       input.Email,
		ManagerID:   input.ManagerID,
		CreatedBy:   input.ActorID,
		UpdatedBy:   input.ActorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, database.MapError("branches.create_with_id", err)
	}
	branch := mapBranch(row)
	return &branch, nil
}

func (r *repository) Update(ctx context.Context, input UpdateInput) (*Branch, error) {
	row, err := r.queries.UpdateBranch(ctx, dbgen.UpdateBranchParams{
		ID:          input.ID,
		TenantID:    input.TenantID,
		Name:        input.Name,
		Address:     input.Address,
		Description: input.Description,
		Phone:       input.Phone,
		Email:       input.Email,
		ManagerID:   input.ManagerID,
		UpdatedBy:   input.ActorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Branch with ID '%d' was not found.", input.ID)
		}
		return nil, database.MapError("branches.update", err)
	}
	branch := mapBranch(row)
	return &branch, nil
}

func (r *repository) Delete(ctx context.Context, tenantID, id int64, actorID *int64) error {
	affected, err := r.queries.SoftDeleteBranch(ctx, dbgen.SoftDeleteBranchParams{
		ID:        id,
		TenantID:  tenantID,
		UpdatedBy: actorID,
	})
	if err != nil {
		return database.MapError("branches.delete", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Branch with ID '%d' was not found.", id)
	}
	// Members who had switched into the branch go back to their own.
	if _, err := r.queries.ClearActiveBranch(ctx, dbgen.ClearActiveBranchParams{TenantID: &tenantID, BranchID: &id}); err != nil {
		return database.MapError("branches.clear_active", err)
	}
	return nil
}

func (r *repository) CountInTenant(ctx context.Context, tenantID int64) (int64, error) {
	count, err := r.queries.CountTenantBranches(ctx, tenantID)
	if err != nil {
		return 0, database.MapError("branches.count_in_tenant", err)
	}
	return count, nil
}

func (r *repository) CountMembers(ctx context.Context, tenantID, id int64) (int64, error) {
	count, err := r.queries.CountBranchMembers(ctx, dbgen.CountBranchMembersParams{TenantID: &tenantID, BranchID: &id})
	if err != nil {
		return 0, database.MapError("branches.count_members", err)
	}
	return count, nil
}

func (r *repository) BelongsToTenant(ctx context.Context, tenantID, id int64) (bool, error) {
	belongs, err := r.queries.BranchBelongsToTenant(ctx, dbgen.BranchBelongsToTenantParams{
		BranchID: id,
		TenantID: tenantID,
	})
	if err != nil {
		return false, database.MapError("branches.belongs", err)
	}
	return belongs, nil
}

func (r *repository) SoftDeleteForTenant(ctx context.Context, tenantID int64) error {
	if _, err := r.queries.SoftDeleteBranchesForTenant(ctx, tenantID); err != nil {
		return database.MapError("branches.delete_for_tenant", err)
	}
	return nil
}

func (r *repository) SyncIDSequence(ctx context.Context) error {
	if err := r.queries.SyncBranchIDSequence(ctx); err != nil {
		return database.MapError("branches.sync_sequence", err)
	}
	return nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
