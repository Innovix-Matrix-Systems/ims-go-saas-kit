package activityexport

import (
	"context"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// Repository reads what an export adds to the feed's rows.
type Repository interface {
	// TenantNames maps organization ids to their names (unknown ids are left out).
	TenantNames(ctx context.Context, ids []int64) (map[int64]string, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) TenantNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	rows, err := r.queries.ListTenantsByIDs(ctx, ids)
	if err != nil {
		return nil, database.MapError("activity_export.tenant_names", err)
	}
	names := make(map[int64]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}
