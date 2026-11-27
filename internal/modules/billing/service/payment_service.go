package service

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// PaymentService manages payment records.
type PaymentService struct {
	payments   domain.PaymentRepository
	currencies domain.CurrencySource
	now        func() time.Time
}

// NewPaymentService creates the payment service; payments are recorded in
// the currencies' default.
func NewPaymentService(payments domain.PaymentRepository, currencies domain.CurrencySource) *PaymentService {
	return &PaymentService{payments: payments, currencies: currencies, now: time.Now}
}

// ListForTenant returns the tenant's payments.
func (s *PaymentService) ListForTenant(ctx context.Context, tenantID int64, limit, offset int32) ([]domain.Payment, int64, error) {
	return s.payments.ListForTenant(ctx, tenantID, limit, offset)
}

// List returns every organization's payments (admin surface), narrowed by
// filter (status, one organization).
func (s *PaymentService) List(ctx context.Context, filter domain.PaymentFilter) ([]domain.Payment, int64, error) {
	return s.payments.List(ctx, filter)
}

// GetForTenant returns one payment scoped to the tenant.
func (s *PaymentService) GetForTenant(ctx context.Context, tenantID, id int64) (*domain.Payment, error) {
	payment, err := s.payments.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if payment.TenantID != tenantID {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Payment with ID '%d' was not found.", id)
	}
	return payment, nil
}

// FindByGatewayRef resolves a gateway payment reference.
func (s *PaymentService) FindByGatewayRef(ctx context.Context, gatewayRef string) (*domain.Payment, error) {
	return s.payments.GetByGatewayRef(ctx, gatewayRef)
}

// RecordManual registers a completed manual payment (cash/bank transfer).
// Callers are responsible for activating the subscription.
func (s *PaymentService) RecordManual(ctx context.Context, tenantID, subscriptionID int64, method string, amount int64, notes string, actorID int64) (_ *domain.Payment, err error) {
	ctx, end := tracing.Begin(ctx, "billing.record_manual_payment",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("subscription.id", subscriptionID),
		attribute.String("payment.method", method),
	)
	defer end(&err)
	if !domain.IsManualMethod(method) {
		return nil, apperr.Validation(map[string][]string{
			"payment_method": {"Only cash and bank transfer payments can be recorded manually."},
		})
	}
	if amount <= 0 {
		return nil, apperr.Validation(map[string][]string{
			"amount": {"The amount must be greater than zero."},
		})
	}

	currency, err := s.currencies.DefaultCode(ctx)
	if err != nil {
		return nil, err
	}
	invoice, err := domain.GenerateInvoiceNumber(s.now())
	if err != nil {
		return nil, err
	}
	payment, err := s.payments.Create(ctx, domain.PaymentInput{
		TenantID:       tenantID,
		SubscriptionID: &subscriptionID,
		PaymentMethod:  method,
		Status:         domain.PaymentCompleted,
		Amount:         amount,
		Currency:       currency,
		InvoiceNumber:  invoice,
		CreatedBy:      &actorID,
	})
	if err != nil {
		return nil, err
	}
	_ = notes
	return s.payments.UpdateStatus(ctx, payment.ID, domain.PaymentCompleted, &actorID, strPtr("manual"))
}
