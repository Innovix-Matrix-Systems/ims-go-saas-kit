package dev

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// Developer is an internal user holding the hidden Developer role.
type Developer struct {
	UserID       int64      `json:"user_id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	IsActive     bool       `json:"is_active"`
	IsSelf       bool       `json:"is_self"`
	ActiveTokens int64      `json:"active_tokens"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// DeveloperFilter filters the developer directory.
type DeveloperFilter struct {
	Search string
	Limit  int32
	Offset int32
}

// DeveloperRepository reads the developer directory. Account creation goes
// through identity.Service; role provisioning goes through access.RoleService.
type DeveloperRepository interface {
	List(ctx context.Context, filter DeveloperFilter) ([]Developer, int64, error)
	Count(ctx context.Context) (int64, error)
}

type developerRepository struct {
	queries *dbgen.Queries
}

// NewDeveloperRepository creates the sqlc-backed developer directory.
func NewDeveloperRepository(queries *dbgen.Queries) DeveloperRepository {
	return &developerRepository{queries: queries}
}

func (r *developerRepository) List(ctx context.Context, filter DeveloperFilter) ([]Developer, int64, error) {
	rows, err := r.queries.ListDevDevelopers(ctx, dbgen.ListDevDevelopersParams{
		RoleName:   authz.DeveloperRole,
		Search:     filter.Search,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, 0, database.MapError("dev.list_developers", err)
	}
	total, err := r.queries.CountDevDevelopers(ctx, dbgen.CountDevDevelopersParams{
		RoleName: authz.DeveloperRole,
		Search:   filter.Search,
	})
	if err != nil {
		return nil, 0, database.MapError("dev.count_developers", err)
	}
	items := make([]Developer, 0, len(rows))
	for _, row := range rows {
		items = append(items, Developer{
			UserID:       row.ID,
			Name:         row.Name,
			Email:        row.Email,
			IsActive:     row.IsActive == 1,
			ActiveTokens: row.ActiveTokens,
			LastLoginAt:  timePtr(row.LastLoginAt),
			CreatedAt:    row.CreatedAt.Time,
		})
	}
	return items, total, nil
}

func (r *developerRepository) Count(ctx context.Context) (int64, error) {
	total, err := r.queries.CountDevDevelopers(ctx, dbgen.CountDevDevelopersParams{
		RoleName: authz.DeveloperRole,
		Search:   "",
	})
	if err != nil {
		return 0, database.MapError("dev.count_developers", err)
	}
	return total, nil
}
