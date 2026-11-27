package seeder

import (
	"context"
	"fmt"
	"log/slog"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// plan is one of the sample public packages `ims db:seed` creates. Replace
// them with your product's plans (or edit them in the console: seeding again
// never overwrites a package that exists).
type plan struct {
	name        string
	description string
	price       int64 // minor units of the default currency (cents of USD on a new install)
	cycle       string
	yearly      int64 // yearly price in minor units (0: monthly only)
	sortOrder   int32
	limits      map[domain.Feature]int64
	switches    map[domain.Feature]bool
}

// Per-plan feature values. A limit of -1 is unlimited.
var (
	freeLimits = map[domain.Feature]int64{
		domain.FeatureUsersMax: 3, domain.FeatureBranchesMax: 1, domain.FeatureStorageMB: 100,
	}
	proLimits = map[domain.Feature]int64{
		domain.FeatureUsersMax: 25, domain.FeatureBranchesMax: 3, domain.FeatureStorageMB: 5120,
	}
	businessLimits = map[domain.Feature]int64{
		domain.FeatureUsersMax: 100, domain.FeatureBranchesMax: -1, domain.FeatureStorageMB: 51200,
	}
	freeSwitches = map[domain.Feature]bool{
		domain.FeaturePatientsManage: true, domain.FeatureAppointmentsManage: true, domain.FeatureReportsBasic: true,
	}
	proSwitches = map[domain.Feature]bool{
		domain.FeaturePatientsManage: true, domain.FeatureAppointmentsManage: true, domain.FeatureInvoicesManage: true,
		domain.FeatureReportsBasic: true, domain.FeatureReportsAdvanced: true, domain.FeatureReportsExport: true,
		domain.FeatureAuditLog: true,
	}
	businessSwitches = map[domain.Feature]bool{
		domain.FeaturePatientsManage: true, domain.FeatureAppointmentsManage: true, domain.FeatureInvoicesManage: true,
		domain.FeatureReportsBasic: true, domain.FeatureReportsAdvanced: true, domain.FeatureReportsExport: true,
		domain.FeatureAuditLog: true, domain.FeatureSMSNotifications: true, domain.FeatureAPIAccess: true,
		domain.FeatureCustomBranding: true,
	}
)

// Plans are the sample public packages: Free, then Pro and Business, each
// monthly or yearly (two months free). Free uses the one_time cycle so a free
// subscription never reaches the end of a period.
var Plans = []plan{
	{name: "Free", description: "For trying it out: up to 3 members.", price: 0, cycle: domain.CycleOneTime, sortOrder: 1, limits: freeLimits, switches: freeSwitches},
	{name: "Pro", description: "For growing teams: up to 25 members.", price: 1900, yearly: 19000, cycle: domain.CycleMonthly, sortOrder: 2, limits: proLimits, switches: proSwitches},
	{name: "Business", description: "For larger organizations: up to 100 members.", price: 4900, yearly: 49000, cycle: domain.CycleMonthly, sortOrder: 3, limits: businessLimits, switches: businessSwitches},
}

// features lists every catalog feature with this plan's value: limits as
// numbers, switches on or off. A feature the plan does not name is off.
func (p plan) features() []domain.FeatureInput {
	out := make([]domain.FeatureInput, 0, len(domain.Features))
	for _, feature := range domain.Features {
		if feature.IsLimit() {
			limit, ok := p.limits[feature]
			out = append(out, domain.FeatureInput{FeatureKey: string(feature), IsEnabled: ok, LimitValue: limit})
			continue
		}
		out = append(out, domain.FeatureInput{FeatureKey: string(feature), IsEnabled: p.switches[feature]})
	}
	return out
}

// SeedPlans creates each sample package that does not exist yet,
// matched by name. A package that exists is left alone, so prices and limits
// staff change in the console are never overwritten by seeding again. `ims
// db:seed` runs it after Run; Run alone (the test kit's base seed) does not.
func SeedPlans(ctx context.Context, queries *dbgen.Queries, logger *slog.Logger) error {
	packages := domain.NewPackageRepository(queries)
	existing, err := packages.List(ctx)
	if err != nil {
		return fmt.Errorf("list packages: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, pkg := range existing {
		have[pkg.Name] = true
	}
	for _, p := range Plans {
		if have[p.name] {
			continue
		}
		description := p.description
		pkg, err := packages.Create(ctx, domain.PackageInput{
			Name:         p.name,
			Description:  &description,
			Price:        p.price,
			BillingCycle: p.cycle,
			YearlyPrice:  p.yearlyPrice(),
			IsActive:     true,
			SortOrder:    p.sortOrder,
		})
		if err != nil {
			return fmt.Errorf("create package %s: %w", p.name, err)
		}
		if err := packages.ReplaceFeatures(ctx, pkg.ID, p.features()); err != nil {
			return fmt.Errorf("set package %s features: %w", p.name, err)
		}
		logger.Info("package created", "package", p.name, "price", p.price, "cycle", p.cycle)
	}
	return nil
}

// yearlyPrice is the plan's yearly price (nil: monthly only).
func (p plan) yearlyPrice() *int64 {
	if p.yearly == 0 {
		return nil
	}
	yearly := p.yearly
	return &yearly
}
