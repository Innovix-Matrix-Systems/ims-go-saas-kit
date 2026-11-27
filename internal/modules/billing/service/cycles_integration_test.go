package service_test

import (
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	service "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
)

// Monthly and yearly on one plan, and grace then read-only
// (docs/guides/billing.md#monthly-and-yearly, #grace-and-read-only).

// team is a monthly plan at 3,000 that is also offered yearly at 30,000.
func (h *changesHarness) team() *domain.Package {
	h.t.Helper()
	yearly := int64(30000)
	pkg, err := h.packages.Create(h.ctx, domain.PackageInput{
		Name: "Team", Price: 3000, BillingCycle: domain.CycleMonthly, YearlyPrice: &yearly, IsActive: true,
	}, []domain.FeatureInput{{FeatureKey: string(domain.FeatureUsersMax), IsEnabled: true, LimitValue: 10}})
	if err != nil {
		h.t.Fatalf("create Team: %v", err)
	}
	return pkg
}

func TestYearlyPriceOnlyOnMonthlyPlans(t *testing.T) {
	h := newChangesHarness(t)
	yearly := int64(100)
	_, err := h.packages.Create(h.ctx, domain.PackageInput{
		Name: "Odd", Price: 50, BillingCycle: domain.CycleYearly, YearlyPrice: &yearly, IsActive: true,
	}, nil)
	if !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("yearly price on a yearly plan: err = %v, want a validation error", err)
	}
	if _, err := h.subscriptions.Preview(h.ctx, h.tenant("cyc0"), h.basic.ID, domain.CycleYearly); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("Basic has no yearly price: preview err = %v, want a validation error", err)
	}
}

func TestAssignYearly(t *testing.T) {
	h := newChangesHarness(t)
	team := h.team()
	tenant := h.tenant("cyc1")
	subscription, payment, err := h.subscriptions.Assign(h.ctx, service.AssignInput{
		TenantID: tenant, PackageID: team.ID, ActorID: 1, PaymentMethod: domain.MethodCash, Cycle: domain.CycleYearly,
	})
	if err != nil {
		t.Fatalf("Assign(yearly) error = %v", err)
	}
	if subscription.BillingCycle == nil || *subscription.BillingCycle != domain.CycleYearly {
		t.Fatalf("billing_cycle = %v, want yearly", subscription.BillingCycle)
	}
	if payment == nil || payment.Amount != 30000 {
		t.Fatalf("payment = %+v, want the yearly price 30000", payment)
	}
	if subscription.ExpiresAt == nil || subscription.ExpiresAt.Before(time.Now().AddDate(0, 11, 0)) {
		t.Fatalf("expires_at = %v, want about a year out", subscription.ExpiresAt)
	}
}

// Monthly → yearly of the same plan starts now and credits the unused days;
// the same cycle again is a renewal.
func TestMonthlyToYearlyIsAnUpgrade(t *testing.T) {
	h := newChangesHarness(t)
	team := h.team()
	tenant := h.tenant("cyc2")
	h.paidPlan(tenant, team, 3000, 10, 30)

	change, err := h.subscriptions.Preview(h.ctx, tenant, team.ID, domain.CycleYearly)
	if err != nil {
		t.Fatalf("Preview(yearly) error = %v", err)
	}
	q := change.Quote
	if q.Kind != domain.ChangeUpgrade || q.Effective != domain.EffectiveNow || q.Price != 30000 || q.Credit != 2000 || q.AmountDue != 28000 {
		t.Fatalf("monthly → yearly = %s/%s price %d credit %d due %d; want an upgrade now: 30000 − 2000 = 28000",
			q.Kind, q.Effective, q.Price, q.Credit, q.AmountDue)
	}
	same, err := h.subscriptions.Preview(h.ctx, tenant, team.ID, "")
	if err != nil {
		t.Fatalf("Preview(monthly) error = %v", err)
	}
	if same.Quote.Kind != domain.ChangeRenew || same.Quote.Price != 3000 {
		t.Fatalf("monthly → monthly = %s at %d, want a renewal at 3000", same.Quote.Kind, same.Quote.Price)
	}
}

// Yearly → monthly of the same plan waits for the period end, recorded as the
// next plan and cycle; buying the yearly cycle again then renews it.
func TestYearlyToMonthlyWaitsForThePeriodEnd(t *testing.T) {
	h := newChangesHarness(t)
	team := h.team()
	tenant := h.tenant("cyc3")
	if _, _, err := h.subscriptions.Assign(h.ctx, service.AssignInput{
		TenantID: tenant, PackageID: team.ID, ActorID: 1, PaymentMethod: domain.MethodCash, Cycle: domain.CycleYearly,
	}); err != nil {
		t.Fatalf("Assign(yearly) error = %v", err)
	}

	change, err := h.subscriptions.Preview(h.ctx, tenant, team.ID, domain.CycleMonthly)
	if err != nil {
		t.Fatalf("Preview(monthly) error = %v", err)
	}
	if change.Quote.Kind != domain.ChangeDowngrade || change.Quote.Effective != domain.EffectivePeriodEnd || change.Quote.DueAtRenewal != 3000 {
		t.Fatalf("yearly → monthly = %s/%s due at renewal %d; want a downgrade at the period end, 3000",
			change.Quote.Kind, change.Quote.Effective, change.Quote.DueAtRenewal)
	}
	result, err := h.subscriptions.CheckoutAt(h.ctx, tenant, team.ID, domain.CycleMonthly, domain.MethodBankTransfer, nil, 1)
	if err != nil {
		t.Fatalf("CheckoutAt(monthly) error = %v", err)
	}
	next := result.Subscription
	if result.Payment != nil || next.NextPackageID == nil || *next.NextPackageID != team.ID ||
		next.NextBillingCycle == nil || *next.NextBillingCycle != domain.CycleMonthly {
		t.Fatalf("after choosing monthly: payment %v, next %v/%v; want nothing paid, Team monthly at the renewal",
			result.Payment, next.NextPackageID, next.NextBillingCycle)
	}
	renew, err := h.subscriptions.Preview(h.ctx, tenant, team.ID, domain.CycleMonthly)
	if err != nil || renew.Quote.Kind != domain.ChangeRenew {
		t.Fatalf("the chosen next cycle again = %v, %v; want a renewal", renew, err)
	}
}

// A lapsed paid plan keeps applying during the grace period, then the
// organization is read-only; paying lifts it.
func TestGraceThenReadOnly(t *testing.T) {
	h := newChangesHarness(t)
	guard := h.guard.WithGrace(14 * 24 * time.Hour)
	subscriptions := h.subscriptions.WithGrace(14 * 24 * time.Hour)
	tenant := h.tenant("cyc4")

	never, err := guard.Standing(h.ctx, tenant)
	if err != nil || never.ReadOnly {
		t.Fatalf("a tenant that never had a plan: %+v, %v; want not read-only", never, err)
	}

	// Lapsed three days ago: still Basic's limits, not read-only.
	subscription := h.paidPlan(tenant, h.basic, 1000, 33, 30)
	standing, err := guard.Standing(h.ctx, tenant)
	if err != nil || !standing.InGrace || standing.ReadOnly || standing.GraceEndsAt == nil {
		t.Fatalf("three days after the lapse: %+v, %v; want in grace", standing, err)
	}
	if limit, err := guard.Limit(h.ctx, tenant, domain.FeatureUsersMax); err != nil || limit != 3 {
		t.Fatalf("users.max in grace = %d, %v; want Basic's 3", limit, err)
	}

	// Lapsed twenty days ago: read-only, no plan applies.
	h.setPeriod(subscription.ID, -50*24, -20*24)
	if readOnly, err := guard.ReadOnly(h.ctx, tenant); err != nil || !readOnly {
		t.Fatalf("twenty days after the lapse: read-only = %v, %v; want true", readOnly, err)
	}
	status, err := subscriptions.Status(h.ctx, tenant)
	if err != nil || !status.ReadOnly || status.InGrace {
		t.Fatalf("status = %+v, %v; want read-only", status, err)
	}

	// Paying for a plan lifts it at once.
	h.paidPlan(tenant, h.pro, 3000, 0, 30)
	if readOnly, err := guard.ReadOnly(h.ctx, tenant); err != nil || readOnly {
		t.Fatalf("after paying: read-only = %v, %v; want false", readOnly, err)
	}
}
