package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	service "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	tenantsrepo "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

// Plan changes, renewals, credits and refunds (docs/guides/billing.md#changing-plans)
// against a real database. Time moves by rewriting a period's dates.

type changesHarness struct {
	t             *testing.T
	ctx           context.Context
	pool          *pgxpool.Pool
	tenants       tenantsrepo.Repository
	packages      *service.PackageService
	subscriptions *service.SubscriptionService
	approvals     *service.ApprovalService
	guard         *service.Guard
	subsRepo      domain.SubscriptionRepository
	paymentsRepo  domain.PaymentRepository
	invoices      *service.InvoiceService
	basic         *domain.Package
	pro           *domain.Package
	proYearly     *domain.Package
	free          *domain.Package
}

func newChangesHarness(t *testing.T) *changesHarness {
	t.Helper()
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"billing_credits", "payment_approval_requests", "payments", "subscriptions",
		"package_features", "packages", "activity_log",
		"settings", "data_processing_jobs", "cache",
		"personal_access_tokens", "role_has_permissions", "model_has_roles",
		"users", "roles", "permissions", "branches", "tenants",
	)
	packagesRepo := domain.NewPackageRepository(queries)
	subsRepo := domain.NewSubscriptionRepository(queries)
	paymentsRepo := domain.NewPaymentRepository(queries)
	approvalsRepo := domain.NewApprovalRepository(queries)
	tx := database.NewTxManager(&database.Conn{Name: "primary", Pool: pool})
	orchestrator := billing.NewOrchestrator(paymentsRepo, domain.FixedCurrency("BDT")).
		Register(domain.MethodCash, billing.CashDriver{}).
		Register(domain.MethodBankTransfer, billing.BankTransferDriver{})
	h := &changesHarness{
		t: t, ctx: context.Background(), pool: pool,
		tenants:  tenantsrepo.NewRepository(queries),
		packages: service.NewPackageService(packagesRepo).WithTx(tx),
		subscriptions: service.NewSubscriptionService(subsRepo, packagesRepo, paymentsRepo, approvalsRepo, orchestrator).
			WithTx(tx).WithCurrencies(domain.FixedCurrency("BDT")),
		approvals:    service.NewApprovalService(approvalsRepo, paymentsRepo, subsRepo, tx),
		guard:        service.NewGuard(subsRepo, packagesRepo),
		subsRepo:     subsRepo,
		paymentsRepo: paymentsRepo,
		invoices:     service.NewInvoiceService(domain.NewReportRepository(queries), nil),
	}
	h.basic = h.plan("Basic", 1000, domain.CycleMonthly, 3)
	h.pro = h.plan("Pro", 3000, domain.CycleMonthly, 10)
	h.proYearly = h.plan("Pro yearly", 30000, domain.CycleYearly, 10)
	h.free = h.plan("Free", 0, domain.CycleMonthly, 1)
	return h
}

func (h *changesHarness) plan(name string, price int64, cycle string, users int64) *domain.Package {
	h.t.Helper()
	pkg, err := h.packages.Create(h.ctx, domain.PackageInput{Name: name, Price: price, BillingCycle: cycle, IsActive: true},
		[]domain.FeatureInput{{FeatureKey: string(domain.FeatureUsersMax), IsEnabled: true, LimitValue: users}})
	if err != nil {
		h.t.Fatalf("create package %s: %v", name, err)
	}
	return pkg
}

func (h *changesHarness) tenant(code string) int64 {
	h.t.Helper()
	tenant, err := h.tenants.Create(h.ctx, tenantsrepo.CreateInput{Name: "Org " + code, Code: code, IsActive: true})
	if err != nil {
		h.t.Fatalf("create tenant: %v", err)
	}
	return tenant.ID
}

// paidPlan puts the tenant on pkg, paid in cash (staff), with the period
// moved so that usedDays of it are behind.
func (h *changesHarness) paidPlan(tenantID int64, pkg *domain.Package, amount int64, usedDays, periodDays int) *domain.Subscription {
	h.t.Helper()
	subscription, _, err := h.subscriptions.Assign(h.ctx, service.AssignInput{
		TenantID: tenantID, PackageID: pkg.ID, ActorID: 1, PaymentMethod: domain.MethodCash, Amount: &amount,
	})
	if err != nil {
		h.t.Fatalf("assign %s: %v", pkg.Name, err)
	}
	// An hour earlier, so the whole days left never depend on the clocks of
	// Go and Postgres agreeing to the millisecond (days left round up).
	h.setPeriod(subscription.ID, -usedDays*24-1, (periodDays-usedDays)*24-1)
	return subscription
}

// setPeriod moves a subscription's period to start startHours from now and
// end endHours from now.
func (h *changesHarness) setPeriod(subscriptionID int64, startHours, endHours int) {
	h.t.Helper()
	if _, err := h.pool.Exec(h.ctx,
		`UPDATE subscriptions SET starts_at = now() + make_interval(hours => $2), expires_at = now() + make_interval(hours => $3) WHERE id = $1`,
		subscriptionID, startHours, endHours); err != nil {
		h.t.Fatalf("move period: %v", err)
	}
}

func (h *changesHarness) inForce(tenantID int64) *domain.Subscription {
	h.t.Helper()
	subscription, found, err := h.subsRepo.GetActiveForTenant(h.ctx, tenantID)
	if err != nil || !found {
		h.t.Fatalf("no plan in force for tenant %d: %v", tenantID, err)
	}
	return subscription
}

func (h *changesHarness) balance(tenantID int64) int64 {
	h.t.Helper()
	summary, err := h.subscriptions.Credits(h.ctx, tenantID, 50, 0)
	if err != nil {
		h.t.Fatalf("credits: %v", err)
	}
	return summary.Balance
}

func (h *changesHarness) approvalFor(paymentID int64) *domain.Approval {
	h.t.Helper()
	items, _, err := h.approvals.List(h.ctx, domain.ApprovalPending, 100, 0)
	if err != nil {
		h.t.Fatalf("list approvals: %v", err)
	}
	for i := range items {
		if items[i].PaymentID == paymentID {
			return &items[i]
		}
	}
	h.t.Fatalf("no pending approval for payment %d", paymentID)
	return nil
}

func near(t *testing.T, what string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil || got.Sub(want).Abs() > 2*time.Minute {
		t.Fatalf("%s = %v, want about %v", what, got, want)
	}
}

func wantCode(t *testing.T, err error, code apperr.Code) {
	t.Helper()
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestUpgradeCreditsTheUnusedTimeAndStartsWhenApproved(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("upgrade")
	basic := h.paidPlan(tenant, h.basic, 1000, 15, 30)

	preview, err := h.subscriptions.Preview(h.ctx, tenant, h.pro.ID, "")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	quote := preview.Quote
	if quote.Kind != domain.ChangeUpgrade || quote.Credit != 500 || quote.AmountDue != 2500 || quote.UnusedDays != 15 {
		t.Fatalf("preview = kind %s credit %d due %d unused %d", quote.Kind, quote.Credit, quote.AmountDue, quote.UnusedDays)
	}

	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, domain.MethodBankTransfer, map[string]any{"reference_number": "TX-1"}, 7)
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	payment := result.Payment
	if payment.Amount != 2500 || payment.Gross != 3000 || payment.Kind != domain.ChangeUpgrade || len(payment.Lines) != 2 {
		t.Fatalf("payment = amount %d gross %d kind %s lines %+v", payment.Amount, payment.Gross, payment.Kind, payment.Lines)
	}
	if payment.ReplacesSubscriptionID == nil || *payment.ReplacesSubscriptionID != basic.ID {
		t.Fatalf("replaces = %v, want %d", payment.ReplacesSubscriptionID, basic.ID)
	}
	if h.inForce(tenant).ID != basic.ID {
		t.Fatal("Basic must stay in force until the payment is confirmed")
	}

	if _, err := h.approvals.Approve(h.ctx, h.approvalFor(payment.ID).ID, 1, nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	pro := h.inForce(tenant)
	if pro.PackageID != h.pro.ID || pro.Status != domain.StatusActive {
		t.Fatalf("in force = %+v, want Pro active", pro)
	}
	near(t, "Pro starts", pro.StartsAt, time.Now())
	near(t, "Pro ends", pro.ExpiresAt, time.Now().AddDate(0, 1, 0))
	if old, _ := h.subsRepo.Get(h.ctx, basic.ID); old.Status != domain.StatusCancelled {
		t.Fatalf("Basic status = %s, want cancelled", old.Status)
	}

	invoice, err := h.invoices.For(h.ctx, tenant, payment.ID)
	if err != nil {
		t.Fatalf("invoice: %v", err)
	}
	if len(invoice.Rows) != 2 || invoice.Rows[0].Money.Minor != 3000 || invoice.Rows[1].Money.Minor != -500 ||
		invoice.Rows[1].Description != "Unused time on Basic plan (15 days)" {
		t.Fatalf("invoice rows = %+v", invoice.Rows)
	}
	near(t, "invoice period start", invoice.Rows[0].PeriodStart, time.Now())
}

func TestOnlyOneCheckoutWaitsAtATime(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("one-checkout")
	first, err := h.subscriptions.Checkout(h.ctx, tenant, h.basic.ID, domain.MethodCash, nil, 7)
	if err != nil {
		t.Fatalf("first checkout: %v", err)
	}
	_, err = h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, domain.MethodCash, nil, 7)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.Conflict || appErr.Meta["reason"] != service.CodeCheckoutPending ||
		appErr.Meta["payment_id"] != first.Payment.ID {
		t.Fatalf("second checkout error = %v (meta %+v), want CONFLICT CHECKOUT_PENDING", err, appErr)
	}
	// The same plan and method again is the same checkout (double click).
	again, err := h.subscriptions.Checkout(h.ctx, tenant, h.basic.ID, domain.MethodCash, nil, 7)
	if err != nil || again.Payment.ID != first.Payment.ID {
		t.Fatalf("repeat checkout = %v, %v; want payment %d", again, err, first.Payment.ID)
	}
}

func TestConcurrentCheckoutsForDifferentPlansLetOneThrough(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("race")
	plans := []*domain.Package{h.basic, h.pro, h.proYearly, h.basic, h.pro}
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		ok        int
		conflicts int
	)
	for _, pkg := range plans {
		wg.Add(1)
		go func(pkg *domain.Package) {
			defer wg.Done()
			_, err := h.subscriptions.Checkout(h.ctx, tenant, pkg.ID, domain.MethodCash, nil, 7)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case apperr.Is(err, apperr.Conflict):
				conflicts++
			default:
				t.Errorf("checkout: %v", err)
			}
		}(pkg)
	}
	wg.Wait()
	var pending int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM payments WHERE tenant_id = $1 AND status = 'pending'`, tenant).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 || ok+conflicts != len(plans) || ok < 1 {
		t.Fatalf("pending payments = %d, ok %d, conflicts %d; want exactly one waiting", pending, ok, conflicts)
	}
}

func TestDowngradeWaitsForTheRenewalAndThenStarts(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("downgrade")
	pro := h.paidPlan(tenant, h.pro, 3000, 5, 30)

	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.basic.ID, "", nil, 7)
	if err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	if result.Payment != nil || result.Change.Quote.Kind != domain.ChangeDowngrade || result.Change.Quote.DueAtRenewal != 1000 {
		t.Fatalf("downgrade result = payment %v kind %s", result.Payment, result.Change.Quote.Kind)
	}
	current := h.inForce(tenant)
	if current.ID != pro.ID || current.NextPackageID == nil || *current.NextPackageID != h.basic.ID {
		t.Fatalf("in force = %+v, want Pro with next Basic", current)
	}
	status, err := h.subscriptions.Status(h.ctx, tenant)
	if err != nil || status.NextPackage == nil || status.NextPackage.ID != h.basic.ID || !status.Active {
		t.Fatalf("status = %+v, %v", status, err)
	}

	// Renewing now pays for Basic, which starts when Pro ends.
	renew, err := h.subscriptions.Checkout(h.ctx, tenant, h.basic.ID, domain.MethodCash, nil, 7)
	if err != nil {
		t.Fatalf("renew onto Basic: %v", err)
	}
	if renew.Payment.Kind != domain.ChangeRenew || renew.Payment.Amount != 1000 {
		t.Fatalf("renew payment = kind %s amount %d", renew.Payment.Kind, renew.Payment.Amount)
	}
	if _, err := h.approvals.Approve(h.ctx, h.approvalFor(renew.Payment.ID).ID, 1, nil); err != nil {
		t.Fatalf("approve renewal: %v", err)
	}
	scheduled, err := h.subsRepo.Get(h.ctx, renew.Subscription.ID)
	if err != nil || scheduled.Status != domain.StatusScheduled || !scheduled.StartsAt.Equal(*pro.ExpiresAt) && h.inForce(tenant).ID != pro.ID {
		t.Fatalf("renewal = %+v, %v; want scheduled", scheduled, err)
	}
	proNow, _ := h.subsRepo.Get(h.ctx, pro.ID)
	if !scheduled.StartsAt.Equal(*proNow.ExpiresAt) {
		t.Fatalf("Basic starts %v, Pro ends %v", scheduled.StartsAt, proNow.ExpiresAt)
	}
	if proNow.NextPackageID != nil {
		t.Fatal("the renewal is arranged: next_package_id should be cleared")
	}
	if h.inForce(tenant).ID != pro.ID {
		t.Fatal("Pro stays in force until its end")
	}

	// Pro ends: the scheduled Basic counts at once (before the task runs)...
	h.setPeriod(pro.ID, -30*24, -1)
	h.setPeriod(scheduled.ID, -1, 30*24)
	if got := h.inForce(tenant); got.ID != scheduled.ID {
		t.Fatalf("in force after Pro ended = %d, want the scheduled Basic %d", got.ID, scheduled.ID)
	}
	if limit, err := h.guard.Limit(h.ctx, tenant, domain.FeatureUsersMax); err != nil || limit != 3 {
		t.Fatalf("users.max = %d, %v; want Basic's 3", limit, err)
	}
	// ...and the task makes it active.
	expired, started, err := h.subscriptions.ProcessPeriodEnds(h.ctx)
	if err != nil || expired < 1 || started < 1 {
		t.Fatalf("ProcessPeriodEnds = %d expired, %d started, %v", expired, started, err)
	}
	if got, _ := h.subsRepo.Get(h.ctx, scheduled.ID); got.Status != domain.StatusActive {
		t.Fatalf("Basic status = %s, want active", got.Status)
	}
	if got, _ := h.subsRepo.Get(h.ctx, pro.ID); got.Status != domain.StatusExpired {
		t.Fatalf("Pro status = %s, want expired", got.Status)
	}
}

func TestSwitchToFreeWaitsAndCanBeUndone(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("free")
	pro := h.paidPlan(tenant, h.pro, 3000, 5, 30)

	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.free.ID, "", nil, 7)
	if err != nil {
		t.Fatalf("to free: %v", err)
	}
	if result.Payment != nil || result.Subscription.Status != domain.StatusScheduled || result.Subscription.PackageID != h.free.ID {
		t.Fatalf("result = %+v", result.Subscription)
	}
	if h.inForce(tenant).ID != pro.ID {
		t.Fatal("Pro must stay until its end")
	}
	status, _ := h.subscriptions.Status(h.ctx, tenant)
	if status.Scheduled == nil || status.ScheduledPackage.ID != h.free.ID {
		t.Fatalf("status scheduled = %+v", status.Scheduled)
	}
	if _, err := h.subscriptions.KeepPlan(h.ctx, tenant, 7); err != nil {
		t.Fatalf("KeepPlan: %v", err)
	}
	if got, _ := h.subsRepo.Get(h.ctx, result.Subscription.ID); got.Status != domain.StatusCancelled {
		t.Fatalf("free plan status = %s, want cancelled", got.Status)
	}
}

func TestEarlyRenewalKeepsTheDaysAndTheOverrides(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("renew")
	pro := h.paidPlan(tenant, h.pro, 3000, 25, 30)
	if _, err := h.subscriptions.ApplyOverrides(h.ctx, pro.ID, map[string]any{"users.max": 25}); err != nil {
		t.Fatalf("overrides: %v", err)
	}
	pro, _ = h.subsRepo.Get(h.ctx, pro.ID)

	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, domain.MethodCash, nil, 7)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if result.Payment.Kind != domain.ChangeRenew || result.Payment.Amount != 3000 {
		t.Fatalf("payment = %+v", result.Payment)
	}
	if _, err := h.approvals.Approve(h.ctx, h.approvalFor(result.Payment.ID).ID, 1, nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	next, _ := h.subsRepo.Get(h.ctx, result.Subscription.ID)
	if next.Status != domain.StatusScheduled || !next.StartsAt.Equal(*pro.ExpiresAt) ||
		!next.ExpiresAt.Equal(pro.ExpiresAt.AddDate(0, 1, 0)) {
		t.Fatalf("next period = %s %v – %v; current ends %v", next.Status, next.StartsAt, next.ExpiresAt, pro.ExpiresAt)
	}
	if next.Overrides["users.max"] != float64(25) {
		t.Fatalf("overrides = %+v, want users.max 25 carried over", next.Overrides)
	}
	// A second renewal is refused: the next period is arranged.
	_, err = h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, domain.MethodCash, nil, 7)
	wantCode(t, err, apperr.InvalidState)
	// Renewal reminders skip a tenant whose next period is paid.
	expiring, err := h.subscriptions.Expiring(h.ctx, time.Now().AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	for _, subscription := range expiring {
		if subscription.TenantID == tenant {
			t.Fatal("a renewed tenant is still reminded")
		}
	}
}

func TestAccountCreditIsUsedAndGivenBackWhenACheckoutFails(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("credit")
	if _, err := h.subscriptions.AdjustCredit(h.ctx, tenant, 400, "Goodwill", 1); err != nil {
		t.Fatalf("AdjustCredit: %v", err)
	}
	if _, err := h.subscriptions.AdjustCredit(h.ctx, tenant, -500, "Too much", 1); err == nil {
		t.Fatal("credit went below zero")
	}

	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.basic.ID, domain.MethodCash, nil, 7)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if result.Payment.Amount != 600 || result.Payment.BalanceApplied != 400 || h.balance(tenant) != 0 {
		t.Fatalf("amount %d balance applied %d balance %d", result.Payment.Amount, result.Payment.BalanceApplied, h.balance(tenant))
	}
	if err := h.approvals.Withdraw(h.ctx, tenant, result.Payment.ID, 7); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if got := h.balance(tenant); got != 400 {
		t.Fatalf("balance after withdraw = %d, want 400", got)
	}

	result, err = h.subscriptions.Checkout(h.ctx, tenant, h.basic.ID, domain.MethodBankTransfer, nil, 7)
	if err != nil {
		t.Fatalf("checkout again: %v", err)
	}
	if _, err := h.approvals.Reject(h.ctx, h.approvalFor(result.Payment.ID).ID, 1, nil); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := h.balance(tenant); got != 400 {
		t.Fatalf("balance after reject = %d, want 400", got)
	}
}

func TestCreditCoveringTheWholePriceStartsThePlanAtOnce(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("covered")
	if _, err := h.subscriptions.AdjustCredit(h.ctx, tenant, 5000, "Prepaid", 1); err != nil {
		t.Fatal(err)
	}
	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, "", nil, 7)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if result.Payment.PaymentMethod != domain.MethodCredit || result.Payment.Amount != 0 ||
		result.Payment.Status != domain.PaymentCompleted || result.Subscription.Status != domain.StatusActive {
		t.Fatalf("payment %+v subscription %s", result.Payment, result.Subscription.Status)
	}
	if got := h.balance(tenant); got != 2000 {
		t.Fatalf("balance = %d, want 2000", got)
	}
}

func TestUnusedTimeBeyondTheNewPriceBecomesCredit(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("surplus")
	basicYear := h.plan("Basic yearly", 12000, domain.CycleYearly, 3)
	h.paidPlan(tenant, basicYear, 12000, 30, 365)

	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, "", nil, 7)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	quote := result.Change.Quote
	if quote.Kind != domain.ChangeUpgrade || quote.AmountDue != 0 || quote.Surplus == 0 {
		t.Fatalf("quote kind %s due %d surplus %d", quote.Kind, quote.AmountDue, quote.Surplus)
	}
	if result.Subscription.Status != domain.StatusActive || result.Subscription.PackageID != h.pro.ID {
		t.Fatalf("subscription = %+v", result.Subscription)
	}
	if got := h.balance(tenant); got != quote.Surplus {
		t.Fatalf("balance = %d, want the surplus %d", got, quote.Surplus)
	}
}

func TestCancelAtPeriodEndCreditsAPaidRenewalAndCanBeUndone(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("cancel")
	pro := h.paidPlan(tenant, h.pro, 3000, 25, 30)
	renew, err := h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, domain.MethodCash, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.Approve(h.ctx, h.approvalFor(renew.Payment.ID).ID, 1, nil); err != nil {
		t.Fatal(err)
	}

	cancelled, err := h.subscriptions.CancelAtPeriodEnd(h.ctx, tenant, 7)
	if err != nil {
		t.Fatalf("CancelAtPeriodEnd: %v", err)
	}
	if !cancelled.CancelAtPeriodEnd || cancelled.ID != pro.ID || h.inForce(tenant).ID != pro.ID {
		t.Fatalf("cancelled = %+v", cancelled)
	}
	if got, _ := h.subsRepo.Get(h.ctx, renew.Subscription.ID); got.Status != domain.StatusCancelled {
		t.Fatalf("paid renewal status = %s, want cancelled", got.Status)
	}
	if got := h.balance(tenant); got != 3000 {
		t.Fatalf("balance = %d, want the renewal's 3000", got)
	}
	expiring, _ := h.subscriptions.Expiring(h.ctx, time.Now().AddDate(0, 0, 30))
	for _, subscription := range expiring {
		if subscription.TenantID == tenant {
			t.Fatal("a cancelled plan is still reminded")
		}
	}
	kept, err := h.subscriptions.KeepPlan(h.ctx, tenant, 7)
	if err != nil || kept.CancelAtPeriodEnd {
		t.Fatalf("KeepPlan = %+v, %v", kept, err)
	}
}

func TestStaffRefunds(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("refund")
	h.paidPlan(tenant, h.basic, 1000, 1, 30)
	payments, _, err := h.paymentsRepo.ListForTenant(h.ctx, tenant, 10, 0)
	if err != nil || len(payments) != 1 {
		t.Fatalf("payments = %v, %v", payments, err)
	}
	payment := payments[0]

	refunded, err := h.subscriptions.Refund(h.ctx, service.RefundInput{
		PaymentID: payment.ID, Amount: 400, Reason: "Goodwill", To: service.RefundToCredit, ActorID: 1,
	})
	if err != nil || refunded.RefundedAmount != 400 || refunded.Status != domain.PaymentCompleted {
		t.Fatalf("refund to credit = %+v, %v", refunded, err)
	}
	if got := h.balance(tenant); got != 400 {
		t.Fatalf("balance = %d, want 400", got)
	}
	_, err = h.subscriptions.Refund(h.ctx, service.RefundInput{PaymentID: payment.ID, Amount: 700, Reason: "Too much", To: service.RefundToOriginal, ActorID: 1})
	wantCode(t, err, apperr.ValidationFailed)

	refunded, err = h.subscriptions.Refund(h.ctx, service.RefundInput{
		PaymentID: payment.ID, Amount: 600, Reason: "Closing the account", To: service.RefundToOriginal, EndSubscription: true, ActorID: 1,
	})
	if err != nil || refunded.RefundedAmount != 1000 || refunded.Status != domain.PaymentRefunded {
		t.Fatalf("full refund = %+v, %v", refunded, err)
	}
	if _, found, _ := h.subsRepo.GetActiveForTenant(h.ctx, tenant); found {
		t.Fatal("the plan should have ended with the refund")
	}
	_, err = h.subscriptions.Refund(h.ctx, service.RefundInput{PaymentID: payment.ID, Amount: 1, Reason: "Again", To: service.RefundToCredit, ActorID: 1})
	wantCode(t, err, apperr.InvalidState)

	invoice, err := h.invoices.For(h.ctx, tenant, payment.ID)
	if err != nil || invoice.RefundedAmount != 1000 || invoice.Refunded.Minor != 1000 {
		t.Fatalf("invoice refunded = %+v, %v", invoice, err)
	}
}

func TestApprovalWithTheAmountReceived(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("received")
	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.basic.ID, domain.MethodBankTransfer, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	approval := h.approvalFor(result.Payment.ID)
	less := int64(900)
	_, err = h.approvals.ApproveReceived(h.ctx, approval.ID, 1, nil, &less)
	wantCode(t, err, apperr.ValidationFailed)
	more := int64(1250)
	if _, err := h.approvals.ApproveReceived(h.ctx, approval.ID, 1, nil, &more); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if got := h.balance(tenant); got != 250 {
		t.Fatalf("balance = %d, want the 250 overpaid", got)
	}
}

func TestApprovalSaysWhenTheQuoteIsOutOfDate(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("stale")
	h.paidPlan(tenant, h.basic, 1000, 15, 30)
	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, domain.MethodBankTransfer, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	approval := h.approvalFor(result.Payment.ID)
	details, err := h.approvals.Details(h.ctx, []int64{approval.ID})
	if err != nil || details[approval.ID].QuoteStale || details[approval.ID].Kind != domain.ChangeUpgrade || len(details[approval.ID].Lines) != 2 {
		t.Fatalf("details = %+v, %v", details[approval.ID], err)
	}
	// Staff move the organization meanwhile: the credited plan is gone.
	if _, _, err := h.subscriptions.Assign(h.ctx, service.AssignInput{TenantID: tenant, PackageID: h.proYearly.ID, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	details, _ = h.approvals.Details(h.ctx, []int64{approval.ID})
	if !details[approval.ID].QuoteStale {
		t.Fatal("quote_stale = false after staff changed the plan")
	}
}

func TestStaffAssignInChangeModeFollowsTheCustomerRules(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("staff-change")
	h.paidPlan(tenant, h.basic, 1000, 15, 30)
	subscription, payment, err := h.subscriptions.Assign(h.ctx, service.AssignInput{
		TenantID: tenant, PackageID: h.pro.ID, ActorID: 1, PaymentMethod: domain.MethodCash, Mode: service.AssignChange,
	})
	if err != nil {
		t.Fatalf("assign change: %v", err)
	}
	if payment.Amount != 2500 || payment.Kind != domain.ChangeStaff || payment.Status != domain.PaymentCompleted || subscription.Status != domain.StatusActive {
		t.Fatalf("payment %+v subscription %s", payment, subscription.Status)
	}
	// A downgrade in change mode waits for the period end.
	subscription, payment, err = h.subscriptions.Assign(h.ctx, service.AssignInput{
		TenantID: tenant, PackageID: h.basic.ID, ActorID: 1, Mode: service.AssignChange,
	})
	if err != nil || payment != nil || subscription.NextPackageID == nil || *subscription.NextPackageID != h.basic.ID {
		t.Fatalf("staff downgrade = %+v, %+v, %v", subscription, payment, err)
	}
}

func TestAHiddenPlanCanOnlyBeRenewedByItsSubscribers(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("hidden")
	h.paidPlan(tenant, h.basic, 1000, 25, 30)
	if _, err := h.pool.Exec(h.ctx, `UPDATE packages SET is_active = false WHERE id = $1`, h.basic.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.subscriptions.Preview(h.ctx, tenant, h.basic.ID, ""); err != nil {
		t.Fatalf("renewing a hidden plan: %v", err)
	}
	other := h.tenant("hidden-other")
	_, err := h.subscriptions.Preview(h.ctx, other, h.basic.ID, "")
	wantCode(t, err, apperr.BusinessLogicError)
}

func TestPayingDuringATrialOfTheSamePlanStartsWhenTheTrialEnds(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("trial")
	trialPlan := h.plan("Trial Pro", 3000, domain.CycleMonthly, 10)
	if _, err := h.pool.Exec(h.ctx, `UPDATE packages SET trial_days = 14 WHERE id = $1`, trialPlan.ID); err != nil {
		t.Fatal(err)
	}
	trial, err := h.subscriptions.StartTrial(h.ctx, tenant, trialPlan.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.subscriptions.Checkout(h.ctx, tenant, trialPlan.ID, domain.MethodCash, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.Approve(h.ctx, h.approvalFor(result.Payment.ID).ID, 1, nil); err != nil {
		t.Fatal(err)
	}
	paid, _ := h.subsRepo.Get(h.ctx, result.Subscription.ID)
	if paid.Status != domain.StatusScheduled || !paid.StartsAt.Equal(*trial.ExpiresAt) {
		t.Fatalf("paid period = %s from %v, trial ends %v", paid.Status, paid.StartsAt, trial.ExpiresAt)
	}
	if h.inForce(tenant).ID != trial.ID {
		t.Fatal("the trial must run to its end")
	}
}

func TestAPaymentApprovedAfterThePlanLapsedStartsNow(t *testing.T) {
	h := newChangesHarness(t)
	tenant := h.tenant("lapsed")
	pro := h.paidPlan(tenant, h.pro, 3000, 25, 30)
	result, err := h.subscriptions.Checkout(h.ctx, tenant, h.pro.ID, domain.MethodCash, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	// The plan ends while the renewal waits for staff.
	h.setPeriod(pro.ID, -40*24, -24)
	if _, err := h.approvals.Approve(h.ctx, h.approvalFor(result.Payment.ID).ID, 1, nil); err != nil {
		t.Fatal(err)
	}
	renewed := h.inForce(tenant)
	if renewed.ID != result.Subscription.ID || renewed.Status != domain.StatusActive {
		t.Fatalf("in force = %+v", renewed)
	}
	near(t, "starts", renewed.StartsAt, time.Now())
}
