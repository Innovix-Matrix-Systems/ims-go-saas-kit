package billing_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

type changeQuote struct {
	Kind            string `json:"kind"`
	Effective       string `json:"effective"`
	AmountDue       int64  `json:"amount_due"`
	Price           int64  `json:"price"`
	RequiresPayment bool   `json:"requires_payment"`
	Lines           []struct {
		Type   string `json:"type"`
		Amount int64  `json:"amount"`
	} `json:"lines"`
}

// The checkout modal asks for the price before paying; only billing
// managers can, and the quote matches what the checkout charges.
func TestChangePreviewAndCheckout(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	pro := createPackage(t, app, "Pro Preview", 30000)
	other := createPackage(t, app, "Team Preview", 50000)

	var quote changeQuote
	app.As(admin).Get(fmt.Sprintf("/api/v1/billing/change-preview?package_id=%d", pro)).Status(t, http.StatusOK).Data(t, &quote)
	// The default organization is on a trial: a paid plan starts now, in full.
	if quote.Kind != "new" || quote.Effective != "now" || quote.AmountDue != 30000 || !quote.RequiresPayment || len(quote.Lines) != 1 {
		t.Fatalf("quote = %+v", quote)
	}
	app.As(admin).Get("/api/v1/billing/change-preview").Status(t, http.StatusUnprocessableEntity)
	viewer := app.User(t, testkit.Role(app.Role(t, "Billing viewer", "billing.view")))
	app.As(viewer).Get(fmt.Sprintf("/api/v1/billing/change-preview?package_id=%d", pro)).Status(t, http.StatusForbidden)

	var created struct {
		Payment payment     `json:"payment"`
		Change  changeQuote `json:"change"`
	}
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pro, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "TX-9"},
	}).Status(t, http.StatusCreated).Data(t, &created)
	if created.Change.AmountDue != 30000 || created.Payment.Status != "pending" {
		t.Fatalf("checkout = %+v", created)
	}

	// A second checkout for another plan waits for the first.
	var problem struct {
		ErrorCode string         `json:"error_code"`
		Meta      map[string]any `json:"meta"`
	}
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{"package_id": other, "payment_method": "cash"}).
		Status(t, http.StatusConflict).JSON(t, &problem)
	if problem.ErrorCode != "CONFLICT" || problem.Meta["reason"] != "CHECKOUT_PENDING" {
		t.Fatalf("problem = %+v", problem)
	}
}

// Account credit belongs to one organization: staff add it, the
// organization sees it, nobody else does.
func TestAccountCreditIsPerOrganization(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)
	outsider := app.User(t, testkit.InTenant(app.Tenant(t, "Other Org")))

	path := fmt.Sprintf("/api/v1/billing/internal/tenants/%d/credits", admin.TenantID)
	app.As(admin).Post(path, map[string]any{"amount": 500, "reason": "Self-service"}).Status(t, http.StatusForbidden)
	app.As(staff).Post(path, map[string]any{"amount": 500, "reason": "Goodwill"}).Status(t, http.StatusCreated)
	app.As(staff).Post(path, map[string]any{"amount": -900, "reason": "Too much"}).Status(t, http.StatusUnprocessableEntity)

	var credits struct {
		Balance int64 `json:"balance"`
		Entries []struct {
			Amount int64  `json:"amount"`
			Reason string `json:"reason"`
		} `json:"entries"`
	}
	app.As(admin).Get("/api/v1/billing/credits").Status(t, http.StatusOK).Data(t, &credits)
	if credits.Balance != 500 || len(credits.Entries) != 1 || credits.Entries[0].Reason != "Goodwill" {
		t.Fatalf("own credits = %+v", credits)
	}
	app.As(outsider).Get("/api/v1/billing/credits").Status(t, http.StatusOK).Data(t, &credits)
	if credits.Balance != 0 || len(credits.Entries) != 0 {
		t.Fatalf("another organization sees %+v", credits)
	}
	var status struct {
		CreditBalance int64 `json:"credit_balance"`
	}
	app.As(admin).Get("/api/v1/billing/subscription/status").Status(t, http.StatusOK).Data(t, &status)
	if status.CreditBalance != 500 {
		t.Fatalf("status credit_balance = %d, want 500", status.CreditBalance)
	}
	app.As(staff).Get(path).Status(t, http.StatusOK).Data(t, &credits)
	if credits.Balance != 500 {
		t.Fatalf("staff view balance = %d", credits.Balance)
	}
}

// Refunds are a staff action; the organization cannot refund itself.
func TestStaffRefundOverHTTP(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)
	pro := createPackage(t, app, "Pro Refund", 30000)

	var assigned struct {
		Payment payment `json:"payment"`
	}
	app.As(staff).Post(fmt.Sprintf("/api/v1/billing/internal/tenants/%d/subscription", admin.TenantID), map[string]any{
		"package_id": pro, "payment_method": "cash",
	}).Status(t, http.StatusCreated).Data(t, &assigned)

	path := fmt.Sprintf("/api/v1/billing/internal/payments/%d/refund", assigned.Payment.ID)
	body := map[string]any{"amount": 10000, "reason": "Partial goodwill", "to": "credit"}
	app.As(admin).Post(path, body).Status(t, http.StatusForbidden)
	var refunded struct {
		RefundedAmount int64  `json:"refunded_amount"`
		Status         string `json:"status"`
	}
	app.As(staff).Post(path, body).Status(t, http.StatusOK).Data(t, &refunded)
	if refunded.RefundedAmount != 10000 || refunded.Status != "completed" {
		t.Fatalf("refund = %+v", refunded)
	}
	app.As(staff).Post(path, map[string]any{"amount": 90000, "reason": "Too much", "to": "original"}).Status(t, http.StatusUnprocessableEntity)
	app.As(staff).Post(path, map[string]any{"amount": 1, "reason": "Bad target", "to": "wallet"}).Status(t, http.StatusUnprocessableEntity)
}

// An organization stops its renewal and changes its mind.
func TestCancelAtPeriodEndAndKeep(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	viewer := app.User(t, testkit.Role(app.Role(t, "Billing viewer", "billing.view")))

	app.As(viewer).Post("/api/v1/billing/subscription/cancel", nil).Status(t, http.StatusForbidden)
	var subscription struct {
		CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
		Status            string `json:"status"`
	}
	app.As(admin).Post("/api/v1/billing/subscription/cancel", nil).Status(t, http.StatusOK).Data(t, &subscription)
	if !subscription.CancelAtPeriodEnd || subscription.Status == "cancelled" {
		t.Fatalf("after cancel = %+v, want the plan kept until its end", subscription)
	}
	app.As(admin).Post("/api/v1/billing/subscription/keep", nil).Status(t, http.StatusOK).Data(t, &subscription)
	if subscription.CancelAtPeriodEnd {
		t.Fatal("keep did not undo the cancellation")
	}
}
