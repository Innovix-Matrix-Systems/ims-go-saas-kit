package featureflags

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// FeatureFlag is one stored flag row.
type FeatureFlag struct {
	ID         int64
	Key        string
	TargetType *string
	TargetID   *int64
	Value      bool
	Rollout    *int16 // percentage of users/tenants enabled; nil = everyone
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Repository is the persistence contract for feature flags.
type Repository interface {
	ListGlobal(ctx context.Context) ([]FeatureFlag, error)
	ListByTarget(ctx context.Context, target Target) ([]FeatureFlag, error)
	UpsertGlobal(ctx context.Context, key string, value bool, rollout *int16, actorID *int64) (*FeatureFlag, error)
	UpsertTarget(ctx context.Context, key string, target Target, value bool, rollout *int16, actorID *int64) (*FeatureFlag, error)
	InsertGlobalIfAbsent(ctx context.Context, key string, value bool) (*FeatureFlag, bool, error)
	Delete(ctx context.Context, key string, target Target) (bool, error)
	TenantExists(ctx context.Context, tenantID int64) (bool, error)
	UserTenant(ctx context.Context, userID int64) (int64, bool, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed feature-flag repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) ListGlobal(ctx context.Context) ([]FeatureFlag, error) {
	rows, err := r.queries.ListGlobalFeatureFlags(ctx)
	if err != nil {
		return nil, database.MapError("featureflags.list_global", err)
	}
	return mapFlags(rows), nil
}

func (r *repository) ListByTarget(ctx context.Context, target Target) ([]FeatureFlag, error) {
	rows, err := r.queries.ListFeatureFlagsByTarget(ctx, dbgen.ListFeatureFlagsByTargetParams{
		TargetType: target.TypePtr(),
		TargetID:   target.IDPtr(),
	})
	if err != nil {
		return nil, database.MapError("featureflags.list_target", err)
	}
	return mapFlags(rows), nil
}

func (r *repository) UpsertGlobal(ctx context.Context, key string, value bool, rollout *int16, actorID *int64) (*FeatureFlag, error) {
	row, err := r.queries.UpsertGlobalFeatureFlag(ctx, dbgen.UpsertGlobalFeatureFlagParams{
		Key:     key,
		Value:   value,
		Rollout: rollout,
		ActorID: actorID,
	})
	if err != nil {
		return nil, database.MapError("featureflags.upsert_global", err)
	}
	flag := mapFlag(row)
	return &flag, nil
}

func (r *repository) UpsertTarget(ctx context.Context, key string, target Target, value bool, rollout *int16, actorID *int64) (*FeatureFlag, error) {
	row, err := r.queries.UpsertTargetFeatureFlag(ctx, dbgen.UpsertTargetFeatureFlagParams{
		Key:        key,
		TargetType: target.TypePtr(),
		TargetID:   target.IDPtr(),
		Value:      value,
		Rollout:    rollout,
		ActorID:    actorID,
	})
	if err != nil {
		return nil, database.MapError("featureflags.upsert_target", err)
	}
	flag := mapFlag(row)
	return &flag, nil
}

func (r *repository) InsertGlobalIfAbsent(ctx context.Context, key string, value bool) (*FeatureFlag, bool, error) {
	row, err := r.queries.InsertGlobalFeatureFlagIfAbsent(ctx, dbgen.InsertGlobalFeatureFlagIfAbsentParams{
		Key:   key,
		Value: value,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, database.MapError("featureflags.insert_global", err)
	}
	flag := mapFlag(row)
	return &flag, true, nil
}

func (r *repository) Delete(ctx context.Context, key string, target Target) (bool, error) {
	affected, err := r.queries.DeleteFeatureFlag(ctx, dbgen.DeleteFeatureFlagParams{
		Key:        key,
		TargetType: target.TypePtr(),
		TargetID:   target.IDPtr(),
	})
	if err != nil {
		return false, database.MapError("featureflags.delete", err)
	}
	return affected > 0, nil
}

func (r *repository) TenantExists(ctx context.Context, tenantID int64) (bool, error) {
	_, err := r.queries.GetTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, database.MapError("featureflags.tenant_exists", err)
	}
	return true, nil
}

func (r *repository) UserTenant(ctx context.Context, userID int64) (int64, bool, error) {
	row, err := r.queries.GetUser(ctx, dbgen.GetUserParams{ID: userID, AllTenants: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, database.MapError("featureflags.user_tenant", err)
	}
	if row.TenantID == nil {
		return 0, true, nil
	}
	return *row.TenantID, true, nil
}

func mapFlag(row dbgen.FeatureFlag) FeatureFlag {
	return FeatureFlag{
		ID:         row.ID,
		Key:        row.Key,
		TargetType: row.TargetType,
		TargetID:   row.TargetID,
		Value:      row.Value,
		Rollout:    row.Rollout,
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}
}

func mapFlags(rows []dbgen.FeatureFlag) []FeatureFlag {
	flags := make([]FeatureFlag, 0, len(rows))
	for _, row := range rows {
		flags = append(flags, mapFlag(row))
	}
	return flags
}
