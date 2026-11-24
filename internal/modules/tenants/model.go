package tenants

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
)

// Tenant is the domain representation of a tenant.
type Tenant struct {
	ID       int64
	Name     string
	Code     string
	Slug     *string
	Domain   *string
	Phone    *string
	Email    *string
	IsActive bool
	// OwnerUserID is the organization's owner (nil: none recorded).
	OwnerUserID *int64
	CreatedBy   *int64
	UpdatedBy   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

func mapTenant(row dbgen.Tenant) Tenant {
	return Tenant{
		ID:          row.ID,
		Name:        row.Name,
		Code:        row.Code,
		Slug:        row.Slug,
		Domain:      row.Domain,
		Phone:       row.Phone,
		Email:       row.Email,
		IsActive:    row.IsActive,
		OwnerUserID: row.OwnerUserID,
		CreatedBy:   row.CreatedBy,
		UpdatedBy:   row.UpdatedBy,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
		DeletedAt:   timePtr(row.DeletedAt),
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
