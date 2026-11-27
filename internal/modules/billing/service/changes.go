package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// Plan changes, renewals, credits and refunds (docs/guides/billing.md#changing-plans).
// Every price comes from domain.QuoteChange; this file loads its input,
// applies the result and keeps the account credit ledger.

// CodeCheckoutPending is the meta code of the 409 returned while another
// checkout of the organization waits.
const CodeCheckoutPending = "CHECKOUT_PENDING"

// store is the set of repositories one unit of work uses (the service's own,
// or ones bound to a transaction).
type store struct {
	subscriptions domain.SubscriptionRepository
	packages      domain.PackageRepository
	payments      domain.PaymentRepository
	credits       domain.CreditRepository
}

func storeFor(q *dbgen.Queries) store {
	return store{
		subscriptions: domain.NewSubscriptionRepository(q),
		packages:      domain.NewPackageRepository(q),
		payments:      domain.NewPaymentRepository(q),
		credits:       domain.NewCreditRepository(q),
	}
}

func (s *SubscriptionService) store() store {
	return store{subscriptions: s.subscriptions, packages: s.packages, payments: s.payments, credits: s.credits}
}

// Change is a priced plan change: the quote, and the plans it moves between.
type Change struct {
	Quote          domain.Quote
	Current        *domain.Subscription
	CurrentPackage *domain.Package
	Scheduled      *domain.Subscription
	Currency       string
}

// quoteFor prices a move to pkg for the tenant from what is in force now.
func (s *SubscriptionService) quoteFor(ctx context.Context, st store, tenantID int64, pkg domain.Package) (*Change, error) {
	currency, err := s.currencies.DefaultCode(ctx)
	if err != nil {
		return nil, err
	}
	change := &Change{Currency: currency}
	input := domain.QuoteInput{Now: s.now(), Target: pkg}
	current, found, err := st.subscriptions.GetActiveForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if found {
		base, err := st.packages.Get(ctx, current.PackageID)
		if err != nil {
			return nil, err
		}
		offered := current.PlanFor(*base)
		currentPkg := &offered
		paid, err := st.payments.PaidValue(ctx, current.ID)
		if err != nil {
			return nil, err
		}
		change.Current, change.CurrentPackage = current, currentPkg
		input.Current, input.CurrentPackage, input.CurrentPaid = current, currentPkg, paid
	}
	scheduled, found, err := st.subscriptions.GetScheduledForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if found && (change.Current == nil || scheduled.ID != change.Current.ID) {
		change.Scheduled = scheduled
		input.Scheduled = scheduled
	}
	if st.credits != nil {
		if input.Balance, err = st.credits.Balance(ctx, tenantID, currency); err != nil {
			return nil, err
		}
	}
	quote, err := domain.QuoteChange(input)
	if err != nil {
		return nil, err
	}
	if change.CurrentPackage != nil {
		quote.NameUnusedTime(*change.CurrentPackage)
	}
	change.Quote = quote
	return change, nil
}

// Preview prices a plan change without changing anything (GET
// /billing/change-preview).
func (s *SubscriptionService) Preview(ctx context.Context, tenantID, packageID int64, cycle string) (_ *Change, err error) {
	ctx, end := tracing.Begin(ctx, "billing.change_preview",
		attribute.Int64("tenant.id", tenantID), attribute.Int64("package.id", packageID), attribute.String("billing.cycle", cycle))
	defer end(&err)
	base, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return nil, err
	}
	pkg, err := base.ForCycle(cycle)
	if err != nil {
		return nil, err
	}
	change, err := s.quoteFor(ctx, s.store(), tenantID, pkg)
	if err != nil {
		return nil, err
	}
	if err := available(&pkg, change); err != nil {
		return nil, err
	}
	return change, nil
}

// available: a hidden plan can only be renewed by those already on it (Q-4).
func available(pkg *domain.Package, change *Change) error {
	if pkg.IsActive {
		return nil
	}
	if change.Quote.Kind == domain.ChangeRenew && change.Current != nil && change.Current.PackageID == pkg.ID {
		return nil
	}
	return apperr.New(apperr.BusinessLogicError, "This package is not available.")
}

// checkoutRequest is one plan change asked by the organization or by staff.
type checkoutRequest struct {
	TenantID int64
	ActorID  int64
	Package  *domain.Package
	// Cycle is the billing cycle chosen ("" is the plan's own).
	Cycle       string
	Method      string
	PaymentData map[string]any
	// Staff records the payment as received at once (cash or bank).
	Staff bool
	// Amount is what staff received, when not the amount due.
	Amount *int64
}

// checkoutResult is what a checkout did.
type checkoutResult struct {
	Subscription *domain.Subscription
	Payment      *domain.Payment
	Change       *Change
	Created      bool
}

// checkout applies a plan change on s (bound to the tenant-locked
// transaction): it prices it, then either records the plan for the renewal
// (downgrade), starts or schedules a free plan, settles it from the account
// credit, or charges the amount due.
func (s *SubscriptionService) checkout(ctx context.Context, req checkoutRequest) (*checkoutResult, error) {
	st := s.store()
	tenantTarget := req.TenantID
	// A scheduled plan whose turn came counts as the current one.
	if _, err := st.subscriptions.ActivateDueScheduled(ctx, &tenantTarget); err != nil {
		return nil, err
	}
	offered, err := req.Package.ForCycle(req.Cycle)
	if err != nil {
		return nil, err
	}
	pkg := &offered
	if !req.Staff {
		if existing, found, err := st.payments.PendingCheckout(ctx, req.TenantID, pkg.ID, req.Method); err != nil {
			return nil, err
		} else if found && existing.SubscriptionID != nil {
			subscription, err := st.subscriptions.Get(ctx, *existing.SubscriptionID)
			if err != nil {
				return nil, err
			}
			// The same checkout asked again (a retry or a double click). A
			// checkout of the same plan at another cycle is a different one:
			// it meets the pending-payment check below.
			if subscription.Cycle(*req.Package) == pkg.BillingCycle {
				return &checkoutResult{Subscription: subscription, Payment: existing}, nil
			}
		}
	}
	if pending, found, err := st.payments.PendingForTenant(ctx, req.TenantID); err != nil {
		return nil, err
	} else if found {
		return nil, apperr.New(apperr.Conflict, "Another payment is still waiting. Withdraw it or wait for it to be confirmed before starting a new one.").
			WithMeta(map[string]any{"reason": CodeCheckoutPending, "payment_id": pending.ID, "invoice_number": pending.InvoiceNumber})
	}
	change, err := s.quoteFor(ctx, st, req.TenantID, *pkg)
	if err != nil {
		return nil, err
	}
	if !req.Staff { // staff may put an organization on a hidden plan
		if err := available(pkg, change); err != nil {
			return nil, err
		}
	}
	quote := change.Quote
	now := s.now()

	if !quote.RequiresPayment {
		return s.changeWithoutPayment(ctx, st, req, pkg, change)
	}

	method := req.Method
	amount := quote.AmountDue
	if req.Staff && req.Amount != nil {
		amount = *req.Amount
	}
	creditOnly := amount == 0
	switch {
	case creditOnly:
		method = domain.MethodCredit
	case req.Staff:
		if !domain.IsManualMethod(method) {
			return nil, apperr.Validation(map[string][]string{
				"payment_method": {"Only cash and bank transfer payments can be recorded manually."},
			})
		}
	default:
		switch method {
		case domain.MethodBkash, domain.MethodNagad, domain.MethodCash, domain.MethodBankTransfer:
		default:
			return nil, apperr.Validation(map[string][]string{
				"payment_method": {"The selected payment method is invalid."},
			})
		}
	}

	subscription, err := st.subscriptions.Create(ctx, domain.SubscriptionInput{
		TenantID: req.TenantID, PackageID: pkg.ID, Status: domain.StatusPending,
		StartsAt: &quote.PeriodStart, ExpiresAt: &quote.PeriodEnd, BillingCycle: pkg.BillingCycle,
	})
	if err != nil {
		return nil, err
	}
	kind := quote.Kind
	if req.Staff && kind != domain.ChangeRenew {
		kind = domain.ChangeStaff
	}
	paymentQuote := domain.PaymentQuote{
		Kind: kind, Lines: quote.Lines, Gross: quote.Price,
		BalanceApplied: quote.BalanceApplied, ReplacesSubscriptionID: quote.ReplacesSubscriptionID,
	}

	var payment *domain.Payment
	if creditOnly || req.Staff {
		invoice, err := domain.GenerateInvoiceNumber(now)
		if err != nil {
			return nil, err
		}
		created, err := st.payments.Create(ctx, domain.PaymentInput{
			TenantID: req.TenantID, SubscriptionID: &subscription.ID, PaymentMethod: method,
			Status: domain.PaymentPending, Amount: amount, Currency: change.Currency,
			InvoiceNumber: invoice, CreatedBy: &req.ActorID,
		})
		if err != nil {
			return nil, err
		}
		if payment, err = st.payments.SetQuote(ctx, created.ID, paymentQuote); err != nil {
			return nil, err
		}
		if err := reserveCredit(ctx, st, payment); err != nil {
			return nil, err
		}
		gatewayStatus := "covered by account credit"
		if !creditOnly {
			gatewayStatus = "recorded by staff"
		}
		if payment, err = st.payments.UpdateStatus(ctx, payment.ID, domain.PaymentCompleted, &req.ActorID, &gatewayStatus); err != nil {
			return nil, err
		}
		if subscription, err = fulfil(ctx, st, payment, now); err != nil {
			return nil, err
		}
		return &checkoutResult{Subscription: subscription, Payment: payment, Change: change, Created: true}, nil
	}

	charged, err := s.orchestrator.Charge(ctx, req.TenantID, &subscription.ID, method, amount, req.PaymentData, &req.ActorID)
	if err != nil {
		return nil, err
	}
	if payment, err = st.payments.SetQuote(ctx, charged.ID, paymentQuote); err != nil {
		return nil, err
	}
	if err := reserveCredit(ctx, st, payment); err != nil {
		return nil, err
	}
	if domain.IsManualMethod(method) {
		encoded, _ := json.Marshal(req.PaymentData)
		if _, err := s.approvals.Create(ctx, domain.ApprovalInput{
			TenantID: req.TenantID, SubscriptionID: subscription.ID, PaymentID: payment.ID,
			PaymentMethod: method, PaymentData: encoded, CreatedBy: &req.ActorID,
		}); err != nil {
			return nil, err
		}
	}
	return &checkoutResult{Subscription: subscription, Payment: payment, Change: change, Created: true}, nil
}

// changeWithoutPayment handles changes with nothing to pay now: a downgrade
// is remembered for the renewal (Q-1); a free plan starts now or is
// scheduled for the end of the period.
func (s *SubscriptionService) changeWithoutPayment(ctx context.Context, st store, req checkoutRequest, pkg *domain.Package, change *Change) (*checkoutResult, error) {
	quote := change.Quote
	if pkg.Price > 0 {
		// A paid downgrade (another plan, or the same plan on a shorter
		// cycle): the organization pays for it when it renews.
		cycle := pkg.BillingCycle
		updated, err := st.subscriptions.SetPlanEnd(ctx, change.Current.ID, &pkg.ID, &cycle, false)
		if err != nil {
			return nil, err
		}
		return &checkoutResult{Subscription: updated, Change: change}, nil
	}
	if quote.Effective == domain.EffectiveNow {
		if err := withdrawScheduled(ctx, st, req.TenantID, change.Currency, "Replaced by a new plan.", req.ActorID); err != nil {
			return nil, err
		}
		if err := st.subscriptions.DeactivateActive(ctx, req.TenantID); err != nil {
			return nil, err
		}
		created, err := st.subscriptions.Create(ctx, domain.SubscriptionInput{
			TenantID: req.TenantID, PackageID: pkg.ID, Status: domain.StatusActive,
			StartsAt: &quote.PeriodStart, ExpiresAt: &quote.PeriodEnd, BillingCycle: pkg.BillingCycle,
		})
		if err != nil {
			return nil, err
		}
		return &checkoutResult{Subscription: created, Change: change, Created: true}, nil
	}
	// A free plan after the paid period.
	created, err := st.subscriptions.Create(ctx, domain.SubscriptionInput{
		TenantID: req.TenantID, PackageID: pkg.ID, Status: domain.StatusPending,
		StartsAt: &quote.PeriodStart, ExpiresAt: &quote.PeriodEnd, BillingCycle: pkg.BillingCycle,
	})
	if err != nil {
		return nil, err
	}
	overrides := carriedOverrides(change.Current, pkg.ID)
	scheduled, err := st.subscriptions.Schedule(ctx, created.ID, quote.PeriodStart, quote.PeriodEnd, overrides)
	if err != nil {
		return nil, err
	}
	if _, err := st.subscriptions.SetPlanEnd(ctx, change.Current.ID, nil, nil, false); err != nil {
		return nil, err
	}
	return &checkoutResult{Subscription: scheduled, Change: change, Created: true}, nil
}

// carriedOverrides returns the current subscription's overrides when the
// next period is the same plan (P-14), else none.
func carriedOverrides(current *domain.Subscription, packageID int64) []byte {
	if current == nil || current.PackageID != packageID || len(current.Overrides) == 0 {
		return nil
	}
	encoded, err := json.Marshal(current.Overrides)
	if err != nil {
		return nil
	}
	return encoded
}

// fulfil applies a completed payment to its subscription, in the caller's
// transaction: a renewal waits for the current period (scheduled), anything
// else starts now with a fresh period, replacing the plan in force. Unused
// time beyond the new price becomes account credit.
func fulfil(ctx context.Context, st store, payment *domain.Payment, now time.Time) (*domain.Subscription, error) {
	if payment.SubscriptionID == nil {
		return nil, nil
	}
	subscription, err := st.subscriptions.Get(ctx, *payment.SubscriptionID)
	if err != nil {
		return nil, err
	}
	base, err := st.packages.Get(ctx, subscription.PackageID)
	if err != nil {
		return nil, err
	}
	cycle := subscription.Cycle(*base)
	actor := payment.RecordedBy
	actorID := int64(0)
	if actor != nil {
		actorID = *actor
	}

	if payment.Kind == domain.ChangeRenew {
		current, found, err := st.subscriptions.GetActiveForTenant(ctx, subscription.TenantID)
		if err != nil {
			return nil, err
		}
		if found && current.ID != subscription.ID && current.ExpiresAt != nil && current.ExpiresAt.After(now) {
			if err := withdrawScheduled(ctx, st, subscription.TenantID, payment.Currency, "Replaced by a renewal.", actorID); err != nil {
				return nil, err
			}
			start := *current.ExpiresAt
			scheduled, err := st.subscriptions.Schedule(ctx, subscription.ID, start, domain.PeriodEnd(cycle, start),
				carriedOverrides(current, subscription.PackageID))
			if err != nil {
				return nil, err
			}
			if _, err := st.subscriptions.SetPlanEnd(ctx, current.ID, nil, nil, false); err != nil {
				return nil, err
			}
			return scheduled, nil
		}
		// The current plan ended while the payment waited: start now.
	}

	if err := withdrawScheduled(ctx, st, subscription.TenantID, payment.Currency, "Replaced by a new plan.", actorID); err != nil {
		return nil, err
	}
	if err := st.subscriptions.DeactivateActive(ctx, subscription.TenantID); err != nil {
		return nil, err
	}
	start := now
	activated, err := st.subscriptions.Activate(ctx, subscription.ID, &start, domain.PeriodEnd(cycle, start))
	if err != nil {
		return nil, err
	}
	if surplus := surplusOf(payment.Lines); surplus > 0 && st.credits != nil {
		if _, err := st.credits.Add(ctx, domain.CreditInput{
			TenantID: payment.TenantID, Amount: surplus, Currency: payment.Currency,
			Reason:    fmt.Sprintf("Unused time beyond the price of the new plan (invoice %s).", payment.InvoiceNumber),
			PaymentID: &payment.ID, SubscriptionID: &subscription.ID, CreatedBy: actor,
		}); err != nil {
			return nil, err
		}
	}
	return activated, nil
}

// surplusOf is the unused-time credit beyond the plan price, from the frozen
// lines (0 when the credit did not cover the price).
func surplusOf(lines []domain.InvoiceLine) int64 {
	var total int64
	for _, line := range lines {
		if line.Type == domain.LinePlan || line.Type == domain.LineUnusedTime {
			total += line.Amount
		}
	}
	if total < 0 {
		return -total
	}
	return 0
}

// withdrawScheduled cancels the tenant's scheduled subscription; what was
// paid for it becomes account credit.
func withdrawScheduled(ctx context.Context, st store, tenantID int64, currency, reason string, actorID int64) error {
	cancelled, err := st.subscriptions.CancelScheduled(ctx, tenantID, reason)
	if err != nil {
		return err
	}
	for _, subscription := range cancelled {
		paid, err := st.payments.PaidValue(ctx, subscription.ID)
		if err != nil {
			return err
		}
		if paid <= 0 || st.credits == nil {
			continue
		}
		id := subscription.ID
		var actor *int64
		if actorID != 0 {
			actor = &actorID
		}
		if _, err := st.credits.Add(ctx, domain.CreditInput{
			TenantID: tenantID, Amount: paid, Currency: currency,
			Reason: "A period paid in advance was withdrawn.", SubscriptionID: &id, CreatedBy: actor,
		}); err != nil {
			return err
		}
	}
	return nil
}

// reserveCredit takes the account credit a checkout uses (a negative entry);
// releaseCredit gives it back when the payment does not complete.
func reserveCredit(ctx context.Context, st store, payment *domain.Payment) error {
	if payment.BalanceApplied <= 0 || st.credits == nil {
		return nil
	}
	_, err := st.credits.Add(ctx, domain.CreditInput{
		TenantID: payment.TenantID, Amount: -payment.BalanceApplied, Currency: payment.Currency,
		Reason:    fmt.Sprintf("Used for invoice %s.", payment.InvoiceNumber),
		PaymentID: &payment.ID, SubscriptionID: payment.SubscriptionID, CreatedBy: payment.CreatedBy,
	})
	return err
}

func releaseCredit(ctx context.Context, credits domain.CreditRepository, payment *domain.Payment, why string) error {
	if payment.BalanceApplied <= 0 || credits == nil {
		return nil
	}
	_, err := credits.Add(ctx, domain.CreditInput{
		TenantID: payment.TenantID, Amount: payment.BalanceApplied, Currency: payment.Currency,
		Reason:    fmt.Sprintf("Given back: invoice %s was %s.", payment.InvoiceNumber, why),
		PaymentID: &payment.ID, SubscriptionID: payment.SubscriptionID,
	})
	return err
}

// ── Cancel at period end, keep the plan ────────────────────────────────────

// CancelAtPeriodEnd stops the renewal: the plan runs until it expires, the
// renewal reminders stop, and a period paid in advance becomes account
// credit (P-13).
func (s *SubscriptionService) CancelAtPeriodEnd(ctx context.Context, tenantID, actorID int64) (_ *domain.Subscription, err error) {
	ctx, end := tracing.Begin(ctx, "billing.cancel_at_period_end", attribute.Int64("tenant.id", tenantID))
	defer end(&err)
	var updated *domain.Subscription
	err = s.inTenantLock(ctx, "billing.subscribe", tenantID, func(scoped *SubscriptionService) error {
		st := scoped.store()
		current, found, err := st.subscriptions.GetActiveForTenant(ctx, tenantID)
		if err != nil {
			return err
		}
		if !found {
			return apperr.New(apperr.InvalidState, "There is no plan to cancel.")
		}
		currency, err := scoped.currencies.DefaultCode(ctx)
		if err != nil {
			return err
		}
		if err := withdrawScheduled(ctx, st, tenantID, currency, "The organization cancelled its plan.", actorID); err != nil {
			return err
		}
		updated, err = st.subscriptions.SetPlanEnd(ctx, current.ID, nil, nil, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "subscription.cancel_scheduled",
		Subject: "subscription", SubjectID: updated.ID, Changes: map[string]any{"expires_at": updated.ExpiresAt},
	})
	s.changed(ctx, tenantID)
	return updated, nil
}

// KeepPlan undoes a cancellation or a chosen downgrade: the current plan is
// what the organization renews. A free plan scheduled to follow is withdrawn.
func (s *SubscriptionService) KeepPlan(ctx context.Context, tenantID, actorID int64) (_ *domain.Subscription, err error) {
	ctx, end := tracing.Begin(ctx, "billing.keep_plan", attribute.Int64("tenant.id", tenantID))
	defer end(&err)
	var updated *domain.Subscription
	err = s.inTenantLock(ctx, "billing.subscribe", tenantID, func(scoped *SubscriptionService) error {
		st := scoped.store()
		current, found, err := st.subscriptions.GetActiveForTenant(ctx, tenantID)
		if err != nil {
			return err
		}
		if !found {
			return apperr.New(apperr.InvalidState, "There is no plan to keep.")
		}
		if scheduled, found, err := st.subscriptions.GetScheduledForTenant(ctx, tenantID); err != nil {
			return err
		} else if found && scheduled.ID != current.ID && scheduled.PackageID != current.PackageID {
			paid, err := st.payments.PaidValue(ctx, scheduled.ID)
			if err != nil {
				return err
			}
			if paid > 0 {
				return apperr.New(apperr.InvalidState, "Your next period is already paid for. Ask support to change it.")
			}
			if _, err := st.subscriptions.CancelScheduled(ctx, tenantID, "The organization kept its plan."); err != nil {
				return err
			}
		}
		updated, err = st.subscriptions.SetPlanEnd(ctx, current.ID, nil, nil, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "subscription.plan_kept",
		Subject: "subscription", SubjectID: updated.ID,
	})
	s.changed(ctx, tenantID)
	return updated, nil
}

// inTenantLock runs fn on a copy bound to a transaction holding the tenant's
// billing lock (or on s itself without a transaction manager).
func (s *SubscriptionService) inTenantLock(ctx context.Context, name string, tenantID int64, fn func(scoped *SubscriptionService) error) error {
	if s.tx == nil {
		return fn(s)
	}
	return s.tx.WithTenantLock(ctx, name, tenantID, func(q *dbgen.Queries) error {
		return fn(s.bound(q))
	})
}

// ── Account credit and refunds ─────────────────────────────────────────────

// CreditSummary is an organization's account credit.
type CreditSummary struct {
	Balance  int64
	Currency string
	Entries  []domain.Credit
	Total    int64
}

// Credits returns the tenant's balance (in the default currency) and its
// ledger, newest first.
func (s *SubscriptionService) Credits(ctx context.Context, tenantID int64, limit, offset int32) (*CreditSummary, error) {
	currency, err := s.currencies.DefaultCode(ctx)
	if err != nil {
		return nil, err
	}
	summary := &CreditSummary{Currency: currency, Entries: []domain.Credit{}}
	if s.credits == nil {
		return summary, nil
	}
	if summary.Balance, err = s.credits.Balance(ctx, tenantID, currency); err != nil {
		return nil, err
	}
	if summary.Entries, summary.Total, err = s.credits.ListForTenant(ctx, tenantID, limit, offset); err != nil {
		return nil, err
	}
	return summary, nil
}

// AdjustCredit adds account credit (goodwill, a correction) or takes it back
// (negative), never below zero (staff).
func (s *SubscriptionService) AdjustCredit(ctx context.Context, tenantID, amount int64, reason string, actorID int64) (_ *domain.Credit, err error) {
	ctx, end := tracing.Begin(ctx, "billing.adjust_credit", attribute.Int64("tenant.id", tenantID))
	defer end(&err)
	if amount == 0 {
		return nil, apperr.Validation(map[string][]string{"amount": {"The amount must not be zero."}})
	}
	var credit *domain.Credit
	err = s.inTenantLock(ctx, "billing.subscribe", tenantID, func(scoped *SubscriptionService) error {
		currency, err := scoped.currencies.DefaultCode(ctx)
		if err != nil {
			return err
		}
		balance, err := scoped.credits.Balance(ctx, tenantID, currency)
		if err != nil {
			return err
		}
		if balance+amount < 0 {
			return apperr.Validation(map[string][]string{"amount": {"The account credit cannot go below zero."}})
		}
		credit, err = scoped.credits.Add(ctx, domain.CreditInput{
			TenantID: tenantID, Amount: amount, Currency: currency, Reason: reason, CreatedBy: &actorID,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "billing.credit_adjusted",
		Subject: "billing_credit", SubjectID: credit.ID,
		Changes: map[string]any{"amount": amount, "currency": credit.Currency, "reason": reason},
	})
	s.changed(ctx, tenantID)
	return credit, nil
}

// Refund destinations.
const (
	RefundToCredit   = "credit"
	RefundToOriginal = "original"
)

// RefundInput is a staff refund of (part of) a completed payment.
type RefundInput struct {
	PaymentID int64
	Amount    int64
	Reason    string
	// To is RefundToCredit (account credit) or RefundToOriginal (bKash
	// refunds through the gateway; cash and bank record money paid back by
	// hand).
	To string
	// EndSubscription also ends the plan the payment bought, at once.
	EndSubscription bool
	ActorID         int64
}

// Refund gives money back for a payment (P-12). Nothing is refunded
// automatically anywhere else.
func (s *SubscriptionService) Refund(ctx context.Context, input RefundInput) (_ *domain.Payment, err error) {
	ctx, end := tracing.Begin(ctx, "billing.refund", attribute.Int64("payment.id", input.PaymentID))
	defer end(&err)
	payment, err := s.payments.Get(ctx, input.PaymentID)
	if err != nil {
		return nil, err
	}
	if payment.Status != domain.PaymentCompleted {
		return nil, apperr.New(apperr.InvalidState, "Only a completed payment can be refunded.")
	}
	if input.Amount <= 0 || input.Amount > payment.Amount-payment.RefundedAmount {
		return nil, apperr.Validation(map[string][]string{"amount": {
			fmt.Sprintf("The amount must be between 1 and %d (what is left to refund).", payment.Amount-payment.RefundedAmount),
		}})
	}
	if input.To == RefundToOriginal {
		refunder, ok := s.orchestrator.(Refunder)
		if !ok {
			return nil, apperr.New(apperr.ServiceUnavailable, "Refunds are not available.")
		}
		done, err := refunder.Refund(ctx, payment, input.Amount)
		if err != nil {
			return nil, err
		}
		if !done {
			return nil, apperr.New(apperr.ServiceUnavailable, "The payment gateway did not accept the refund.")
		}
	}
	var refunded *domain.Payment
	err = s.inTenantLock(ctx, "billing.subscribe", payment.TenantID, func(scoped *SubscriptionService) error {
		st := scoped.store()
		var err error
		if refunded, err = st.payments.Refund(ctx, payment.ID, input.Amount); err != nil {
			return err
		}
		if input.To == RefundToCredit && st.credits != nil {
			if _, err := st.credits.Add(ctx, domain.CreditInput{
				TenantID: payment.TenantID, Amount: input.Amount, Currency: payment.Currency,
				Reason:    fmt.Sprintf("Refund of invoice %s: %s", payment.InvoiceNumber, input.Reason),
				PaymentID: &payment.ID, SubscriptionID: payment.SubscriptionID, CreatedBy: &input.ActorID,
			}); err != nil {
				return err
			}
		}
		if input.EndSubscription && payment.SubscriptionID != nil {
			subscription, err := st.subscriptions.Get(ctx, *payment.SubscriptionID)
			if err != nil {
				return err
			}
			switch subscription.Status {
			case domain.StatusActive, domain.StatusTrial, domain.StatusScheduled, domain.StatusPending:
				reason := "Ended with a refund: " + input.Reason
				now := scoped.now()
				if _, err := st.subscriptions.UpdateStatus(ctx, subscription.ID, domain.StatusCancelled, &now, &reason); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: payment.TenantID, ActorID: input.ActorID, Action: "billing.payment_refunded",
		Subject: "payment", SubjectID: payment.ID,
		Changes: map[string]any{
			"invoice_number": payment.InvoiceNumber, "amount": input.Amount, "currency": payment.Currency,
			"to": input.To, "reason": input.Reason, "subscription_ended": input.EndSubscription,
		},
	})
	s.changed(ctx, payment.TenantID)
	return refunded, nil
}

// Refunder is a Gateway that can refund through the payment's driver.
type Refunder interface {
	Refund(ctx context.Context, payment *domain.Payment, amount int64) (bool, error)
}

// ProcessPeriodEnds expires subscriptions whose period ended and starts the
// scheduled ones whose turn came (billing.expire-subscriptions, hourly).
func (s *SubscriptionService) ProcessPeriodEnds(ctx context.Context) (expired, started int64, err error) {
	ctx, end := tracing.Begin(ctx, "billing.process_period_ends")
	defer end(&err)
	run := func(subscriptions domain.SubscriptionRepository) error {
		var err error
		if expired, err = subscriptions.ExpireOverdue(ctx); err != nil {
			return err
		}
		started, err = subscriptions.ActivateDueScheduled(ctx, nil)
		return err
	}
	if s.tx == nil {
		err = run(s.subscriptions)
	} else {
		err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
			return run(domain.NewSubscriptionRepository(q))
		})
	}
	return expired, started, err
}
