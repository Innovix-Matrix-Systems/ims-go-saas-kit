package service

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// Grace and read-only (docs/guides/billing.md#grace-and-read-only).
//
// When a paid plan lapses with nothing to replace it, it keeps applying for
// a grace period (BILLING_GRACE_DAYS). After that the organization is
// read-only: it can sign in, read, export, pay and remove members, but the
// read-only middleware refuses other writes. Nobody is locked out.

// Standing is an organization's billing standing.
type Standing struct {
	// Subscription is the plan that applies: the current one, or the lapsed
	// one during its grace period (nil when none applies).
	Subscription *domain.Subscription
	// InGrace: Subscription lapsed and its grace period runs until
	// GraceEndsAt.
	InGrace     bool
	GraceEndsAt *time.Time
	// ReadOnly: a plan lapsed, its grace period is over and nothing replaced
	// it.
	ReadOnly bool
}

// standingOf works out a tenant's standing at now.
func standingOf(ctx context.Context, subscriptions domain.SubscriptionRepository, tenantID int64, grace time.Duration, now time.Time) (Standing, error) {
	current, found, err := subscriptions.GetActiveForTenant(ctx, tenantID)
	if err != nil {
		return Standing{}, err
	}
	if found {
		return Standing{Subscription: current}, nil
	}
	if grace > 0 {
		lapsed, found, err := subscriptions.GetLapsedForTenant(ctx, tenantID, now.Add(-grace))
		if err != nil {
			return Standing{}, err
		}
		if found && lapsed.ExpiresAt != nil {
			ends := lapsed.ExpiresAt.Add(grace)
			return Standing{Subscription: lapsed, InGrace: true, GraceEndsAt: &ends}, nil
		}
	}
	had, err := subscriptions.HadSubscription(ctx, tenantID)
	if err != nil {
		return Standing{}, err
	}
	return Standing{ReadOnly: had}, nil
}

// WithGrace sets how long a lapsed paid plan keeps applying
// (BILLING_GRACE_DAYS). Zero: a lapse makes the organization read-only at
// once.
func (g *Guard) WithGrace(grace time.Duration) *Guard {
	g.grace = grace
	return g
}

// Standing reports the tenant's billing standing now.
func (g *Guard) Standing(ctx context.Context, tenantID int64) (Standing, error) {
	return standingOf(ctx, g.subscriptions, tenantID, g.grace, time.Now())
}

// ReadOnly reports whether the tenant is read-only (a plan lapsed beyond its
// grace period). The read-only middleware asks it on every write.
func (g *Guard) ReadOnly(ctx context.Context, tenantID int64) (bool, error) {
	standing, err := g.Standing(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return standing.ReadOnly, nil
}

// WithGrace sets the grace period Status reports (BILLING_GRACE_DAYS).
func (s *SubscriptionService) WithGrace(grace time.Duration) *SubscriptionService {
	s.grace = grace
	return s
}
