package billing

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// WithInvoices serves GET /billing/payments/{id}/invoice.
func (h *Handler) WithInvoices(invoices *service.InvoiceService) *Handler {
	h.invoices = invoices
	return h
}

// invoiceCSP lets the document style itself and nothing else.
const invoiceCSP = "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'"

// invoice answers a printable HTML invoice for one of the organization's
// payments, as a download. An unpaid one says so plainly: it becomes a
// receipt once the payment is completed.
func (h *Handler) invoice(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	invoice, err := h.invoices.For(r.Context(), tenancy.From(r).EffectiveTenantID(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", invoiceCSP)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="invoice-%s.html"`, invoice.Number))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = invoiceTemplate.Execute(w, invoice)
}

// invoicePDF answers the same invoice as a PDF download.
func (h *Handler) invoicePDF(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	invoice, data, err := h.invoices.PDF(r.Context(), tenancy.From(r).EffectiveTenantID(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="invoice-%s.pdf"`, invoice.Number))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func invoiceMoney(minor int64, currency string) string {
	symbol := currency + " "
	if strings.EqualFold(currency, "BDT") {
		symbol = "৳"
	}
	return fmt.Sprintf("%s%d.%02d", symbol, minor/100, minor%100)
}

func invoiceDate(at *time.Time) string {
	if at == nil {
		return "—"
	}
	return at.Format("2 January 2006")
}

var invoiceTemplate = template.Must(template.New("invoice").Funcs(template.FuncMap{
	"money": invoiceMoney,
	"date":  invoiceDate,
	"day":   func(at time.Time) string { return at.Format("2 January 2006") },
	"words": func(value string) string { return strings.ReplaceAll(value, "_", " ") },
	"paid":  func(status string) bool { return status == "completed" },
}).Parse(invoiceHTML))

const invoiceHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Invoice {{.Number}}</title>
<style>
  body { font: 14px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; color: #1c1c1f; margin: 0; background: #f4f4f5; }
  .page { max-width: 760px; margin: 32px auto; background: #fff; padding: 40px; border: 1px solid #e4e4e7; border-radius: 10px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 24px; border-bottom: 1px solid #e4e4e7; padding-bottom: 20px; }
  h1 { font-size: 22px; margin: 0 0 4px; }
  .muted { color: #6b6b73; }
  .status { display: inline-block; margin-top: 6px; padding: 2px 10px; border-radius: 999px; font-size: 12px; font-weight: 600; }
  .status.paid { background: #e7f6ec; color: #136c34; }
  .status.open { background: #fdf3e2; color: #8a5300; }
  .parties { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin: 24px 0; }
  .label { font-size: 12px; letter-spacing: .04em; color: #6b6b73; margin-bottom: 4px; }
  table { width: 100%; border-collapse: collapse; margin-top: 8px; }
  th, td { text-align: left; padding: 10px 0; border-bottom: 1px solid #e4e4e7; }
  th { font-size: 12px; letter-spacing: .04em; color: #6b6b73; font-weight: 600; }
  .right { text-align: right; }
  .total td { font-weight: 700; font-size: 16px; border-bottom: 0; }
  footer { margin-top: 28px; font-size: 12px; }
  @media print { body { background: #fff; } .page { margin: 0; border: 0; padding: 0; } }
</style>
</head>
<body>
<div class="page">
  <header>
    <div>
      <h1>Invoice</h1>
      <div class="muted">{{.Number}}</div>
    </div>
    <div class="right">
      <div><strong>{{if .Issuer}}{{.Issuer}}{{else}}Invoice{{end}}</strong></div>
      <div class="muted">Issued {{day .IssuedAt}}</div>
      {{if paid .Status}}<span class="status paid">Paid {{date .PaidAt}}</span>{{else}}<span class="status open">Not paid ({{words .Status}})</span>{{end}}
    </div>
  </header>
  <div class="parties">
    <div>
      <div class="label">BILLED TO</div>
      <div><strong>{{.TenantName}}</strong></div>
      {{if .TenantEmail}}<div>{{.TenantEmail}}</div>{{end}}
      {{if .TenantPhone}}<div>{{.TenantPhone}}</div>{{end}}
    </div>
    <div>
      <div class="label">PAYMENT</div>
      <div>{{words .PaymentMethod}}</div>
      {{if .Reference}}<div class="muted">Reference {{.Reference}}</div>{{end}}
    </div>
  </div>
  <table>
    <thead><tr><th>DESCRIPTION</th><th>PERIOD</th><th class="right">AMOUNT</th></tr></thead>
    <tbody>
      <tr>
        <td>{{if .PackageName}}{{.PackageName}} plan{{else}}Subscription{{end}}{{if .BillingCycle}} <span class="muted">({{words .BillingCycle}})</span>{{end}}</td>
        <td>{{date .PeriodStart}} – {{date .PeriodEnd}}</td>
        <td class="right">{{money .Amount .Currency}}</td>
      </tr>
      <tr class="total"><td colspan="2">Total</td><td class="right">{{money .Amount .Currency}}</td></tr>
    </tbody>
  </table>
  <footer class="muted">{{if paid .Status}}Thank you for your payment.{{else}}This invoice is not paid yet. It becomes a receipt once the payment is confirmed.{{end}}</footer>
</div>
</body>
</html>
`
