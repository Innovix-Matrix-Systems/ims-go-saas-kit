package service

import "context"

// PaymentNotice names a payment people should hear about. The app's wiring
// loads the rest (amount, plan, organization) from the invoice.
type PaymentNotice struct {
	TenantID  int64
	PaymentID int64
	// SubmittedBy is who started the checkout (nil: staff or a gateway).
	SubmittedBy *int64
	// Note is the reviewer's note on a rejection.
	Note *string
}

// PaymentNotifier tells people about a payment's life: staff that a manual
// payment waits for review; the organization that it was submitted,
// received or rejected. Implementations never fail the payment (they log),
// and every call happens after the change is committed.
type PaymentNotifier interface {
	PaymentSubmitted(ctx context.Context, notice PaymentNotice)
	PaymentReceived(ctx context.Context, notice PaymentNotice)
	PaymentRejected(ctx context.Context, notice PaymentNotice)
	PaymentWithdrawn(ctx context.Context, notice PaymentNotice)
	// BillingChanged refreshes the organization's billing screens after a
	// change with no payment notice (a downgrade, a stopped renewal, account
	// credit, a refund).
	BillingChanged(ctx context.Context, tenantID int64)
}

// changed tells open billing screens to refresh (no-op without a notifier).
func (s *SubscriptionService) changed(ctx context.Context, tenantID int64) {
	if s.payNotices != nil {
		s.payNotices.BillingChanged(ctx, tenantID)
	}
}

// WithPaymentNotifier tells people when a checkout is submitted and when a
// gateway payment completes.
func (s *SubscriptionService) WithPaymentNotifier(notifier PaymentNotifier) *SubscriptionService {
	s.payNotices = notifier
	return s
}

// WithPaymentNotifier tells people when staff approve or reject a payment
// and when an organization withdraws one.
func (s *ApprovalService) WithPaymentNotifier(notifier PaymentNotifier) *ApprovalService {
	s.notices = notifier
	return s
}
