package branches

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
)

// Branch is the domain representation of a branch.
type Branch struct {
	ID          int64
	TenantID    int64
	Name        string
	Address     *string
	Description *string
	Phone       *string
	Email       *string
	ManagerID   *int64
	CreatedBy   *int64
	UpdatedBy   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

func mapBranch(row dbgen.Branch) Branch {
	return Branch{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Name:        row.Name,
		Address:     row.Address,
		Description: row.Description,
		Phone:       row.Phone,
		Email:       row.Email,
		ManagerID:   row.ManagerID,
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
