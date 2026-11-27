package service

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
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
	reports domain.ReportRepository
}

// NewUsageService creates the usage reader.
func NewUsageService(guard *Guard, reports domain.ReportRepository) *UsageService {
	return &UsageService{guard: guard, reports: reports}
}

// For returns the tenant's usage of users.max, branches.max and storage.mb
// (storage in whole megabytes, rounded up) against its current plan.
func (s *UsageService) For(ctx context.Context, tenantID int64) (*Usage, error) {
	row, err := s.reports.Usage(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	const mb = 1 << 20
	used := map[domain.Feature]int64{
		domain.FeatureUsersMax:    row.Users,
		domain.FeatureBranchesMax: row.Branches,
		domain.FeatureStorageMB:   (row.StorageBytes + mb - 1) / mb,
	}
	out := &Usage{StorageBytes: row.StorageBytes, Items: make([]UsageItem, 0, len(used))}
	for _, feature := range domain.Features {
		// Only limits with a count: a limit nothing counts yet (collections,
		// share links before the vault exists) or that is not a quantity
		// (audit.retention_days) would show a meaningless "0 of N". A module
		// that starts counting a limit adds it to used.
		if _, counted := used[feature]; !counted || !feature.IsLimit() {
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

// OverLimits lists the limits of a plan's features the tenant already
// exceeds (a downgrade keeps what is over, but nothing more can be added).
func (s *UsageService) OverLimits(ctx context.Context, tenantID int64, features []domain.PackageFeature) ([]UsageItem, error) {
	row, err := s.reports.Usage(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	const mb = 1 << 20
	used := map[domain.Feature]int64{
		domain.FeatureUsersMax:    row.Users,
		domain.FeatureBranchesMax: row.Branches,
		domain.FeatureStorageMB:   (row.StorageBytes + mb - 1) / mb,
	}
	limits := map[domain.Feature]int64{}
	for _, feature := range features {
		if !feature.IsEnabled {
			limits[domain.Feature(feature.FeatureKey)] = 0
			continue
		}
		limits[domain.Feature(feature.FeatureKey)] = feature.LimitValue
	}
	out := []UsageItem{}
	for _, feature := range domain.Features {
		if !feature.IsLimit() {
			continue
		}
		limit := limits[feature] // absent: 0, not in the plan
		if limit >= 0 && used[feature] > limit {
			out = append(out, UsageItem{Feature: feature, Limit: limit, Used: used[feature]})
		}
	}
	return out, nil
}
