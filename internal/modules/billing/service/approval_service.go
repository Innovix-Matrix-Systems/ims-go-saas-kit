package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// ApprovalService implements the manual payment review workflow.
type ApprovalService struct {
	approvals     domain.ApprovalRepository
	payments      domain.PaymentRepository
	subscriptions domain.SubscriptionRepository
	tx            *database.TxManager
	activity      *activity.Recorder
	// notices tells the organization (and staff) about reviews (optional).
	notices PaymentNotifier
}

// NewApprovalService creates the approval service.
func NewApprovalService(approvals domain.ApprovalRepository, payments domain.PaymentRepository, subscriptions domain.SubscriptionRepository, tx *database.TxManager) *ApprovalService {
	return &ApprovalService{approvals: approvals, payments: payments, subscriptions: subscriptions, tx: tx}
}

// WithActivity records withdrawn checkouts in the organization's activity log.
func (s *ApprovalService) WithActivity(recorder *activity.Recorder) *ApprovalService {
	s.activity = recorder
	return s
}

// Withdraw lets an organization take back a checkout nobody has confirmed yet
// (a wrong transfer reference, a change of mind): the pending payment and its
// pending subscription are cancelled and the approval request is closed as
// rejected with a note, so staff no longer see it waiting.
func (s *ApprovalService) Withdraw(ctx context.Context, tenantID, paymentID, actorID int64) (err error) {
	ctx, end := tracing.Begin(ctx, "billing.withdraw_payment",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("payment.id", paymentID),
	)
	defer end(&err)
	payment, err := s.payments.Get(ctx, paymentID)
	if err != nil {
		return err
	}
	if payment.TenantID != tenantID {
		return apperr.Newf(apperr.ResourceNotFound, "Payment with ID '%d' was not found.", paymentID)
	}
	if payment.Status != domain.PaymentPending {
		return apperr.New(apperr.InvalidState, "Only a payment that is still waiting can be withdrawn.")
	}
	note := "Withdrawn by the organization."
	gatewayStatus := "withdrawn by the organization"
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		if approval, err := q.PendingApprovalForPayment(ctx, payment.ID); err == nil {
			if _, err := q.UpdatePaymentApproval(ctx, dbgen.UpdatePaymentApprovalParams{
				ID: approval.ID, Status: domain.ApprovalRejected, ReviewerNote: &note, UpdatedBy: &actorID,
			}); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := q.UpdatePaymentStatus(ctx, dbgen.UpdatePaymentStatusParams{
			ID: payment.ID, Status: domain.PaymentCancelled, GatewayStatus: &gatewayStatus,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.New(apperr.InvalidState, "Only a payment that is still waiting can be withdrawn.")
			}
			return err
		}
		if err := releaseCredit(ctx, domain.NewCreditRepository(q), payment, "withdrawn"); err != nil {
			return err
		}
		if payment.SubscriptionID == nil {
			return nil
		}
		subscription, err := q.GetSubscription(ctx, *payment.SubscriptionID)
		if err != nil || subscription.Status != domain.StatusPending {
			return err
		}
		_, err = q.UpdateSubscriptionStatus(ctx, dbgen.UpdateSubscriptionStatusParams{
			ID: subscription.ID, Status: domain.StatusCancelled, CancelledAt: timestamptz(time.Now()), CancellationReason: &note,
		})
		return err
	})
	if err != nil {
		return err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantID, ActorID: actorID, Action: "billing.payment_withdrawn",
		Subject: "payment", SubjectID: payment.ID,
		Changes: map[string]any{"invoice_number": payment.InvoiceNumber, "amount": payment.Amount, "currency": payment.Currency},
	})
	if s.notices != nil {
		s.notices.PaymentWithdrawn(ctx, PaymentNotice{TenantID: tenantID, PaymentID: payment.ID})
	}
	return nil
}

// ApprovalDetail is what a reviewer sees beside an approval.
type ApprovalDetail struct {
	TenantName    string
	PackageName   string
	Amount        int64
	Currency      string
	InvoiceNumber string
	// Kind and Lines are the checkout quote (domain.Change*, invoice lines).
	Kind  string
	Lines []domain.InvoiceLine
	// QuoteStale: the plan whose unused time was credited is no longer the
	// one in force (staff changed it, or it ended), so the amount may be
	// wrong (P-16).
	QuoteStale bool
	// OverridesDropped is how many per-tenant overrides the change removes.
	OverridesDropped int
}

// Details returns the payer, plan, amount and invoice for approvals by id.
// It reads nothing when the tx manager is absent (unit tests).
func (s *ApprovalService) Details(ctx context.Context, ids []int64) (map[int64]ApprovalDetail, error) {
	out := make(map[int64]ApprovalDetail, len(ids))
	if s.tx == nil || len(ids) == 0 {
		return out, nil
	}
	rows, err := s.tx.Queries().ApprovalDetailsByIDs(ctx, ids)
	if err != nil {
		return nil, database.MapError("billing.approval_details", err)
	}
	subscriptions := domain.NewSubscriptionRepository(s.tx.Queries())
	for _, row := range rows {
		detail := ApprovalDetail{TenantName: row.TenantName, PackageName: row.PackageName,
			Amount: row.Amount, Currency: row.Currency, InvoiceNumber: row.InvoiceNumber,
			Kind: row.Kind, Lines: domain.DecodeLines(row.Lines)}
		{
			current, found, err := subscriptions.GetActiveForTenant(ctx, row.TenantID)
			if err != nil {
				return nil, err
			}
			if row.ReplacesSubscriptionID != nil {
				detail.QuoteStale = !found || current.ID != *row.ReplacesSubscriptionID
			}
			if found && row.Kind != domain.ChangeRenew {
				detail.OverridesDropped = len(current.Overrides)
			}
		}
		out[row.ID] = detail
	}
	return out, nil
}

// List returns approval requests with pagination.
func (s *ApprovalService) List(ctx context.Context, status string, limit, offset int32) ([]domain.Approval, int64, error) {
	return s.approvals.List(ctx, status, limit, offset)
}

// Approve completes the payment and applies it to its subscription
// atomically (the amount received is the invoice amount).
func (s *ApprovalService) Approve(ctx context.Context, id, reviewerID int64, note *string) (*domain.Approval, error) {
	return s.ApproveReceived(ctx, id, reviewerID, note, nil)
}

// ApproveReceived approves with the amount staff actually received: more
// than the invoice becomes account credit; less is refused (reject it, or ask
// for the rest first).
func (s *ApprovalService) ApproveReceived(ctx context.Context, id, reviewerID int64, note *string, received *int64) (_ *domain.Approval, err error) {
	ctx, end := tracing.Begin(ctx, "billing.approve_payment", attribute.Int64("approval.id", id))
	defer end(&err)
	approval, err := s.approvals.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if approval.Status != domain.ApprovalPending {
		return nil, apperr.New(apperr.InvalidState, "This approval request has already been reviewed.")
	}
	payment, err := s.payments.Get(ctx, approval.PaymentID)
	if err != nil {
		return nil, err
	}
	var extra int64
	if received != nil {
		if *received < payment.Amount {
			return nil, apperr.Validation(map[string][]string{"received_amount": {
				"Less than the invoice amount was received. Reject the payment, or ask for the rest before approving.",
			}})
		}
		extra = *received - payment.Amount
	}

	gatewayStatus := "approved by staff"
	err = s.tx.WithTenantLock(ctx, "billing.subscribe", approval.TenantID, func(q *dbgen.Queries) error {
		if _, err := q.UpdatePaymentApproval(ctx, dbgen.UpdatePaymentApprovalParams{
			ID:           approval.ID,
			Status:       domain.ApprovalApproved,
			ReviewerID:   &reviewerID,
			ReviewerNote: note,
			UpdatedBy:    &reviewerID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.New(apperr.InvalidState, "This approval request has already been reviewed.")
			}
			return err
		}
		st := storeFor(q)
		completed, err := st.payments.UpdateStatus(ctx, approval.PaymentID, domain.PaymentCompleted, &reviewerID, &gatewayStatus)
		if err != nil {
			return err
		}
		if _, err := fulfil(ctx, st, completed, time.Now()); err != nil {
			return err
		}
		if extra > 0 {
			if _, err := st.credits.Add(ctx, domain.CreditInput{
				TenantID: completed.TenantID, Amount: extra, Currency: completed.Currency,
				Reason:    "Paid more than invoice " + completed.InvoiceNumber + ".",
				PaymentID: &completed.ID, SubscriptionID: completed.SubscriptionID, CreatedBy: &reviewerID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		return nil, database.MapError("domain.approve_payment", err)
	}
	if s.notices != nil {
		s.notices.PaymentReceived(ctx, PaymentNotice{TenantID: approval.TenantID, PaymentID: approval.PaymentID, SubmittedBy: approval.CreatedBy})
	}
	return s.approvals.Get(ctx, id)
}

// Reject cancels the subscription and fails the payment atomically.
func (s *ApprovalService) Reject(ctx context.Context, id, reviewerID int64, note *string) (_ *domain.Approval, err error) {
	ctx, end := tracing.Begin(ctx, "billing.reject_payment", attribute.Int64("approval.id", id))
	defer end(&err)
	approval, err := s.approvals.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if approval.Status != domain.ApprovalPending {
		return nil, apperr.New(apperr.InvalidState, "This approval request has already been reviewed.")
	}

	gatewayStatus := "rejected by staff"
	reason := "Payment rejected by reviewer."
	if note != nil && *note != "" {
		reason = *note
	}
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		if _, err := q.UpdatePaymentApproval(ctx, dbgen.UpdatePaymentApprovalParams{
			ID:           approval.ID,
			Status:       domain.ApprovalRejected,
			ReviewerID:   &reviewerID,
			ReviewerNote: note,
			UpdatedBy:    &reviewerID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.New(apperr.InvalidState, "This approval request has already been reviewed.")
			}
			return err
		}
		failed, err := q.UpdatePaymentStatus(ctx, dbgen.UpdatePaymentStatusParams{
			ID:            approval.PaymentID,
			Status:        domain.PaymentFailed,
			RecordedBy:    nil,
			GatewayStatus: &gatewayStatus,
		})
		if err != nil {
			return err
		}
		payment := domain.MapPayment(failed)
		if err := releaseCredit(ctx, domain.NewCreditRepository(q), &payment, "rejected"); err != nil {
			return err
		}
		_, err = q.UpdateSubscriptionStatus(ctx, dbgen.UpdateSubscriptionStatusParams{
			ID:                 approval.SubscriptionID,
			Status:             domain.StatusCancelled,
			CancelledAt:        timestamptz(time.Now()),
			CancellationReason: &reason,
		})
		return err
	})
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		return nil, database.MapError("domain.reject_payment", err)
	}
	if s.notices != nil {
		s.notices.PaymentRejected(ctx, PaymentNotice{TenantID: approval.TenantID, PaymentID: approval.PaymentID, SubmittedBy: approval.CreatedBy, Note: note})
	}
	return s.approvals.Get(ctx, id)
}
