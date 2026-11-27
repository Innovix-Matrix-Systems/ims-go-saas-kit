package service

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// Guard resolves feature flags and limits for a tenant's active subscription.
type Guard struct {
	subscriptions domain.SubscriptionRepository
	packages      domain.PackageRepository
	// grace keeps a lapsed paid plan applying for a while (standing.go).
	grace time.Duration
}

// NewGuard creates the feature guard.
func NewGuard(subscriptions domain.SubscriptionRepository, packages domain.PackageRepository) *Guard {
	return &Guard{subscriptions: subscriptions, packages: packages}
}

type planContext struct {
	subscription *domain.Subscription
	features     map[string]domain.PackageFeature
}

func (g *Guard) planFor(ctx context.Context, tenantID int64) (*planContext, error) {
	// The current plan, or a lapsed one during its grace period.
	standing, err := g.Standing(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	subscription := standing.Subscription
	if subscription == nil {
		return nil, nil
	}
	features, err := g.packages.FeaturesFor(ctx, subscription.PackageID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]domain.PackageFeature, len(features))
	for _, feature := range features {
		byKey[feature.FeatureKey] = feature
	}
	return &planContext{subscription: subscription, features: byKey}, nil
}

// HasActiveSubscription reports whether the tenant has a live plan.
func (g *Guard) HasActiveSubscription(ctx context.Context, tenantID int64) (bool, error) {
	plan, err := g.planFor(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return plan != nil, nil
}

// Can reports whether the tenant's plan grants a feature.
func (g *Guard) Can(ctx context.Context, tenantID int64, feature domain.Feature) (bool, error) {
	plan, err := g.planFor(ctx, tenantID)
	if err != nil {
		return false, err
	}
	if plan == nil {
		return false, nil
	}
	if value, ok := plan.subscription.Overrides[string(feature)]; ok {
		return overrideAllows(value), nil
	}
	definition, ok := plan.features[string(feature)]
	if !ok {
		return false, nil
	}
	return definition.IsEnabled, nil
}

// Require returns a 402 error unless the plan grants the feature.
func (g *Guard) Require(ctx context.Context, tenantID int64, feature domain.Feature) error {
	plan, err := g.planFor(ctx, tenantID)
	if err != nil {
		return err
	}
	if plan == nil {
		return apperr.New(apperr.SubscriptionRequired, "An active subscription is required.").
			WithMeta(map[string]any{"feature_key": string(feature)})
	}
	allowed, err := g.Can(ctx, tenantID, feature)
	if err != nil {
		return err
	}
	if !allowed {
		return apperr.New(apperr.FeatureNotAvailable, "This feature is not available on the current plan.").
			WithMeta(map[string]any{"feature_key": string(feature)})
	}
	return nil
}

// Limit resolves the numeric limit for a feature (0 = blocked, -1 = unlimited).
func (g *Guard) Limit(ctx context.Context, tenantID int64, feature domain.Feature) (int64, error) {
	plan, err := g.planFor(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	return plan.limit(feature), nil
}

// limit resolves a feature's limit from an already-loaded plan (nil = no
// subscription = blocked).
func (plan *planContext) limit(feature domain.Feature) int64 {
	if plan == nil {
		return 0
	}
	if value, ok := plan.subscription.Overrides[string(feature)]; ok {
		if number, ok := overrideNumber(value); ok {
			return number
		}
		if enabled, isBool := value.(bool); isBool && enabled {
			return -1
		}
	}
	if definition, ok := plan.features[string(feature)]; ok {
		return definition.LimitValue
	}
	return 0
}

// WithinLimit reports whether current usage is below the feature limit.
func (g *Guard) WithinLimit(ctx context.Context, tenantID int64, feature domain.Feature, current int64) (bool, error) {
	limit, err := g.Limit(ctx, tenantID, feature)
	if err != nil {
		return false, err
	}
	if limit == -1 {
		return true, nil
	}
	return current < limit, nil
}

// RequireWithinLimit returns a 402 error when the plan limit is exhausted.
func (g *Guard) RequireWithinLimit(ctx context.Context, tenantID int64, feature domain.Feature, current int64) error {
	check, err := g.quota(ctx, tenantID, feature)
	if err != nil {
		return err
	}
	return check(current)
}

// QuotaFor reads the tenant's plan once and returns a check that only
// compares numbers. Resource services call it before taking their
// per-tenant lock, so nothing inside the lock needs a second connection
// (which deadlocks once concurrent creates hold the whole pool).
func (g *Guard) QuotaFor(ctx context.Context, tenantID int64, feature string) (func(current int64) error, error) {
	typed := domain.Feature(feature)
	if !typed.IsKnown() {
		return nil, apperr.Newf(apperr.InvalidInput, "Unknown feature %q.", feature)
	}
	return g.quota(ctx, tenantID, typed)
}

func (g *Guard) quota(ctx context.Context, tenantID int64, feature domain.Feature) (func(current int64) error, error) {
	plan, err := g.planFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, apperr.New(apperr.SubscriptionRequired, "An active subscription is required.").
			WithMeta(map[string]any{"feature_key": string(feature)})
	}
	limit := plan.limit(feature)
	return func(current int64) error {
		if limit == -1 || current < limit {
			return nil
		}
		return apperr.New(apperr.UsageLimitExceeded, "The plan limit for this feature has been reached.").
			WithMeta(map[string]any{
				"feature_key": string(feature),
				"limit":       limit,
				"current":     current,
			})
	}, nil
}

// RequireStorage enforces storage.mb (megabytes) for an upload of addBytes on
// top of usedBytes. It only applies when the tenant's plan (or an override)
// defines storage.mb; -1 means unlimited.
func (g *Guard) RequireStorage(ctx context.Context, tenantID, usedBytes, addBytes int64) error {
	plan, err := g.planFor(ctx, tenantID)
	if err != nil || plan == nil {
		return err
	}
	key := string(domain.FeatureStorageMB)
	_, overridden := plan.subscription.Overrides[key]
	if _, defined := plan.features[key]; !defined && !overridden {
		return nil
	}
	limit, err := g.Limit(ctx, tenantID, domain.FeatureStorageMB)
	if err != nil || limit == -1 {
		return err
	}
	const mb = 1 << 20
	if usedBytes+addBytes <= limit*mb {
		return nil
	}
	return apperr.New(apperr.UsageLimitExceeded, "The storage limit of your plan has been reached.").
		WithMeta(map[string]any{
			"feature_key": key,
			"limit":       limit,
			"current":     (usedBytes + mb - 1) / mb,
		})
}

func overrideAllows(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed > 0
	case int:
		return typed > 0
	case int64:
		return typed > 0
	default:
		return false
	}
}

func overrideNumber(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	default:
		return 0, false
	}
}
