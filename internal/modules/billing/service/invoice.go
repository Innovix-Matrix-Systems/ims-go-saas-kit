package service

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/money"
)

// InvoiceService builds invoices for a tenant's own payments.
type InvoiceService struct {
	reports    domain.ReportRepository
	issuer     func(ctx context.Context) string
	currencies domain.CurrencyFormats
}

// WithCurrencies writes amounts the way their currency says; without it
// every currency gets money.Fallback ("BDT 1,500.00").
func (s *InvoiceService) WithCurrencies(currencies domain.CurrencyFormats) *InvoiceService {
	s.currencies = currencies
	return s
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
	format := money.Fallback(invoice.Currency)
	if s.currencies != nil {
		format = s.currencies.Format(ctx, invoice.Currency)
	}
	invoice.Money = money.Of(invoice.Amount, format)
	return invoice, nil
}
