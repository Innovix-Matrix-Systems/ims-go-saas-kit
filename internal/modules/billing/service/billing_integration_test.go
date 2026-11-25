package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	service "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	tenantsrepo "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

type billingHarness struct {
	tenantID      int64
	packages      *service.PackageService
	subscriptions *service.SubscriptionService
	payments      *service.PaymentService
	approvals     *service.ApprovalService
	guard         *service.Guard
	subsRepo      domain.SubscriptionRepository
}

func newBillingHarness(t *testing.T) *billingHarness {
	t.Helper()
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"payment_approval_requests", "payments", "subscriptions",
		"package_features", "packages",
		"settings", "data_processing_jobs", "cache",
		"personal_access_tokens", "role_has_permissions", "model_has_roles",
		"users", "roles", "permissions", "branches", "tenants",
	)

	tenantRepo := tenantsrepo.NewRepository(queries)
	tenant, err := tenantRepo.Create(context.Background(), tenantsrepo.CreateInput{
		Name: "Billing Test Tenant", Code: "billing-test", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	store := memory.New("billing-")
	t.Cleanup(store.Close)

	packagesRepo := domain.NewPackageRepository(queries)
	subsRepo := domain.NewSubscriptionRepository(queries)
	paymentsRepo := domain.NewPaymentRepository(queries)
	approvalsRepo := domain.NewApprovalRepository(queries)
	tx := database.NewTxManager(&database.Conn{Name: "primary", Pool: pool})

	orchestrator := billing.NewOrchestrator(paymentsRepo, "BDT").
		Register(domain.MethodCash, billing.CashDriver{}).
		Register(domain.MethodBankTransfer, billing.BankTransferDriver{}).
		Register(domain.MethodBkash, billing.NewBkashDriver(serviceTestBkashConfig(), store))

	return &billingHarness{
		tenantID:      tenant.ID,
		packages:      service.NewPackageService(packagesRepo).WithTx(tx),
		subscriptions: service.NewSubscriptionService(subsRepo, packagesRepo, paymentsRepo, approvalsRepo, orchestrator).WithTx(tx),
		payments:      service.NewPaymentService(paymentsRepo, "BDT"),
		approvals:     service.NewApprovalService(approvalsRepo, paymentsRepo, subsRepo, tx),
		guard:         service.NewGuard(subsRepo, packagesRepo),
		subsRepo:      subsRepo,
	}
}

func serviceTestBkashConfig() config.Bkash {
	return config.Bkash{
		BaseURL:   "https://bkash.invalid",
		AppKey:    "key",
		AppSecret: "secret",
		Username:  "user",
		Password:  "pass",
	}
}

func (h *billingHarness) createPackage(t *testing.T) *domain.Package {
	t.Helper()
	pkg, err := h.packages.Create(context.Background(), domain.PackageInput{
		Name: "Pro", Price: 125050, BillingCycle: domain.CycleMonthly, IsActive: true,
	}, []domain.FeatureInput{
		{FeatureKey: string(domain.FeatureAPIAccess), IsEnabled: true},
		{FeatureKey: string(domain.FeatureUsersMax), IsEnabled: true, LimitValue: 2},
		{FeatureKey: string(domain.FeatureBranchesMax), IsEnabled: true, LimitValue: 1},
	})
	if err != nil {
		t.Fatalf("create package: %v", err)
	}
	return pkg
}

func TestSubscriptionLifecycleAndGuard(t *testing.T) {
	h := newBillingHarness(t)
	ctx := context.Background()

	pkg := h.createPackage(t)
	if pkg.Price != 125050 || len(pkg.Features) != 3 {
		t.Fatalf("package = %+v", pkg)
	}
	for _, feature := range pkg.Features {
		if feature.FeatureKey == string(domain.FeatureUsersMax) && feature.LimitValue != 2 {
			t.Fatalf("users.max feature = %+v", feature)
		}
	}

	// No subscription yet: features are denied with SUBSCRIPTION_REQUIRED.
	if allowed, err := h.guard.Can(ctx, h.tenantID, domain.FeatureAPIAccess); err != nil || allowed {
		t.Fatalf("Can without subscription = %v, %v", allowed, err)
	}
	if err := h.guard.Require(ctx, h.tenantID, domain.FeatureAPIAccess); !apperr.Is(err, apperr.SubscriptionRequired) {
		t.Fatalf("Require without subscription = %v, want SUBSCRIPTION_REQUIRED", err)
	}

	// Cash subscription: pending subscription + pending payment + approval.
	subscription, payment, err := h.subscriptions.Subscribe(ctx, h.tenantID, pkg.ID, domain.MethodCash, map[string]any{"notes": "paid in cash"}, 1)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if subscription.Status != domain.StatusPending {
		t.Fatalf("subscription status = %q, want pending", subscription.Status)
	}
	if payment.Status != domain.PaymentPending || payment.Amount != pkg.Price || !strings.HasPrefix(payment.InvoiceNumber, "INV-") {
		t.Fatalf("payment = %+v", payment)
	}

	approvals, total, err := h.approvals.List(ctx, domain.ApprovalPending, 10, 0)
	if err != nil || total != 1 {
		t.Fatalf("approvals = %d, %v; want 1", total, err)
	}
	if approvals[0].PaymentMethod != domain.MethodCash {
		t.Fatalf("approval method = %q", approvals[0].PaymentMethod)
	}

	// Still pending → features remain denied.
	if allowed, _ := h.guard.Can(ctx, h.tenantID, domain.FeatureAPIAccess); allowed {
		t.Fatal("feature allowed while subscription pending")
	}

	// Approve → subscription active, payment completed.
	approved, err := h.approvals.Approve(ctx, approvals[0].ID, 1, strPtr("ok"))
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if approved.Status != domain.ApprovalApproved {
		t.Fatalf("approval status = %q", approved.Status)
	}

	status, err := h.subscriptions.Status(ctx, h.tenantID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Active || status.Subscription.Status != domain.StatusActive {
		t.Fatalf("status = %+v", status)
	}
	tenantPayments, paymentTotal, err := h.payments.ListForTenant(ctx, h.tenantID, 10, 0)
	if err != nil || paymentTotal != 1 || tenantPayments[0].Status != domain.PaymentCompleted {
		t.Fatalf("payments = %+v, %v", tenantPayments, err)
	}

	// Feature guard: flags, limits, 402 metadata.
	if allowed, _ := h.guard.Can(ctx, h.tenantID, domain.FeatureAPIAccess); !allowed {
		t.Fatal("api.access denied after activation")
	}
	limit, err := h.guard.Limit(ctx, h.tenantID, domain.FeatureUsersMax)
	if err != nil || limit != 2 {
		t.Fatalf("users.max limit = %d, %v", limit, err)
	}
	if ok, _ := h.guard.WithinLimit(ctx, h.tenantID, domain.FeatureUsersMax, 1); !ok {
		t.Fatal("within limit at 1")
	}
	if ok, _ := h.guard.WithinLimit(ctx, h.tenantID, domain.FeatureUsersMax, 2); ok {
		t.Fatal("within limit at 2 (should be exhausted)")
	}
	limitErr := h.guard.RequireWithinLimit(ctx, h.tenantID, domain.FeatureUsersMax, 2)
	if !apperr.Is(limitErr, apperr.UsageLimitExceeded) {
		t.Fatalf("RequireWithinLimit error = %v", limitErr)
	}
	var appErr *apperr.Error
	if !errors.As(limitErr, &appErr) {
		t.Fatal("error is not *apperr.Error")
	}
	if appErr.Meta["feature_key"] != string(domain.FeatureUsersMax) || appErr.Meta["limit"] != int64(2) || appErr.Meta["current"] != int64(2) {
		t.Fatalf("problem meta = %+v", appErr.Meta)
	}

	// Overrides raise the limit.
	if _, err := h.subscriptions.ApplyOverrides(ctx, status.Subscription.ID, map[string]any{"users.max": 10}); err != nil {
		t.Fatalf("ApplyOverrides() error = %v", err)
	}
	if overrideLimit, _ := h.guard.Limit(ctx, h.tenantID, domain.FeatureUsersMax); overrideLimit != 10 {
		t.Fatalf("override limit = %d, want 10", overrideLimit)
	}
	if _, err := h.subscriptions.ApplyOverrides(ctx, status.Subscription.ID, map[string]any{"nope.nope": true}); err == nil {
		t.Fatal("ApplyOverrides(unknown key) = nil error")
	}

	// Reject flow cancels the subscription and fails the payment.
	secondSubscription, secondPayment, err := h.subscriptions.Subscribe(ctx, h.tenantID, pkg.ID, domain.MethodBankTransfer, map[string]any{"reference_number": "BANK-1"}, 1)
	if err != nil {
		t.Fatalf("Subscribe(bank) error = %v", err)
	}
	pending, _, err := h.approvals.List(ctx, domain.ApprovalPending, 10, 0)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending approvals = %v, %v", pending, err)
	}
	if _, err := h.approvals.Reject(ctx, pending[0].ID, 1, strPtr("invalid transfer")); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	reloadedSub, err := h.subsRepo.Get(ctx, secondSubscription.ID)
	if err != nil || reloadedSub.Status != domain.StatusCancelled {
		t.Fatalf("rejected subscription = %+v, %v", reloadedSub, err)
	}
	reloadedPayment, err := h.payments.GetForTenant(ctx, h.tenantID, secondPayment.ID)
	if err != nil || reloadedPayment.Status != domain.PaymentFailed {
		t.Fatalf("rejected payment = %+v, %v", reloadedPayment, err)
	}

	// Expiry task.
	expires := time.Now().Add(-time.Hour)
	startedAt := time.Now().AddDate(0, -2, 0)
	past, err := h.subsRepo.Create(ctx, domain.SubscriptionInput{
		TenantID: h.tenantID, PackageID: pkg.ID, Status: domain.StatusActive,
		StartsAt: &startedAt, ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("create overdue subscription: %v", err)
	}
	expired, err := h.subscriptions.ExpireOverdue(ctx)
	if err != nil || expired < 1 {
		t.Fatalf("ExpireOverdue() = %d, %v", expired, err)
	}
	overdue, err := h.subsRepo.Get(ctx, past.ID)
	if err != nil || overdue.Status != domain.StatusExpired {
		t.Fatalf("overdue subscription = %+v, %v", overdue, err)
	}

	// Renewal reminders.
	soon := time.Now().AddDate(0, 0, 3)
	reminderStart := time.Now().AddDate(0, 0, -27)
	if _, err := h.subsRepo.Create(ctx, domain.SubscriptionInput{
		TenantID: h.tenantID, PackageID: pkg.ID, Status: domain.StatusActive,
		StartsAt: &reminderStart, ExpiresAt: &soon,
	}); err != nil {
		t.Fatalf("create expiring subscription: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sent, err := h.subscriptions.SendRenewalReminders(ctx, logger, []int{7, 3, 1})
	if err != nil || sent < 1 {
		t.Fatalf("SendRenewalReminders() = %d, %v", sent, err)
	}
}

func strPtr(value string) *string {
	return &value
}
