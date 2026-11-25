package billing_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

type payment struct {
	ID            int64  `json:"id"`
	Status        string `json:"status"`
	InvoiceNumber string `json:"invoice_number"`
}

func createPackage(t *testing.T, app *testkit.App, name string, price int) int64 {
	t.Helper()
	var pkg struct {
		ID int64 `json:"id"`
	}
	app.As(app.SuperAdmin(t)).Post("/api/v1/packages", map[string]any{
		"name": name, "price": price, "billing_cycle": "monthly", "is_active": true,
		"features": []map[string]any{{"feature_key": "users.max", "is_enabled": true, "limit_value": -1}},
	}).Status(t, http.StatusCreated).Data(t, &pkg)
	return pkg.ID
}

// An organization withdraws a bank-transfer checkout nobody confirmed yet:
// the payment is cancelled and staff no longer see it waiting.
func TestTenantWithdrawsPendingCheckout(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)
	pro := createPackage(t, app, "Pro Withdraw", 30000)

	var created struct {
		Payment payment `json:"payment"`
	}
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pro, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "WRONG-1"},
	}).Status(t, http.StatusCreated).Data(t, &created)

	member := app.User(t, testkit.Role(app.Role(t, "Viewer", "billing.view")))
	app.As(member).Post(fmt.Sprintf("/api/v1/billing/payments/%d/cancel", created.Payment.ID), nil).Status(t, http.StatusForbidden)

	other := app.User(t, testkit.InTenant(app.Tenant(t, "Elsewhere")))
	app.As(other).Post(fmt.Sprintf("/api/v1/billing/payments/%d/cancel", created.Payment.ID), nil).Status(t, http.StatusNotFound)

	app.As(admin).Post(fmt.Sprintf("/api/v1/billing/payments/%d/cancel", created.Payment.ID), nil).Status(t, http.StatusNoContent)
	app.As(admin).Post(fmt.Sprintf("/api/v1/billing/payments/%d/cancel", created.Payment.ID), nil).Status(t, http.StatusConflict)
	var logged int
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM activity_log WHERE action = 'billing.payment_withdrawn' AND subject_id = $1 AND actor_id = $2`,
		created.Payment.ID, admin.ID).Scan(&logged); err != nil || logged != 1 {
		t.Errorf("billing.payment_withdrawn entries = %d (%v), want 1", logged, err)
	}

	var payments []payment
	app.As(admin).Get("/api/v1/billing/payments").Status(t, http.StatusOK).Data(t, &payments)
	if len(payments) != 1 || payments[0].Status != "cancelled" {
		t.Fatalf("payments = %+v, want one cancelled", payments)
	}
	var pending []struct{}
	app.As(staff).Get("/api/v1/billing/internal/payment-approvals?status=pending").Status(t, http.StatusOK).Data(t, &pending)
	if len(pending) != 0 {
		t.Fatalf("%d approvals still pending after the withdrawal", len(pending))
	}
}

// A free plan starts at once: no payment, nothing to approve.
func TestTenantMovesToFreePlan(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	free := createPackage(t, app, "Free Move", 0)

	var created struct {
		Subscription struct {
			PackageID int64  `json:"package_id"`
			Status    string `json:"status"`
		} `json:"subscription"`
		Payment *payment `json:"payment"`
	}
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{"package_id": free}).Status(t, http.StatusCreated).Data(t, &created)
	if created.Subscription.Status != "active" || created.Subscription.PackageID != free || created.Payment != nil {
		t.Fatalf("free subscribe = %+v, want an active subscription and no payment", created)
	}
	var status struct {
		Package struct {
			ID int64 `json:"id"`
		} `json:"package"`
	}
	app.As(admin).Get("/api/v1/billing/subscription/status").Status(t, http.StatusOK).Data(t, &status)
	if status.Package.ID != free {
		t.Fatalf("current package = %d, want the free one %d", status.Package.ID, free)
	}
	// A paid plan still needs a payment method.
	pro := createPackage(t, app, "Pro Needs Method", 30000)
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{"package_id": pro}).Status(t, http.StatusUnprocessableEntity)
}

// The invoice is an HTML document for the organization's own payment only.
func TestTenantInvoice(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	pro := createPackage(t, app, "Pro Invoice", 45050)
	var created struct {
		Payment payment `json:"payment"`
	}
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pro, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "INV-REF"},
	}).Status(t, http.StatusCreated).Data(t, &created)

	path := fmt.Sprintf("/api/v1/billing/payments/%d/invoice", created.Payment.ID)
	res := app.As(admin).Get(path).Status(t, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(res.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, created.Payment.InvoiceNumber) ||
		!strings.Contains(body, "Pro Invoice") || !strings.Contains(body, "৳450.50") || !strings.Contains(body, "Not paid") {
		t.Fatalf("invoice is missing its details:\n%s", body)
	}
	other := app.User(t, testkit.InTenant(app.Tenant(t, "Not Theirs")))
	app.As(other).Get(path).Status(t, http.StatusNotFound)
}

// Package editors read the feature catalog instead of hard-coding it.
func TestFeatureCatalog(t *testing.T) {
	app := testkit.New(t)
	var features []struct {
		Key     string `json:"key"`
		IsLimit bool   `json:"is_limit"`
	}
	app.As(app.SuperAdmin(t)).Get("/api/v1/packages/features").Status(t, http.StatusOK).Data(t, &features)
	limits := map[string]bool{}
	for _, feature := range features {
		limits[feature.Key] = feature.IsLimit
	}
	if !limits["users.max"] || limits["api.access"] || len(features) < 10 {
		t.Fatalf("catalog = %+v", features)
	}
	app.As(app.User(t)).Get("/api/v1/packages/features").Status(t, http.StatusForbidden)
}
