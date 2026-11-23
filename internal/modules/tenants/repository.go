package tenants

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Repository is the persistence contract required by the tenants module.
// It is defined here (consumer side) and implemented over sqlc queries.
type Repository interface {
	Get(ctx context.Context, id int64) (*Tenant, error)
	GetByCode(ctx context.Context, code string) (*Tenant, error)
	List(ctx context.Context, filter ListFilter) ([]Tenant, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, input CreateInput) (*Tenant, error)
	CreateWithID(ctx context.Context, input CreateInput) (*Tenant, error)
	Update(ctx context.Context, input UpdateInput) (*Tenant, error)
	Delete(ctx context.Context, id int64, actorID *int64) error
	SyncIDSequence(ctx context.Context) error
}

// ListFilter carries list/search/pagination input.
type ListFilter struct {
	Search string
	Limit  int32
	Offset int32
}

// CreateInput carries tenant creation input.
type CreateInput struct {
	ID       int64
	Name     string
	Code     string
	Slug     *string
	Domain   *string
	Phone    *string
	Email    *string
	IsActive bool
	ActorID  *int64
}

// UpdateInput carries tenant update input.
type UpdateInput struct {
	ID       int64
	Name     string
	Slug     *string
	Domain   *string
	Phone    *string
	Email    *string
	IsActive bool
	ActorID  *int64
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository implementation.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Get(ctx context.Context, id int64) (*Tenant, error) {
	row, err := r.queries.GetTenant(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Tenant with ID '%d' was not found.", id)
		}
		return nil, database.MapError("tenants.get", err)
	}
	tenant := mapTenant(row)
	return &tenant, nil
}

func (r *repository) GetByCode(ctx context.Context, code string) (*Tenant, error) {
	row, err := r.queries.GetTenantByCode(ctx, code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Tenant with code '%s' was not found.", code)
		}
		return nil, database.MapError("tenants.get_by_code", err)
	}
	tenant := mapTenant(row)
	return &tenant, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]Tenant, error) {
	rows, err := r.queries.ListTenants(ctx, dbgen.ListTenantsParams{
		Search:     optionalString(filter.Search),
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("tenants.list", err)
	}
	items := make([]Tenant, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapTenant(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.CountTenants(ctx, optionalString(filter.Search))
	if err != nil {
		return 0, database.MapError("tenants.count", err)
	}
	return total, nil
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*Tenant, error) {
	row, err := r.queries.CreateTenant(ctx, dbgen.CreateTenantParams{
		Name:      input.Name,
		Code:      input.Code,
		Slug:      input.Slug,
		Domain:    input.Domain,
		Phone:     input.Phone,
		Email:     input.Email,
		IsActive:  input.IsActive,
		CreatedBy: input.ActorID,
		UpdatedBy: input.ActorID,
	})
	if err != nil {
		return nil, database.MapError("tenants.create", err)
	}
	tenant := mapTenant(row)
	return &tenant, nil
}

func (r *repository) CreateWithID(ctx context.Context, input CreateInput) (*Tenant, error) {
	row, err := r.queries.CreateTenantWithID(ctx, dbgen.CreateTenantWithIDParams{
		ID:        input.ID,
		Name:      input.Name,
		Code:      input.Code,
		Slug:      input.Slug,
		Domain:    input.Domain,
		Phone:     input.Phone,
		Email:     input.Email,
		IsActive:  input.IsActive,
		CreatedBy: input.ActorID,
		UpdatedBy: input.ActorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, database.MapError("tenants.create_with_id", err)
	}
	tenant := mapTenant(row)
	return &tenant, nil
}

func (r *repository) SyncIDSequence(ctx context.Context) error {
	if err := r.queries.SyncTenantIDSequence(ctx); err != nil {
		return database.MapError("tenants.sync_sequence", err)
	}
	return nil
}

func (r *repository) Update(ctx context.Context, input UpdateInput) (*Tenant, error) {
	row, err := r.queries.UpdateTenant(ctx, dbgen.UpdateTenantParams{
		ID:        input.ID,
		Name:      input.Name,
		Slug:      input.Slug,
		Domain:    input.Domain,
		Phone:     input.Phone,
		Email:     input.Email,
		IsActive:  input.IsActive,
		UpdatedBy: input.ActorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Tenant with ID '%d' was not found.", input.ID)
		}
		return nil, database.MapError("tenants.update", err)
	}
	tenant := mapTenant(row)
	return &tenant, nil
}

func (r *repository) Delete(ctx context.Context, id int64, actorID *int64) error {
	affected, err := r.queries.SoftDeleteTenant(ctx, dbgen.SoftDeleteTenantParams{
		ID:        id,
		UpdatedBy: actorID,
	})
	if err != nil {
		return database.MapError("tenants.delete", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Tenant with ID '%d' was not found.", id)
	}
	return nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
