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
	// Amount defaults to the package price when a payment method is given.
	Amount *int64
}

// WithCurrency sets the currency of payments recorded by Assign.
func (s *SubscriptionService) WithCurrency(currency string) *SubscriptionService {
	s.currency = currency
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
	pkg, err := s.packages.Get(ctx, input.PackageID)
	if err != nil {
		return nil, nil, err
	}
	amount := pkg.Price
	if input.Amount != nil {
		amount = *input.Amount
	}
	if input.PaymentMethod != "" && amount <= 0 {
		return nil, nil, apperr.Validation(map[string][]string{
			"amount": {"The amount must be greater than zero."},
		})
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
			Currency:       s.currency,
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

	changes := map[string]any{"package_id": pkg.ID, "assigned_by": input.ActorID}
	if payment != nil {
		changes["payment_method"] = payment.PaymentMethod
		changes["amount"] = payment.Amount
	}
	s.record(ctx, "subscription.assigned", subscription, changes)
	return subscription, payment, nil
}
