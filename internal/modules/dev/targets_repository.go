package dev

import (
	"context"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// TargetOption is one tenant or user that can receive an override. The
// management panels (flags, settings) share this search surface.
type TargetOption struct {
	ID         int64   `json:"id"`
	Type       string  `json:"type"`
	Name       string  `json:"name"`
	Code       *string `json:"code,omitempty"`
	Email      *string `json:"email,omitempty"`
	TenantID   *int64  `json:"tenant_id,omitempty"`
	TenantName *string `json:"tenant_name,omitempty"`
	Overrides  int64   `json:"overrides"`
}

// TargetStats is the directory header payload for one target kind.
type TargetStats struct {
	Total         int64 `json:"total"`
	WithOverrides int64 `json:"with_overrides"`
}

// TargetRepository is the persistence contract for target search.
type TargetRepository interface {
	List(ctx context.Context, kind, search string, limit, offset int32) ([]TargetOption, int64, error)
	Stats(ctx context.Context, kind string) (TargetStats, error)
}

type targetRepository struct {
	queries *dbgen.Queries
}

// NewTargetRepository creates the sqlc-backed target search repository.
func NewTargetRepository(queries *dbgen.Queries) TargetRepository {
	return &targetRepository{queries: queries}
}

func (r *targetRepository) List(ctx context.Context, kind, search string, limit, offset int32) ([]TargetOption, int64, error) {
	switch kind {
	case "tenant":
		rows, err := r.queries.ListDevTenantTargets(ctx, dbgen.ListDevTenantTargetsParams{
			Search:     search,
			PageOffset: offset,
			PageLimit:  limit,
		})
		if err != nil {
			return nil, 0, database.MapError("dev.list_tenant_targets", err)
		}
		total, err := r.queries.CountDevTenantTargets(ctx, search)
		if err != nil {
			return nil, 0, database.MapError("dev.count_tenant_targets", err)
		}
		targets := make([]TargetOption, 0, len(rows))
		for _, row := range rows {
			code := row.Code
			targets = append(targets, TargetOption{
				ID:        row.ID,
				Type:      "tenant",
				Name:      row.Name,
				Code:      &code,
				Overrides: row.Overrides,
			})
		}
		return targets, total, nil
	case "user":
		rows, err := r.queries.ListDevUserTargets(ctx, dbgen.ListDevUserTargetsParams{
			Search:     search,
			PageOffset: offset,
			PageLimit:  limit,
		})
		if err != nil {
			return nil, 0, database.MapError("dev.list_user_targets", err)
		}
		total, err := r.queries.CountDevUserTargets(ctx, search)
		if err != nil {
			return nil, 0, database.MapError("dev.count_user_targets", err)
		}
		targets := make([]TargetOption, 0, len(rows))
		for _, row := range rows {
			email := row.Email
			targets = append(targets, TargetOption{
				ID:         row.ID,
				Type:       "user",
				Name:       row.Name,
				Email:      &email,
				TenantID:   row.TenantID,
				TenantName: row.TenantName,
				Overrides:  row.Overrides,
			})
		}
		return targets, total, nil
	default:
		return []TargetOption{}, 0, nil
	}
}

func (r *targetRepository) Stats(ctx context.Context, kind string) (TargetStats, error) {
	switch kind {
	case "tenant":
		total, err := r.queries.CountDevTenantTargets(ctx, "")
		if err != nil {
			return TargetStats{}, database.MapError("dev.count_tenant_targets", err)
		}
		withOverrides, err := r.queries.CountDevTenantTargetsWithOverrides(ctx)
		if err != nil {
			return TargetStats{}, database.MapError("dev.count_tenant_overrides", err)
		}
		return TargetStats{Total: total, WithOverrides: withOverrides}, nil
	case "user":
		total, err := r.queries.CountDevUserTargets(ctx, "")
		if err != nil {
			return TargetStats{}, database.MapError("dev.count_user_targets", err)
		}
		withOverrides, err := r.queries.CountDevUserTargetsWithOverrides(ctx)
		if err != nil {
			return TargetStats{}, database.MapError("dev.count_user_overrides", err)
		}
		return TargetStats{Total: total, WithOverrides: withOverrides}, nil
	default:
		return TargetStats{}, nil
	}
}
