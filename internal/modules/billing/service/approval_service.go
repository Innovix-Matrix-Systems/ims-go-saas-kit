package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// ApprovalService implements the manual payment review workflow.
type ApprovalService struct {
	approvals     domain.ApprovalRepository
	payments      domain.PaymentRepository
	subscriptions domain.SubscriptionRepository
	tx            *database.TxManager
}

// NewApprovalService creates the approval service.
func NewApprovalService(approvals domain.ApprovalRepository, payments domain.PaymentRepository, subscriptions domain.SubscriptionRepository, tx *database.TxManager) *ApprovalService {
	return &ApprovalService{approvals: approvals, payments: payments, subscriptions: subscriptions, tx: tx}
}

// List returns approval requests with pagination.
func (s *ApprovalService) List(ctx context.Context, status string, limit, offset int32) ([]domain.Approval, int64, error) {
	return s.approvals.List(ctx, status, limit, offset)
}

// Approve activates the subscription and completes the payment atomically.
func (s *ApprovalService) Approve(ctx context.Context, id, reviewerID int64, note *string) (*domain.Approval, error) {
	approval, err := s.approvals.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if approval.Status != domain.ApprovalPending {
		return nil, apperr.New(apperr.InvalidState, "This approval request has already been reviewed.")
	}

	gatewayStatus := "approved by staff"
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
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
		if _, err := q.UpdatePaymentStatus(ctx, dbgen.UpdatePaymentStatusParams{
			ID:            approval.PaymentID,
			Status:        domain.PaymentCompleted,
			RecordedBy:    &reviewerID,
			GatewayStatus: &gatewayStatus,
		}); err != nil {
			return err
		}
		subscription, err := q.GetSubscription(ctx, approval.SubscriptionID)
		if err != nil {
			return err
		}
		if err := q.DeactivateActiveSubscriptions(ctx, subscription.TenantID); err != nil {
			return err
		}
		expires := subscription.ExpiresAt
		if !expires.Valid {
			expires = timestamptz(subscription.CreatedAt.Time.AddDate(0, 1, 0))
		}
		_, err = q.ActivateSubscription(ctx, dbgen.ActivateSubscriptionParams{
			ID:        approval.SubscriptionID,
			ExpiresAt: expires,
		})
		return err
	})
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		return nil, database.MapError("domain.approve_payment", err)
	}
	return s.approvals.Get(ctx, id)
}

// Reject cancels the subscription and fails the payment atomically.
func (s *ApprovalService) Reject(ctx context.Context, id, reviewerID int64, note *string) (*domain.Approval, error) {
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
		if _, err := q.UpdatePaymentStatus(ctx, dbgen.UpdatePaymentStatusParams{
			ID:            approval.PaymentID,
			Status:        domain.PaymentFailed,
			RecordedBy:    nil,
			GatewayStatus: &gatewayStatus,
		}); err != nil {
			return err
		}
		_, err := q.UpdateSubscriptionStatus(ctx, dbgen.UpdateSubscriptionStatusParams{
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
	return s.approvals.Get(ctx, id)
}
