package service_test

import (
	"context"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	service "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/service"
)

func TestAssignPlan(t *testing.T) {
	h := newBillingHarness(t)
	ctx := context.Background()
	pkg := h.createPackage(t)

	// Cash: active at once, with a completed payment for the package price.
	first, payment, err := h.subscriptions.Assign(ctx, service.AssignInput{
		TenantID: h.tenantID, PackageID: pkg.ID, ActorID: 1, PaymentMethod: domain.MethodCash,
	})
	if err != nil {
		t.Fatalf("Assign(cash) error = %v", err)
	}
	if first.Status != domain.StatusActive || first.ExpiresAt == nil {
		t.Fatalf("subscription = %+v, want active with an expiry", first)
	}
	if payment == nil || payment.Status != domain.PaymentCompleted || payment.Amount != pkg.Price ||
		payment.SubscriptionID == nil || *payment.SubscriptionID != first.ID {
		t.Fatalf("payment = %+v, want completed for %d on subscription %d", payment, pkg.Price, first.ID)
	}
	if limit, err := h.guard.Limit(ctx, h.tenantID, domain.FeatureUsersMax); err != nil || limit != 2 {
		t.Fatalf("users.max limit = %d, %v; want 2", limit, err)
	}

	// Free of charge: no payment; the previous subscription is replaced.
	second, payment, err := h.subscriptions.Assign(ctx, service.AssignInput{
		TenantID: h.tenantID, PackageID: pkg.ID, ActorID: 1,
	})
	if err != nil || payment != nil {
		t.Fatalf("Assign(free) = %+v, %v; want no payment", payment, err)
	}
	status, err := h.subscriptions.Status(ctx, h.tenantID)
	if err != nil || status.Subscription == nil || status.Subscription.ID != second.ID || !status.Active {
		t.Fatalf("Status() = %+v, %v; want subscription %d active", status, err, second.ID)
	}
	old, err := h.subsRepo.Get(ctx, first.ID)
	if err != nil || old.Status == domain.StatusActive {
		t.Fatalf("previous subscription = %+v, %v; want it no longer active", old, err)
	}

	// A custom amount is recorded as given.
	amount := int64(5000)
	_, payment, err = h.subscriptions.Assign(ctx, service.AssignInput{
		TenantID: h.tenantID, PackageID: pkg.ID, ActorID: 1, PaymentMethod: domain.MethodBankTransfer, Amount: &amount,
	})
	if err != nil || payment == nil || payment.Amount != amount {
		t.Fatalf("Assign(bank_transfer, 5000) payment = %+v, %v", payment, err)
	}

	// Invalid input.
	for name, input := range map[string]service.AssignInput{
		"gateway method":        {TenantID: h.tenantID, PackageID: pkg.ID, PaymentMethod: domain.MethodBkash},
		"amount without method": {TenantID: h.tenantID, PackageID: pkg.ID, Amount: &amount},
		"unknown package":       {TenantID: h.tenantID, PackageID: 999999},
	} {
		if _, _, err := h.subscriptions.Assign(ctx, input); err == nil {
			t.Errorf("%s: Assign() error = nil, want an error", name)
		}
	}
}

func TestOverridesNullRemoves(t *testing.T) {
	h := newBillingHarness(t)
	ctx := context.Background()
	pkg := h.createPackage(t)
	subscription, _, err := h.subscriptions.Assign(ctx, service.AssignInput{TenantID: h.tenantID, PackageID: pkg.ID, ActorID: 1})
	if err != nil {
		t.Fatalf("Assign() error = %v", err)
	}

	updated, err := h.subscriptions.ApplyOverrides(ctx, subscription.ID, map[string]any{
		string(domain.FeatureUsersMax): float64(5), string(domain.FeatureReportsExport): true,
	})
	if err != nil || len(updated.Overrides) != 2 {
		t.Fatalf("ApplyOverrides(set) = %+v, %v", updated, err)
	}
	if limit, _ := h.guard.Limit(ctx, h.tenantID, domain.FeatureUsersMax); limit != 5 {
		t.Fatalf("users.max with override = %d, want 5", limit)
	}

	// null removes only that key; the plan's value applies again.
	updated, err = h.subscriptions.ApplyOverrides(ctx, subscription.ID, map[string]any{string(domain.FeatureUsersMax): nil})
	if err != nil {
		t.Fatalf("ApplyOverrides(null) error = %v", err)
	}
	if _, still := updated.Overrides[string(domain.FeatureUsersMax)]; still || len(updated.Overrides) != 1 {
		t.Fatalf("overrides after null = %v, want only reports.export", updated.Overrides)
	}
	if limit, _ := h.guard.Limit(ctx, h.tenantID, domain.FeatureUsersMax); limit != 2 {
		t.Fatalf("users.max after reset = %d, want the plan's 2", limit)
	}

	if _, err := h.subscriptions.ApplyOverrides(ctx, subscription.ID, map[string]any{"nope.feature": nil}); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("unknown key error = %v, want VALIDATION_FAILED", err)
	}
}
