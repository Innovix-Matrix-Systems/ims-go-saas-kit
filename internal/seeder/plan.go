package seeder

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
)

// DefaultPackageName is the hidden package the seeder gives the default
// tenant: every feature on, every limit unlimited.
const DefaultPackageName = "Unlimited"

// ensureDefaultSubscription gives the default tenant an active, non-expiring
// subscription to the hidden "Unlimited" package (created if missing), so a
// fresh install can add users and branches right away. It changes nothing
// when the tenant already has an active subscription.
func ensureDefaultSubscription(ctx context.Context, queries *dbgen.Queries, tenantID int64, logger *slog.Logger) error {
	subscriptions := domain.NewSubscriptionRepository(queries)
	if _, found, err := subscriptions.GetActiveForTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("look up default tenant subscription: %w", err)
	} else if found {
		return nil
	}

	packages := domain.NewPackageRepository(queries)
	pkg, err := ensureDefaultPackage(ctx, packages)
	if err != nil {
		return err
	}
	// An active subscription must expire in the future to count (billing
	// treats a NULL expiry as not current), so "never" is a century away.
	now := time.Now()
	expires := now.AddDate(100, 0, 0)
	if _, err := subscriptions.Create(ctx, domain.SubscriptionInput{
		TenantID:  tenantID,
		PackageID: pkg.ID,
		Status:    domain.StatusActive,
		StartsAt:  &now,
		ExpiresAt: &expires,
	}); err != nil {
		return fmt.Errorf("subscribe default tenant: %w", err)
	}
	logger.Info("default tenant subscribed", "tenant_id", tenantID, "package", pkg.Name)
	return nil
}

func ensureDefaultPackage(ctx context.Context, packages domain.PackageRepository) (*domain.Package, error) {
	existing, err := packages.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	for i := range existing {
		if existing[i].Name == DefaultPackageName {
			return &existing[i], nil
		}
	}
	description := "Every feature, no limits. Hidden from public pricing; assigned by the seeder to the default tenant."
	pkg, err := packages.Create(ctx, domain.PackageInput{
		Name:         DefaultPackageName,
		Description:  &description,
		Price:        0,
		BillingCycle: "one_time",
		IsActive:     false,
		SortOrder:    999,
	})
	if err != nil {
		return nil, fmt.Errorf("create default package: %w", err)
	}
	features := make([]domain.FeatureInput, 0, len(domain.Features))
	for _, feature := range domain.Features {
		features = append(features, domain.FeatureInput{FeatureKey: string(feature), IsEnabled: true, LimitValue: -1})
	}
	if err := packages.ReplaceFeatures(ctx, pkg.ID, features); err != nil {
		return nil, fmt.Errorf("set default package features: %w", err)
	}
	return pkg, nil
}
