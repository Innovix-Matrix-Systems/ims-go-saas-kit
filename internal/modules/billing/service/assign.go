package service

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// AssignInput moves a tenant onto a plan from the admin side (support staff,
// sales deals, complimentary accounts).
type AssignInput struct {
	TenantID  int64
	PackageID int64
	ActorID   int64
	// PaymentMethod is "cash" or "bank_transfer" to record what the tenant
	// paid; empty assigns the plan without a payment (complimentary).
	PaymentMethod string
	// Amount defaults to the package price when a payment method is given
	// (AssignReplace), or to the amount due (AssignChange).
	Amount *int64
	// Mode is AssignReplace (the default: end the current plan now, no
	// credit) or AssignChange (the customer's rules: unused-time credit,
	// account credit, downgrades and renewals at the end of the period).
	Mode string
	// Cycle is the billing cycle ("" is the plan's own).
	Cycle string
}

// Assign modes.
const (
	AssignReplace = "replace"
	AssignChange  = "change"
)

// WithCurrencies sets where the currency of payments recorded by Assign
// comes from (the default currency; USD until set).
func (s *SubscriptionService) WithCurrencies(currencies domain.CurrencySource) *SubscriptionService {
	s.currencies = currencies
	return s
}

// Assign ends the tenant's current subscription and starts an active one on
// the package, recording a completed manual payment when a method is given.
// It runs in one transaction under the same per-tenant lock as Subscribe.
// Hidden (inactive) packages can be assigned; overrides start empty because
// they belong to the subscription, not the tenant.
func (s *SubscriptionService) Assign(ctx context.Context, input AssignInput) (_ *domain.Subscription, _ *domain.Payment, err error) {
	ctx, end := tracing.Begin(ctx, "billing.assign_plan",
		attribute.Int64("tenant.id", input.TenantID),
		attribute.Int64("package.id", input.PackageID),
	)
	defer end(&err)
	if input.Mode == AssignChange {
		return s.assignChange(ctx, input)
	}
	if input.PaymentMethod != "" && !domain.IsManualMethod(input.PaymentMethod) {
		return nil, nil, apperr.Validation(map[string][]string{
			"payment_method": {"Only cash and bank transfer payments can be recorded manually."},
		})
	}
	if input.PaymentMethod == "" && input.Amount != nil {
		return nil, nil, apperr.Validation(map[string][]string{
			"payment_method": {"A payment method is required when an amount is given."},
		})
	}
	base, err := s.packages.Get(ctx, input.PackageID)
	if err != nil {
		return nil, nil, err
	}
	offered, err := base.ForCycle(input.Cycle)
	if err != nil {
		return nil, nil, err
	}
	pkg := &offered
	amount := pkg.Price
	if input.Amount != nil {
		amount = *input.Amount
	}
	if input.PaymentMethod != "" && amount <= 0 {
		return nil, nil, apperr.Validation(map[string][]string{
			"amount": {"The amount must be greater than zero."},
		})
	}
	currency, err := s.currencies.DefaultCode(ctx)
	if err != nil {
		return nil, nil, err
	}

	var (
		subscription *domain.Subscription
		payment      *domain.Payment
	)
	steps := func(subscriptions domain.SubscriptionRepository, payments domain.PaymentRepository) error {
		now := s.now()
		expires := nextExpiry(pkg.BillingCycle, now)
		if err := subscriptions.DeactivateActive(ctx, input.TenantID); err != nil {
			return err
		}
		created, err := subscriptions.Create(ctx, domain.SubscriptionInput{
			TenantID:  input.TenantID,
			PackageID: pkg.ID,
			Status:    domain.StatusActive,
			StartsAt:  &now,
			ExpiresAt: &expires,
			// The cycle chosen, so renewals and invoices follow it.
			BillingCycle: pkg.BillingCycle,
		})
		if err != nil {
			return err
		}
		subscription = created
		if input.PaymentMethod == "" {
			return nil
		}
		invoice, err := domain.GenerateInvoiceNumber(now)
		if err != nil {
			return err
		}
		pending, err := payments.Create(ctx, domain.PaymentInput{
			TenantID:       input.TenantID,
			SubscriptionID: &created.ID,
			PaymentMethod:  input.PaymentMethod,
			Status:         domain.PaymentPending,
			Amount:         amount,
			Currency:       currency,
			InvoiceNumber:  invoice,
			CreatedBy:      &input.ActorID,
		})
		if err != nil {
			return err
		}
		payment, err = payments.UpdateStatus(ctx, pending.ID, domain.PaymentCompleted, &input.ActorID, strPtr("manual"))
		return err
	}
	if s.tx == nil {
		err = steps(s.subscriptions, s.payments)
	} else {
		err = s.tx.WithTenantLock(ctx, "billing.subscribe", input.TenantID, func(q *dbgen.Queries) error {
			return steps(domain.NewSubscriptionRepository(q), domain.NewPaymentRepository(q))
		})
	}
	if err != nil {
		return nil, nil, err
	}

	changes := map[string]any{"package_id": pkg.ID, "billing_cycle": pkg.BillingCycle, "assigned_by": input.ActorID}
	if payment != nil {
		changes["payment_method"] = payment.PaymentMethod
		changes["amount"] = payment.Amount
	}
	s.record(ctx, "subscription.assigned", subscription, changes)
	return subscription, payment, nil
}

// assignChange applies the customer's rules for staff (P-15): the payment is
// recorded as received at once.
func (s *SubscriptionService) assignChange(ctx context.Context, input AssignInput) (*domain.Subscription, *domain.Payment, error) {
	if input.PaymentMethod != "" && !domain.IsManualMethod(input.PaymentMethod) {
		return nil, nil, apperr.Validation(map[string][]string{
			"payment_method": {"Only cash and bank transfer payments can be recorded manually."},
		})
	}
	pkg, err := s.packages.Get(ctx, input.PackageID)
	if err != nil {
		return nil, nil, err
	}
	var result *checkoutResult
	err = s.inTenantLock(ctx, "billing.subscribe", input.TenantID, func(scoped *SubscriptionService) error {
		var err error
		result, err = scoped.checkout(ctx, checkoutRequest{
			TenantID: input.TenantID, ActorID: input.ActorID, Package: pkg, Cycle: input.Cycle,
			Method: input.PaymentMethod, Staff: true, Amount: input.Amount,
		})
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	s.afterCheckout(ctx, input.TenantID, input.ActorID, pkg, input.PaymentMethod, result)
	return result.Subscription, result.Payment, nil
}
