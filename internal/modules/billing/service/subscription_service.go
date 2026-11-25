package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// SubscriptionService implements the subscription lifecycle.
type SubscriptionService struct {
	subscriptions domain.SubscriptionRepository
	packages      domain.PackageRepository
	payments      domain.PaymentRepository
	approvals     domain.ApprovalRepository
	credits       domain.CreditRepository
	orchestrator  Gateway
	notifier      Notifier
	tx            *database.TxManager
	now           func() time.Time
	activity      *activity.Recorder
	currencies    domain.CurrencySource
	// payNotices tells staff and the organization about payments (optional).
	payNotices PaymentNotifier
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
	s.credits = domain.NewCreditRepository(tx.Queries())
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
		currencies:    domain.FixedCurrency("USD"),
	}
}

// StatusResult is the tenant's current subscription view.
type StatusResult struct {
	Subscription *domain.Subscription `json:"subscription"`
	Package      *domain.Package      `json:"package"`
	Active       bool                 `json:"active"`
	// Scheduled is the period arranged to follow (a renewal paid early or a
	// free plan), with its plan.
	Scheduled        *domain.Subscription `json:"scheduled"`
	ScheduledPackage *domain.Package      `json:"scheduled_package"`
	// NextPackage is the plan chosen for the renewal (a downgrade).
	NextPackage *domain.Package `json:"next_package"`
	// CreditBalance is the account credit, in Currency.
	CreditBalance int64  `json:"credit_balance"`
	Currency      string `json:"currency"`
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
	if currency, err := s.currencies.DefaultCode(ctx); err == nil {
		result.Currency = currency
		if s.credits != nil {
			if result.CreditBalance, err = s.credits.Balance(ctx, tenantID, currency); err != nil {
				return nil, err
			}
		}
	} else {
		return nil, err
	}
	if !found {
		return result, nil
	}
	result.Subscription = subscription
	pkg, err := s.packages.Get(ctx, subscription.PackageID)
	if err != nil {
		return nil, err
	}
	result.Package = pkg
	inForce := subscription.Status == domain.StatusActive || subscription.Status == domain.StatusTrial ||
		(subscription.Status == domain.StatusScheduled && subscription.StartsAt != nil && !subscription.StartsAt.After(s.now()))
	result.Active = inForce && subscription.ExpiresAt != nil && subscription.ExpiresAt.After(s.now())
	if !result.Active {
		return result, nil
	}
	if subscription.NextPackageID != nil {
		if result.NextPackage, err = s.packages.Get(ctx, *subscription.NextPackageID); err != nil {
			return nil, err
		}
	}
	scheduled, found, err := s.subscriptions.GetScheduledForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if found && scheduled.ID != subscription.ID {
		result.Scheduled = scheduled
		if result.ScheduledPackage, err = s.packages.Get(ctx, scheduled.PackageID); err != nil {
			return nil, err
		}
	}
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

// CheckoutResult is what a checkout did: the subscription (new, scheduled,
// or the current one marked for a downgrade), the payment if one was made,
// and the priced change.
type CheckoutResult struct {
	Subscription *domain.Subscription
	Payment      *domain.Payment
	Change       *Change
}

// Subscribe moves the organization to a plan (docs/guides/billing.md#changing-plans):
// an upgrade or a new plan starts when paid, crediting the unused time of the
// current one; a renewal is paid now and starts when the current period ends;
// a downgrade is remembered and paid at the renewal; account credit is used
// first. It is idempotent per tenant + package + method while a checkout is
// pending (H11), and only one checkout can wait at a time (P-7).
func (s *SubscriptionService) Subscribe(ctx context.Context, tenantID, packageID int64, method string, paymentData map[string]any, actorID int64) (*domain.Subscription, *domain.Payment, error) {
	result, err := s.Checkout(ctx, tenantID, packageID, method, paymentData, actorID)
	if err != nil {
		return nil, nil, err
	}
	return result.Subscription, result.Payment, nil
}

// Checkout is Subscribe returning the priced change as well.
func (s *SubscriptionService) Checkout(ctx context.Context, tenantID, packageID int64, method string, paymentData map[string]any, actorID int64) (_ *CheckoutResult, err error) {
	ctx, end := tracing.Begin(ctx, "billing.subscribe",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("package.id", packageID),
		attribute.String("payment.method", method),
	)
	defer end(&err)
	pkg, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return nil, err
	}
	var result *checkoutResult
	// Every query runs on the locked transaction: a second pool connection
	// here deadlocks once concurrent checkouts for one tenant hold the whole
	// pool while waiting for the lock.
	err = s.inTenantLock(ctx, "billing.subscribe", tenantID, func(scoped *SubscriptionService) error {
		var err error
		result, err = scoped.checkout(ctx, checkoutRequest{
			TenantID: tenantID, ActorID: actorID, Package: pkg, Method: method, PaymentData: paymentData,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.afterCheckout(ctx, tenantID, actorID, pkg, method, result)
	return &CheckoutResult{Subscription: result.Subscription, Payment: result.Payment, Change: result.Change}, nil
}

// afterCheckout records the change and tells people about it (after commit).
func (s *SubscriptionService) afterCheckout(ctx context.Context, tenantID, actorID int64, pkg *domain.Package, method string, result *checkoutResult) {
	tracing.Annotate(ctx, attribute.Int64("subscription.id", result.Subscription.ID), attribute.Bool("subscription.created", result.Created))
	if result.Change == nil {
		return // an existing checkout was returned
	}
	quote := result.Change.Quote
	if !result.Created {
		// A downgrade remembered for the renewal.
		s.record(ctx, "subscription.downgrade_scheduled", result.Subscription, map[string]any{
			"package_id": pkg.ID, "starts_at": quote.PeriodStart, "due_at_renewal": quote.DueAtRenewal,
		})
		s.changed(ctx, tenantID)
		return
	}
	changes := map[string]any{"package_id": pkg.ID, "kind": quote.Kind, "status": result.Subscription.Status}
	if result.Payment != nil {
		changes["method"] = result.Payment.PaymentMethod
		changes["amount"] = result.Payment.Amount
	}
	s.record(ctx, "subscription.created", result.Subscription, changes)
	if result.Payment == nil {
		s.changed(ctx, tenantID)
		return
	}
	if s.payNotices == nil {
		return
	}
	switch {
	case result.Payment.Status == domain.PaymentCompleted:
		s.payNotices.PaymentReceived(ctx, PaymentNotice{TenantID: tenantID, PaymentID: result.Payment.ID, SubmittedBy: &actorID})
	case domain.IsManualMethod(method):
		submitter := actorID
		s.payNotices.PaymentSubmitted(ctx, PaymentNotice{TenantID: tenantID, PaymentID: result.Payment.ID, SubmittedBy: &submitter})
	}
}

// bound returns a copy whose repositories and gateway use q (a transaction).
func (s *SubscriptionService) bound(q *dbgen.Queries) *SubscriptionService {
	scoped := *s
	scoped.subscriptions = domain.NewSubscriptionRepository(q)
	scoped.packages = domain.NewPackageRepository(q)
	scoped.payments = domain.NewPaymentRepository(q)
	scoped.approvals = domain.NewApprovalRepository(q)
	scoped.credits = domain.NewCreditRepository(q)
	if gateway, ok := s.orchestrator.(boundGateway); ok {
		scoped.orchestrator = gateway.WithPayments(scoped.payments)
	}
	return &scoped
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
		return subscriptions.Activate(ctx, subscriptionID, nil, expires)
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
		completed, err := s.completeAndActivate(ctx, payment, gatewayStatus)
		if err == nil && s.payNotices != nil {
			s.payNotices.PaymentReceived(ctx, PaymentNotice{TenantID: payment.TenantID, PaymentID: payment.ID, SubmittedBy: payment.CreatedBy})
		}
		return completed, err
	case result.IsFailed:
		failed, err := s.payments.UpdateStatus(ctx, payment.ID, domain.PaymentFailed, nil, gatewayStatus)
		if err != nil {
			return nil, err
		}
		if err := releaseCredit(ctx, s.credits, failed, "not paid"); err != nil {
			return nil, err
		}
		return failed, nil
	default:
		// Not final yet (e.g. the payer has not finished): keep it pending.
		return s.payments.Get(ctx, payment.ID)
	}
}

// completeAndActivate marks the payment completed and applies it to its
// subscription in one transaction (M9/M11): there is never a completed
// payment without its plan, or the other way round.
func (s *SubscriptionService) completeAndActivate(ctx context.Context, payment *domain.Payment, gatewayStatus *string) (*domain.Payment, error) {
	steps := func(st store) (*domain.Payment, error) {
		completed, err := st.payments.UpdateStatus(ctx, payment.ID, domain.PaymentCompleted, nil, gatewayStatus)
		if err != nil {
			return nil, err
		}
		if _, err := fulfil(ctx, st, completed, s.now()); err != nil {
			return nil, err
		}
		return completed, nil
	}
	if s.tx == nil {
		return steps(s.store())
	}
	var completed *domain.Payment
	err := s.tx.WithTenantLock(ctx, "billing.subscribe", payment.TenantID, func(q *dbgen.Queries) error {
		var err error
		completed, err = steps(storeFor(q))
		return err
	})
	return completed, err
}

// List returns subscriptions with pagination (admin surface).
func (s *SubscriptionService) List(ctx context.Context, status string, limit, offset int32) ([]domain.Subscription, int64, error) {
	return s.subscriptions.List(ctx, status, limit, offset)
}

// ExpireOverdue marks overdue subscriptions as expired and starts the
// scheduled ones whose turn came; it returns how many expired.
func (s *SubscriptionService) ExpireOverdue(ctx context.Context) (int64, error) {
	expired, _, err := s.ProcessPeriodEnds(ctx)
	return expired, err
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
	return domain.PeriodEnd(cycle, from)
}
