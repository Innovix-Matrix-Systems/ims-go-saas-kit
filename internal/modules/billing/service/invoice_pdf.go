package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// PDF returns the invoice of one of the tenant's payments as a PDF, the same
// content as the HTML invoice.
func (s *InvoiceService) PDF(ctx context.Context, tenantID, paymentID int64) (*domain.Invoice, []byte, error) {
	invoice, err := s.For(ctx, tenantID, paymentID)
	if err != nil {
		return nil, nil, err
	}
	data, err := RenderInvoicePDF(invoice)
	if err != nil {
		return nil, nil, err
	}
	return invoice, data, nil
}

// RenderInvoicePDF draws an invoice on one A4 page with the standard PDF
// fonts (no font files to ship). Those fonts cover Western European text;
// other scripts in names print as "?", and amounts use the currency code.
func RenderInvoicePDF(invoice *domain.Invoice) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("Invoice "+invoice.Number, true)
	pdf.SetCreator(invoice.Issuer, true)
	pdf.SetMargins(20, 20, 20)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()
	text := pdf.UnicodeTranslatorFromDescriptor("") // UTF-8 → cp1252

	const width = 170.0
	muted := func() { pdf.SetTextColor(107, 107, 115) }
	ink := func() { pdf.SetTextColor(28, 28, 31) }
	rule := func() {
		pdf.SetDrawColor(228, 228, 231)
		y := pdf.GetY()
		pdf.Line(20, y, 20+width, y)
	}
	paid := invoice.Status == domain.PaymentCompleted

	// Header: title and number left, issuer, date and status right.
	top := pdf.GetY()
	ink()
	pdf.SetFont("Helvetica", "B", 20)
	pdf.CellFormat(width/2, 9, "Invoice", "", 2, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	muted()
	pdf.CellFormat(width/2, 6, text(invoice.Number), "", 2, "L", false, 0, "")

	pdf.SetXY(20+width/2, top)
	ink()
	pdf.SetFont("Helvetica", "B", 11)
	issuer := invoice.Issuer
	if issuer == "" {
		issuer = "Invoice"
	}
	pdf.CellFormat(width/2, 6, text(issuer), "", 2, "R", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	muted()
	pdf.CellFormat(width/2, 6, "Issued "+invoiceDay(invoice.IssuedAt), "", 2, "R", false, 0, "")
	pdf.SetFont("Helvetica", "B", 10)
	if paid {
		pdf.SetTextColor(19, 108, 52)
		pdf.CellFormat(width/2, 6, "Paid "+invoiceDate(invoice.PaidAt), "", 2, "R", false, 0, "")
	} else {
		pdf.SetTextColor(138, 83, 0)
		pdf.CellFormat(width/2, 6, "Not paid ("+invoiceWords(invoice.Status)+")", "", 2, "R", false, 0, "")
	}
	pdf.SetXY(20, top+24)
	rule()
	pdf.Ln(8)

	// Parties.
	partiesTop := pdf.GetY()
	label := func(value string) {
		pdf.SetFont("Helvetica", "B", 8)
		muted()
		pdf.CellFormat(width/2, 5, value, "", 2, "L", false, 0, "")
	}
	line := func(value string, bold bool) {
		style := ""
		if bold {
			style = "B"
		}
		pdf.SetFont("Helvetica", style, 10)
		ink()
		pdf.CellFormat(width/2, 5.5, text(value), "", 2, "L", false, 0, "")
	}
	label("BILLED TO")
	line(invoice.TenantName, true)
	if invoice.TenantEmail != "" {
		line(invoice.TenantEmail, false)
	}
	if invoice.TenantPhone != "" {
		line(invoice.TenantPhone, false)
	}
	leftBottom := pdf.GetY()
	pdf.SetXY(20+width/2, partiesTop)
	pdf.SetLeftMargin(20 + width/2)
	label("PAYMENT")
	line(invoiceWords(invoice.PaymentMethod), false)
	if invoice.Reference != "" {
		line("Reference "+invoice.Reference, false)
	}
	pdf.SetLeftMargin(20)
	pdf.SetXY(20, max(leftBottom, pdf.GetY())+8)

	// Line item and total.
	columns := []float64{78, 54, 38}
	pdf.SetFont("Helvetica", "B", 8)
	muted()
	for i, heading := range []string{"DESCRIPTION", "PERIOD", "AMOUNT"} {
		align := "L"
		if i == 2 {
			align = "R"
		}
		pdf.CellFormat(columns[i], 8, heading, "B", 0, align, false, 0, "")
	}
	pdf.Ln(-1)
	description := "Subscription"
	if invoice.PackageName != "" {
		description = invoice.PackageName + " plan"
	}
	if invoice.BillingCycle != "" {
		description += " (" + invoiceWords(invoice.BillingCycle) + ")"
	}
	amount := FormatMoney(invoice.Amount, invoice.Currency)
	pdf.SetFont("Helvetica", "", 10)
	ink()
	pdf.CellFormat(columns[0], 10, text(description), "B", 0, "L", false, 0, "")
	pdf.CellFormat(columns[1], 10, shortDate(invoice.PeriodStart)+" - "+shortDate(invoice.PeriodEnd), "B", 0, "L", false, 0, "")
	pdf.CellFormat(columns[2], 10, amount, "B", 1, "R", false, 0, "")
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(columns[0]+columns[1], 11, "Total", "", 0, "L", false, 0, "")
	pdf.CellFormat(columns[2], 11, amount, "", 1, "R", false, 0, "")

	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 9)
	muted()
	footer := "This invoice is not paid yet. It becomes a receipt once the payment is confirmed."
	if paid {
		footer = "Thank you for your payment."
	}
	pdf.MultiCell(width, 5, footer, "", "L", false)

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("billing: render invoice pdf: %w", err)
	}
	return out.Bytes(), nil
}

// FormatMoney formats minor units with the currency code ("BDT 1,500.00"),
// as invoices, receipts and payment notices show amounts.
func FormatMoney(minor int64, currency string) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	whole := fmt.Sprintf("%d", minor/100)
	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return fmt.Sprintf("%s %s%s.%02d", strings.ToUpper(currency), sign, grouped.String(), minor%100)
}

func invoiceDay(at time.Time) string { return at.Format("2 January 2006") }

func invoiceDate(at *time.Time) string {
	if at == nil {
		return "-"
	}
	return invoiceDay(*at)
}

// shortDate keeps the period column narrow ("25 Nov 2026").
func shortDate(at *time.Time) string {
	if at == nil {
		return "-"
	}
	return at.Format("2 Jan 2006")
}

func invoiceWords(value string) string { return strings.ReplaceAll(value, "_", " ") }
