package billing_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type feedItem struct {
	Title string         `json:"title"`
	Data  map[string]any `json:"data"`
}

func feedTitles(t *testing.T, app *testkit.App, user *testkit.User, path string) []string {
	t.Helper()
	var items []feedItem
	app.As(user).Get(path).Status(t, http.StatusOK).Data(t, &items)
	titles := make([]string, 0, len(items))
	for _, item := range items {
		titles = append(titles, item.Title)
	}
	return titles
}

func has(titles []string, want string) bool {
	for _, title := range titles {
		if title == want {
			return true
		}
	}
	return false
}

// A bank-transfer checkout tells the staff who can approve it and the
// organization's billing managers; approving it tells the organization the
// payment was received and mails a receipt with the invoice PDF. Nobody
// else hears about it.
func TestPaymentNoticesReachApproversAndBillingManagers(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)
	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer Notices", "billing.view")))
	outsider := app.User(t, testkit.InTenant(app.Tenant(t, "Elsewhere Notices")))
	pro := createPackage(t, app, "Pro Notices", 150000)

	var created struct {
		Payment payment `json:"payment"`
	}
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pro, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "TRX-42"},
	}).Status(t, http.StatusCreated).Data(t, &created)

	if titles := feedTitles(t, app, staff, "/api/v1/internal/notification/feed"); !has(titles, "Payment to review: "+tenantName(t, app)) {
		t.Fatalf("staff feed = %q, want the payment to review", titles)
	}
	if titles := feedTitles(t, app, admin, "/api/v1/notification/feed"); !has(titles, "Payment submitted for confirmation") {
		t.Fatalf("billing manager feed = %q, want the submitted notice", titles)
	}
	if titles := feedTitles(t, app, viewer, "/api/v1/notification/feed"); has(titles, "Payment submitted for confirmation") {
		t.Fatalf("a member without billing.manage got the notice: %q", titles)
	}
	if titles := feedTitles(t, app, outsider, "/api/v1/notification/feed"); len(titles) != 0 {
		t.Fatalf("another organization got %q", titles)
	}

	// The invoice is also a PDF.
	pdf := app.As(admin).Get(fmt.Sprintf("/api/v1/billing/payments/%d/invoice.pdf", created.Payment.ID)).Status(t, http.StatusOK)
	if got := pdf.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("invoice.pdf Content-Type = %q", got)
	}
	if !bytes.HasPrefix(pdf.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("invoice.pdf is not a PDF")
	}
	app.As(outsider).Get(fmt.Sprintf("/api/v1/billing/payments/%d/invoice.pdf", created.Payment.ID)).Status(t, http.StatusNotFound)

	var approvals []struct {
		ID int64 `json:"id"`
	}
	app.As(staff).Get("/api/v1/billing/internal/payment-approvals?status=pending").Status(t, http.StatusOK).Data(t, &approvals)
	if len(approvals) != 1 {
		t.Fatalf("pending approvals = %d, want 1", len(approvals))
	}
	app.As(staff).Post(fmt.Sprintf("/api/v1/billing/internal/payment-approvals/%d/approve", approvals[0].ID), map[string]any{}).
		Status(t, http.StatusOK)

	if titles := feedTitles(t, app, admin, "/api/v1/notification/feed"); !has(titles, "Payment received") {
		t.Fatalf("billing manager feed after approval = %q, want payment received", titles)
	}
	receipt := app.Queued(t, "payment-receipt")
	if receipt["invoice"] != created.Payment.InvoiceNumber || receipt["amount"] != "BDT 1,500.00" {
		t.Fatalf("receipt data = %v", receipt)
	}
	var attached int
	if err := app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job
		WHERE args->>'kind' = 'delivery.send'
		  AND args->'payload'->'message'->>'template' = 'payment-receipt'
		  AND args->'payload'->'message'->'attachments'->0->>'content_type' = 'application/pdf'`).Scan(&attached); err != nil || attached == 0 {
		t.Fatalf("receipt mails with the PDF attached = %d (%v), want at least 1", attached, err)
	}
}

func tenantName(t *testing.T, app *testkit.App) string {
	t.Helper()
	var name string
	if err := app.Pool.QueryRow(context.Background(), `SELECT name FROM tenants WHERE id = $1`, testkit.DefaultTenant).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}
