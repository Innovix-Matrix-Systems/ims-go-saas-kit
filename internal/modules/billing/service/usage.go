package service

import (
	"context"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// UsageItem is one plan limit and how much of it a tenant uses.
type UsageItem struct {
	Feature domain.Feature
	// Limit follows Guard.Limit: -1 unlimited, 0 not in the plan.
	Limit int64
	Used  int64
}

// Usage reports a tenant's use of its plan limits.
type Usage struct {
	Items        []UsageItem
	StorageBytes int64
}

// UsageService reads what a tenant uses of its plan.
type UsageService struct {
	guard   *Guard
	queries *dbgen.Queries
}

// NewUsageService creates the usage reader.
func NewUsageService(guard *Guard, queries *dbgen.Queries) *UsageService {
	return &UsageService{guard: guard, queries: queries}
}

// For returns the tenant's usage of users.max, branches.max and storage.mb
// (storage in whole megabytes, rounded up) against its current plan.
func (s *UsageService) For(ctx context.Context, tenantID int64) (*Usage, error) {
	row, err := s.queries.TenantUsage(ctx, tenantID)
	if err != nil {
		return nil, database.MapError("billing.usage", err)
	}
	const mb = 1 << 20
	used := map[domain.Feature]int64{
		domain.FeatureUsersMax:    row.Users,
		domain.FeatureBranchesMax: row.Branches,
		domain.FeatureStorageMB:   (row.StorageBytes + mb - 1) / mb,
	}
	out := &Usage{StorageBytes: row.StorageBytes, Items: make([]UsageItem, 0, len(used))}
	for _, feature := range domain.Features {
		if !feature.IsLimit() {
			continue
		}
		limit, err := s.guard.Limit(ctx, tenantID, feature)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, UsageItem{Feature: feature, Limit: limit, Used: used[feature]})
	}
	return out, nil
}
