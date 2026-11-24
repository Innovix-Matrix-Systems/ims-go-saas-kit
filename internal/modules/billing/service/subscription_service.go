package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
)

// SubscriptionService implements the subscription lifecycle.
type SubscriptionService struct {
	subscriptions domain.SubscriptionRepository
	packages      domain.PackageRepository
	payments      domain.PaymentRepository
	approvals     domain.ApprovalRepository
	orchestrator  Gateway
	notifier      Notifier
	tx            *database.TxManager
	now           func() time.Time
	activity      *activity.Recorder
	currency      string
}

// WithActivity records subscription changes in the tenant's activity log.
func (s *SubscriptionService) WithActivity(recorder *activity.Recorder) *SubscriptionService {
	s.activity = recorder
	return s
}

func (s *SubscriptionService) record(ctx context.Context, action string, subscription *domain.Subscription, changes map[string]any) {
	if subscription == nil {
		return
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: subscription.TenantID, Action: action,
		Subject: "subscription", SubjectID: subscription.ID, Changes: changes,
	})
}

// WithTx makes payment completion + activation atomic and serializes
// concurrent subscribes per tenant. Without it (unit tests with fakes) the
// same steps run without a transaction.
func (s *SubscriptionService) WithTx(tx *database.TxManager) *SubscriptionService {
	s.tx = tx
	return s
}

// NewSubscriptionService creates the subscription service.
func NewSubscriptionService(
	subscriptions domain.SubscriptionRepository,
	packages domain.PackageRepository,
	payments domain.PaymentRepository,
	approvals domain.ApprovalRepository,
	orchestrator Gateway,
) *SubscriptionService {
	return &SubscriptionService{
		subscriptions: subscriptions,
		packages:      packages,
		payments:      payments,
		approvals:     approvals,
		orchestrator:  orchestrator,
		now:           time.Now,
		currency:      "BDT",
	}
}

// StatusResult is the tenant's current subscription view.
type StatusResult struct {
	Subscription *domain.Subscription `json:"subscription"`
	Package      *domain.Package      `json:"package"`
	Active       bool                 `json:"active"`
}

// Status returns the tenant's active subscription, falling back to the latest
// one (a pending/cancelled row) when no plan is currently active.
func (s *SubscriptionService) Status(ctx context.Context, tenantID int64) (*StatusResult, error) {
	subscription, found, err := s.subscriptions.GetActiveForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !found {
		subscription, found, err = s.subscriptions.GetLatestForTenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
	}
	result := &StatusResult{}
	if !found {
		return result, nil
	}
	result.Subscription = subscription
	pkg, err := s.packages.Get(ctx, subscription.PackageID)
	if err != nil {
		return nil, err
	}
	result.Package = pkg
	result.Active = (subscription.Status == domain.StatusActive || subscription.Status == domain.StatusTrial) &&
		subscription.ExpiresAt != nil && subscription.ExpiresAt.After(s.now())
	return result, nil
}

// GetForTenant loads a subscription and verifies it belongs to the tenant.
func (s *SubscriptionService) GetForTenant(ctx context.Context, tenantID, subscriptionID int64) (*domain.Subscription, error) {
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if subscription.TenantID != tenantID {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Subscription with ID '%d' was not found.", subscriptionID)
	}
	return subscription, nil
}

// StartTrial starts a trial subscription when the package offers one.
func (s *SubscriptionService) StartTrial(ctx context.Context, tenantID, packageID int64) (_ *domain.Subscription, err error) {
	ctx, end := tracing.Begin(ctx, "billing.start_trial",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("package.id", packageID),
	)
	defer end(&err)
	pkg, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg.TrialDays <= 0 {
		return nil, apperr.New(apperr.BusinessLogicError, "This package does not offer a trial period.")
	}

	now := s.now()
	expires := now.AddDate(0, 0, int(pkg.TrialDays))
	if err := s.subscriptions.DeactivateActive(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.subscriptions.Create(ctx, domain.SubscriptionInput{
		TenantID:    tenantID,
		PackageID:   pkg.ID,
		Status:      domain.StatusTrial,
		StartsAt:    &now,
		TrialEndsAt: &expires,
		ExpiresAt:   &expires,
	})
}

// Subscribe creates a pending subscription, charges the selected gateway and
// (for manual methods) creates the staff approval request. It is idempotent
// per tenant + package + method: while a checkout is still pending, a retry
// or double click returns that checkout instead of charging again (H11).
func (s *SubscriptionService) Subscribe(ctx context.Context, tenantID, packageID int64, method string, paymentData map[string]any, actorID int64) (_ *domain.Subscription, _ *domain.Payment, err error) {
	ctx, end := tracing.Begin(ctx, "billing.subscribe",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("package.id", packageID),
		attribute.String("payment.method", method),
	)
	defer end(&err)
	// A free plan has nothing to pay or approve: it starts at once, the way
	// staff assign a plan (no payment is recorded).
	if pkg, err := s.packages.Get(ctx, packageID); err != nil {
		return nil, nil, err
	} else if pkg.Price == 0 {
		if !pkg.IsActive {
			return nil, nil, apperr.New(apperr.BusinessLogicError, "This package is not available.")
		}
		return s.Assign(ctx, AssignInput{TenantID: tenantID, PackageID: packageID, ActorID: actorID})
	}
	var (
		subscription *domain.Subscription
		payment      *domain.Payment
	)
	var created bool
	run := func(scoped *SubscriptionService) error {
		var err error
		subscription, payment, created, err = scoped.subscribe(ctx, tenantID, packageID, method, paymentData, actorID)
		return err
	}
	if s.tx == nil {
		err = run(s)
	} else {
		// Every query runs on the locked transaction: a second pool
		// connection here deadlocks once concurrent checkouts for one tenant
		// hold the whole pool while waiting for the lock.
		err = s.tx.WithTenantLock(ctx, "billing.subscribe", tenantID, func(q *dbgen.Queries) error {
			return run(s.bound(q))
		})
	}
	if err != nil {
		return nil, nil, err
	}
	tracing.Annotate(ctx, attribute.Int64("subscription.id", subscription.ID), attribute.Bool("subscription.created", created))
	if created {
		s.record(ctx, "subscription.created", subscription, map[string]any{
			"package_id": packageID, "method": method, "status": subscription.Status,
		})
	}
	return subscription, payment, nil
}

// bound returns a copy whose repositories and gateway use q (a transaction).
func (s *SubscriptionService) bound(q *dbgen.Queries) *SubscriptionService {
	scoped := *s
	scoped.subscriptions = domain.NewSubscriptionRepository(q)
	scoped.packages = domain.NewPackageRepository(q)
	scoped.payments = domain.NewPaymentRepository(q)
	scoped.approvals = domain.NewApprovalRepository(q)
	if gateway, ok := s.orchestrator.(boundGateway); ok {
		scoped.orchestrator = gateway.WithPayments(scoped.payments)
	}
	return &scoped
}

func (s *SubscriptionService) subscribe(ctx context.Context, tenantID, packageID int64, method string, paymentData map[string]any, actorID int64) (*domain.Subscription, *domain.Payment, bool, error) {
	switch method {
	case domain.MethodBkash, domain.MethodNagad, domain.MethodCash, domain.MethodBankTransfer:
	default:
		return nil, nil, false, apperr.Validation(map[string][]string{
			"payment_method": {"The selected payment method is invalid."},
		})
	}

	pkg, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return nil, nil, false, err
	}
	if !pkg.IsActive {
		return nil, nil, false, apperr.New(apperr.BusinessLogicError, "This package is not available.")
	}

	if existing, found, err := s.payments.PendingCheckout(ctx, tenantID, pkg.ID, method); err != nil {
		return nil, nil, false, err
	} else if found && existing.SubscriptionID != nil {
		subscription, err := s.subscriptions.Get(ctx, *existing.SubscriptionID)
		if err != nil {
			return nil, nil, false, err
		}
		return subscription, existing, false, nil
	}

	now := s.now()
	expires := nextExpiry(pkg.BillingCycle, now)
	subscription, err := s.subscriptions.Create(ctx, domain.SubscriptionInput{
		TenantID:  tenantID,
		PackageID: pkg.ID,
		Status:    domain.StatusPending,
		StartsAt:  &now,
		ExpiresAt: &expires,
	})
	if err != nil {
		return nil, nil, false, err
	}

	payment, err := s.orchestrator.Charge(ctx, tenantID, &subscription.ID, method, pkg.Price, paymentData, &actorID)
	if err != nil {
		reason := "Payment initiation failed."
		_, _ = s.subscriptions.UpdateStatus(ctx, subscription.ID, domain.StatusCancelled, &now, &reason)
		return nil, nil, false, err
	}

	if domain.IsManualMethod(method) {
		encoded, _ := json.Marshal(paymentData)
		if _, err := s.approvals.Create(ctx, domain.ApprovalInput{
			TenantID:       tenantID,
			SubscriptionID: subscription.ID,
			PaymentID:      payment.ID,
			PaymentMethod:  method,
			PaymentData:    encoded,
			CreatedBy:      &actorID,
		}); err != nil {
			reason := "Payment approval could not be created."
			_, _ = s.subscriptions.UpdateStatus(ctx, subscription.ID, domain.StatusCancelled, &now, &reason)
			_, _ = s.payments.UpdateStatus(ctx, payment.ID, domain.PaymentFailed, nil, nil)
			return nil, nil, false, err
		}
	}

	return subscription, payment, true, nil
}

// Activate marks a subscription active (renewals, callbacks, approvals).
func (s *SubscriptionService) Activate(ctx context.Context, subscriptionID int64) (_ *domain.Subscription, err error) {
	ctx, end := tracing.Begin(ctx, "billing.activate_subscription", attribute.Int64("subscription.id", subscriptionID))
	defer end(&err)
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	expires := s.expiryForActivation(ctx, subscription)
	steps := func(subscriptions domain.SubscriptionRepository) (*domain.Subscription, error) {
		if err := subscriptions.DeactivateActive(ctx, subscription.TenantID); err != nil {
			return nil, err
		}
		return subscriptions.Activate(ctx, subscriptionID, expires)
	}
	var activated *domain.Subscription
	if s.tx == nil {
		activated, err = steps(s.subscriptions)
	} else {
		err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
			var err error
			activated, err = steps(domain.NewSubscriptionRepository(q))
			return err
		})
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, "subscription.activated", activated, map[string]any{"expires_at": activated.ExpiresAt})
	return activated, nil
}

// Cancel ends a subscription immediately: the guard only honours active and
// trial subscriptions, so access stops now (M14 decision). To stop renewal
// but keep access until expiry, simply do not renew.
func (s *SubscriptionService) Cancel(ctx context.Context, subscriptionID int64, reason *string) (_ *domain.Subscription, err error) {
	ctx, end := tracing.Begin(ctx, "billing.cancel_subscription", attribute.Int64("subscription.id", subscriptionID))
	defer end(&err)
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if subscription.Status == domain.StatusCancelled || subscription.Status == domain.StatusExpired {
		return nil, apperr.New(apperr.InvalidState, "This subscription has already ended.")
	}
	if reason == nil || *reason == "" {
		value := "Cancelled by administrator."
		reason = &value
	}
	now := s.now()
	cancelled, err := s.subscriptions.UpdateStatus(ctx, subscriptionID, domain.StatusCancelled, &now, reason)
	if err != nil {
		return nil, err
	}
	s.record(ctx, "subscription.cancelled", cancelled, map[string]any{"reason": *reason})
	return cancelled, nil
}

// ApplyOverrides merges per-tenant feature overrides (super-admin only). A
// null value removes that override, so the feature falls back to the plan.
func (s *SubscriptionService) ApplyOverrides(ctx context.Context, subscriptionID int64, overrides map[string]any) (_ *domain.Subscription, err error) {
	ctx, end := tracing.Begin(ctx, "billing.apply_overrides", attribute.Int64("subscription.id", subscriptionID))
	defer end(&err)
	subscription, err := s.subscriptions.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	for key, value := range overrides {
		if !domain.Feature(key).IsKnown() {
			return nil, apperr.Validation(map[string][]string{
				"overrides": {"Unknown feature key '" + key + "'."},
			})
		}
		switch value.(type) {
		case nil, bool, float64, int, int64:
		default:
			return nil, apperr.Validation(map[string][]string{
				"overrides": {"Override values must be booleans, numbers or null (remove)."},
			})
		}
	}
	merged := make(map[string]any, len(subscription.Overrides)+len(overrides))
	for key, value := range subscription.Overrides {
		merged[key] = value
	}
	for key, value := range overrides {
		if value == nil {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "encode overrides", err)
	}
	return s.subscriptions.SetOverrides(ctx, subscriptionID, encoded)
}

// HandleGatewayCallback finalizes a gateway payment and activates its
// subscription on success. Completed or refunded payments are returned
// unchanged so repeated callbacks are idempotent.
func (s *SubscriptionService) HandleGatewayCallback(ctx context.Context, paymentID int64, method string, payload map[string]any) (_ *domain.Payment, err error) {
	ctx, end := tracing.Begin(ctx, "billing.payment_callback",
		attribute.Int64("payment.id", paymentID),
		attribute.String("payment.method", method),
	)
	defer end(&err)
	if domain.IsManualMethod(method) {
		return nil, apperr.New(apperr.InvalidInput, "Manual payment methods do not accept callbacks.")
	}
	payment, err := s.payments.Get(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if payment.PaymentMethod != method {
		return nil, apperr.New(apperr.InvalidInput, "The callback gateway does not match the payment method.")
	}
	if payment.Status == domain.PaymentCompleted || payment.Status == domain.PaymentRefunded {
		return payment, nil
	}

	result, err := s.orchestrator.HandleCallback(ctx, method, payload)
	if err != nil {
		return nil, err
	}

	response, _ := json.Marshal(result.RawResponse)
	var gatewayStatus *string
	if result.GatewayStatus != nil && *result.GatewayStatus != "" {
		gatewayStatus = result.GatewayStatus
	}
	// Keep the original gateway reference (the value the callback lookup and
	// verify/refund calls rely on); the gateway transaction id lives in the
	// stored response payload.
	gatewayRef := result.GatewayRef
	if payment.GatewayRef != nil {
		gatewayRef = nil
	}
	_ = s.payments.UpdateGateway(ctx, payment.ID, gatewayRef, gatewayStatus, response)

	switch {
	case result.IsSuccessful:
		return s.completeAndActivate(ctx, payment, gatewayStatus)
	case result.IsFailed:
		return s.payments.UpdateStatus(ctx, payment.ID, domain.PaymentFailed, nil, gatewayStatus)
	default:
		// Not final yet (e.g. the payer has not finished): keep it pending.
		return s.payments.Get(ctx, payment.ID)
	}
}

// completeAndActivate marks the payment completed and activates its
// subscription in one transaction (M9/M11): there is never a completed
// payment without an active plan, or the other way round.
func (s *SubscriptionService) completeAndActivate(ctx context.Context, payment *domain.Payment, gatewayStatus *string) (*domain.Payment, error) {
	steps := func(payments domain.PaymentRepository, subscriptions domain.SubscriptionRepository) (*domain.Payment, error) {
		completed, err := payments.UpdateStatus(ctx, payment.ID, domain.PaymentCompleted, nil, gatewayStatus)
		if err != nil {
			return nil, err
		}
		if payment.SubscriptionID != nil {
			subscription, err := subscriptions.Get(ctx, *payment.SubscriptionID)
			if err != nil {
				return nil, err
			}
			expires := s.expiryForActivation(ctx, subscription)
			if err := subscriptions.DeactivateActive(ctx, subscription.TenantID); err != nil {
				return nil, err
			}
			if _, err := subscriptions.Activate(ctx, subscription.ID, expires); err != nil {
				return nil, err
			}
		}
		return completed, nil
	}
	if s.tx == nil {
		return steps(s.payments, s.subscriptions)
	}
	var completed *domain.Payment
	err := s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		var err error
		completed, err = steps(domain.NewPaymentRepository(q), domain.NewSubscriptionRepository(q))
		return err
	})
	return completed, err
}

// List returns subscriptions with pagination (admin surface).
func (s *SubscriptionService) List(ctx context.Context, status string, limit, offset int32) ([]domain.Subscription, int64, error) {
	return s.subscriptions.List(ctx, status, limit, offset)
}

// ExpireOverdue marks overdue subscriptions as expired.
func (s *SubscriptionService) ExpireOverdue(ctx context.Context) (_ int64, err error) {
	ctx, end := tracing.Begin(ctx, "billing.expire_overdue")
	defer end(&err)
	return s.subscriptions.ExpireOverdue(ctx)
}

// SuspendLapsedTenants suspends every tenant whose subscription expired more
// than grace ago and that has no current, trial, pending or past-due
// subscription (BILLING_SUSPEND_ON_EXPIRY). keepTenantID (the default tenant)
// is never suspended. Each suspension is a tenant.suspended system row.
func (s *SubscriptionService) SuspendLapsedTenants(ctx context.Context, grace time.Duration, keepTenantID int64) (_ []domain.LapsedTenant, err error) {
	ctx, end := tracing.Begin(ctx, "billing.suspend_lapsed_tenants")
	defer end(&err)
	lapsed, err := s.subscriptions.SuspendLapsedTenants(ctx, s.now().Add(-grace), keepTenantID)
	if err != nil {
		return nil, err
	}
	for _, tenant := range lapsed {
		s.activity.Record(ctx, activity.Entry{
			Action: activity.ActionTenantSuspended, Subject: "tenant", SubjectID: tenant.ID,
			Changes: map[string]any{
				"name": tenant.Name, "code": tenant.Code, "is_active": false, "reason": "subscription_expired",
			},
		})
	}
	return lapsed, nil
}

// Notifier delivers in-app notifications for billing events (implemented by
// the notifications module; nil keeps the log-only behavior).
type Notifier interface {
	NotifyRenewalReminder(ctx context.Context, tenantID, subscriptionID int64, expiresAt time.Time, daysLeft int) error
}

// WithNotifier attaches the in-app notification notifier.
func (s *SubscriptionService) WithNotifier(notifier Notifier) *SubscriptionService {
	s.notifier = notifier
	return s
}

// SendRenewalReminders creates in-app reminders for subscriptions expiring
// within the configured day windows (logs when no notifier is attached).
func (s *SubscriptionService) SendRenewalReminders(ctx context.Context, logger *slog.Logger, days []int) (_ int, err error) {
	ctx, end := tracing.Begin(ctx, "billing.send_renewal_reminders")
	defer end(&err)
	if len(days) == 0 {
		days = []int{7, 3, 1}
	}
	maxDays := 0
	for _, day := range days {
		if day > maxDays {
			maxDays = day
		}
	}
	now := s.now()
	subscriptions, err := s.subscriptions.Expiring(ctx, now.AddDate(0, 0, maxDays))
	if err != nil {
		return 0, err
	}

	sent := 0
	for _, subscription := range subscriptions {
		if subscription.ExpiresAt == nil {
			continue
		}
		daysLeft := int(math.Ceil(subscription.ExpiresAt.Sub(now).Hours() / 24))
		for _, day := range days {
			if daysLeft != day {
				continue
			}
			if s.notifier != nil {
				if err := s.notifier.NotifyRenewalReminder(ctx, subscription.TenantID, subscription.ID, *subscription.ExpiresAt, daysLeft); err != nil {
					logger.Error("renewal reminder notification failed",
						"tenant_id", subscription.TenantID,
						"subscription_id", subscription.ID,
						"error", err,
					)
				} else {
					sent++
				}
			} else {
				logger.Info("renewal reminder queued",
					"tenant_id", subscription.TenantID,
					"subscription_id", subscription.ID,
					"days_left", daysLeft,
				)
				sent++
			}
			break
		}
	}
	return sent, nil
}

// Expiring returns subscriptions expiring before the given time.
func (s *SubscriptionService) Expiring(ctx context.Context, before time.Time) ([]domain.Subscription, error) {
	return s.subscriptions.Expiring(ctx, before)
}

func (s *SubscriptionService) expiryForActivation(ctx context.Context, subscription *domain.Subscription) time.Time {
	now := s.now()
	if subscription.ExpiresAt != nil && subscription.ExpiresAt.After(now) {
		return *subscription.ExpiresAt
	}
	pkg, err := s.packages.Get(ctx, subscription.PackageID)
	if err != nil {
		return now.AddDate(0, 1, 0)
	}
	return nextExpiry(pkg.BillingCycle, now)
}

func nextExpiry(cycle string, from time.Time) time.Time {
	switch cycle {
	case domain.CycleYearly:
		return from.AddDate(1, 0, 0)
	case domain.CycleOneTime:
		return from.AddDate(100, 0, 0)
	default:
		return from.AddDate(0, 1, 0)
	}
}
