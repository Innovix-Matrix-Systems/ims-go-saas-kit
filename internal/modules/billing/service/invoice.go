package service

import (
	"context"
	"fmt"
	"strings"

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
	invoice.Rows = invoiceRows(invoice, format)
	if invoice.RefundedAmount > 0 {
		invoice.Refunded = money.Of(invoice.RefundedAmount, format)
	}
	return invoice, nil
}

// invoiceRows turns the frozen lines into printed rows; a payment without
// lines (made before plan changes were priced) prints one plan row. The plan
// row shows the subscription's real period (it starts when paid).
func invoiceRows(invoice *domain.Invoice, format money.Currency) []domain.InvoiceRow {
	planName := func(name, cycle string) string {
		description := "Subscription"
		if name != "" {
			description = name + " plan"
		}
		if cycle != "" {
			description += " (" + strings.ReplaceAll(cycle, "_", " ") + ")"
		}
		return description
	}
	if len(invoice.Lines) == 0 {
		return []domain.InvoiceRow{{
			Description: planName(invoice.PackageName, invoice.BillingCycle),
			PeriodStart: invoice.PeriodStart, PeriodEnd: invoice.PeriodEnd, Money: invoice.Money,
		}}
	}
	rows := make([]domain.InvoiceRow, 0, len(invoice.Lines))
	for _, line := range invoice.Lines {
		row := domain.InvoiceRow{Money: money.Of(line.Amount, format)}
		switch line.Type {
		case domain.LinePlan:
			row.Description = planName(line.PackageName, line.BillingCycle)
			row.PeriodStart, row.PeriodEnd = line.PeriodStart, line.PeriodEnd
			if invoice.PeriodStart != nil && invoice.PeriodEnd != nil {
				row.PeriodStart, row.PeriodEnd = invoice.PeriodStart, invoice.PeriodEnd
			}
		case domain.LineUnusedTime:
			row.Description = fmt.Sprintf("Unused time on %s (%d days)", planName(line.PackageName, ""), line.Days)
		case domain.LineAccountCredit:
			row.Description = "Account credit"
		default:
			row.Description = line.Type
		}
		rows = append(rows, row)
	}
	return rows
}
