package service

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// InvoiceService builds invoices for a tenant's own payments.
type InvoiceService struct {
	reports domain.ReportRepository
	issuer  func(ctx context.Context) string
}

// NewInvoiceService creates the invoice builder; issuer names who issues the
// invoice (the platform's name).
func NewInvoiceService(reports domain.ReportRepository, issuer func(ctx context.Context) string) *InvoiceService {
	return &InvoiceService{reports: reports, issuer: issuer}
}

// For returns the invoice of one of the tenant's payments.
func (s *InvoiceService) For(ctx context.Context, tenantID, paymentID int64) (*domain.Invoice, error) {
	invoice, err := s.reports.Invoice(ctx, tenantID, paymentID)
	if err != nil {
		return nil, err
	}
	if s.issuer != nil {
		invoice.Issuer = s.issuer(ctx)
	}
	return invoice, nil
}
