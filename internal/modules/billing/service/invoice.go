package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Invoice is everything an invoice document shows for one payment.
type Invoice struct {
	Number        string
	Status        string
	PaymentMethod string
	Amount        int64
	Currency      string
	Reference     string
	IssuedAt      time.Time
	PaidAt        *time.Time
	Issuer        string
	TenantName    string
	TenantEmail   string
	TenantPhone   string
	PackageName   string
	BillingCycle  string
	PeriodStart   *time.Time
	PeriodEnd     *time.Time
}

// InvoiceService builds invoices for a tenant's own payments.
type InvoiceService struct {
	queries *dbgen.Queries
	issuer  func(ctx context.Context) string
}

// NewInvoiceService creates the invoice builder; issuer names who issues the
// invoice (the platform's name).
func NewInvoiceService(queries *dbgen.Queries, issuer func(ctx context.Context) string) *InvoiceService {
	return &InvoiceService{queries: queries, issuer: issuer}
}

// For returns the invoice of one of the tenant's payments.
func (s *InvoiceService) For(ctx context.Context, tenantID, paymentID int64) (*Invoice, error) {
	row, err := s.queries.InvoiceForPayment(ctx, dbgen.InvoiceForPaymentParams{PaymentID: paymentID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Payment with ID '%d' was not found.", paymentID)
	}
	if err != nil {
		return nil, database.MapError("billing.invoice", err)
	}
	invoice := &Invoice{
		Number: row.InvoiceNumber, Status: row.Status, PaymentMethod: row.PaymentMethod, Amount: row.Amount,
		Currency: row.Currency, Reference: row.GatewayRef, IssuedAt: row.CreatedAt.Time, TenantName: row.TenantName,
		TenantEmail: row.TenantEmail, TenantPhone: row.TenantPhone, PackageName: row.PackageName, BillingCycle: row.BillingCycle,
	}
	if row.PaidAt.Valid {
		invoice.PaidAt = &row.PaidAt.Time
	}
	if row.StartsAt.Valid {
		invoice.PeriodStart = &row.StartsAt.Time
	}
	if row.ExpiresAt.Valid {
		invoice.PeriodEnd = &row.ExpiresAt.Time
	}
	if s.issuer != nil {
		invoice.Issuer = s.issuer(ctx)
	}
	return invoice, nil
}
