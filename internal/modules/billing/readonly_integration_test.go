package billing_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// A plan with a yearly price is bought yearly through the API: the payment is
// the yearly price and the subscription remembers the cycle
// (docs/guides/billing.md#monthly-and-yearly).
func TestSubscribeYearlyThroughTheAPI(t *testing.T) {
	app := testkit.New(t)
	staff := app.As(app.SuperAdmin(t))
	admin := app.As(app.User(t))

	var pkg struct {
		ID          int64  `json:"id"`
		YearlyPrice *int64 `json:"yearly_price"`
	}
	staff.Post("/api/v1/packages", map[string]any{
		"name": "Team", "price": 50000, "billing_cycle": "monthly", "yearly_price": 500000, "is_active": true,
		"features": []map[string]any{{"feature_key": "users.max", "is_enabled": true, "limit_value": 100}},
	}).Status(t, http.StatusCreated).Data(t, &pkg)
	if pkg.YearlyPrice == nil || *pkg.YearlyPrice != 500000 {
		t.Fatalf("yearly_price = %v, want 500000", pkg.YearlyPrice)
	}
	staff.Post("/api/v1/packages", map[string]any{
		"name": "Yearly only", "price": 100, "billing_cycle": "yearly", "yearly_price": 90, "is_active": true,
	}).Status(t, http.StatusUnprocessableEntity)

	var created struct {
		Subscription struct {
			BillingCycle *string `json:"billing_cycle"`
		} `json:"subscription"`
		Payment struct {
			Amount int64 `json:"amount"`
		} `json:"payment"`
	}
	admin.Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pkg.ID, "billing_cycle": "yearly", "payment_method": "bank_transfer",
		"payment_data": map[string]any{"reference_number": "YEAR-1"},
	}).Status(t, http.StatusCreated).Data(t, &created)
	if created.Payment.Amount != 500000 || created.Subscription.BillingCycle == nil || *created.Subscription.BillingCycle != "yearly" {
		t.Fatalf("yearly checkout = amount %d, cycle %v; want 500000, yearly", created.Payment.Amount, created.Subscription.BillingCycle)
	}
}

// lapse moves the tenant's plan to have expired days ago.
func lapse(t *testing.T, app *testkit.App, tenantID int64, days int) {
	t.Helper()
	if _, err := app.Pool.Exec(context.Background(),
		`UPDATE subscriptions SET status = 'expired', expires_at = now() - make_interval(days => $2) WHERE tenant_id = $1`,
		tenantID, days); err != nil {
		t.Fatal(err)
	}
}

// Grace then read-only (docs/guides/billing.md#grace-and-read-only): during
// the grace period everything works; after it, writes are refused with 402
// read_only except the allowlist, reads always work and staff are not
// blocked.
func TestReadOnlyAfterTheGracePeriod(t *testing.T) {
	app := testkit.New(t)
	admin := app.As(app.User(t))

	// Three days after the lapse: still in grace, writes work.
	lapse(t, app, testkit.DefaultTenant, 3)
	var status struct {
		InGrace     bool       `json:"in_grace"`
		GraceEndsAt *time.Time `json:"grace_ends_at"`
		ReadOnly    bool       `json:"read_only"`
	}
	admin.Get("/api/v1/billing/subscription/status").Status(t, http.StatusOK).Data(t, &status)
	if !status.InGrace || status.ReadOnly || status.GraceEndsAt == nil {
		t.Fatalf("in grace: status = %+v", status)
	}
	admin.Post("/api/v1/role", map[string]any{"name": "Support"}).Status(t, http.StatusCreated)

	// Twenty days after the lapse: read-only.
	lapse(t, app, testkit.DefaultTenant, 20)
	admin.Get("/api/v1/billing/subscription/status").Status(t, http.StatusOK).Data(t, &status)
	if !status.ReadOnly || status.InGrace {
		t.Fatalf("after grace: status = %+v", status)
	}
	var refused struct {
		ErrorCode string         `json:"error_code"`
		Meta      map[string]any `json:"meta"`
	}
	admin.Post("/api/v1/role", map[string]any{"name": "Auditors"}).Status(t, http.StatusPaymentRequired).JSON(t, &refused)
	if refused.ErrorCode != "SUBSCRIPTION_REQUIRED" || refused.Meta["reason"] != "read_only" {
		t.Fatalf("refusal = %+v, want SUBSCRIPTION_REQUIRED with reason read_only", refused)
	}

	// Reads, the allowlist and staff still pass.
	admin.Get("/api/v1/role").Status(t, http.StatusOK)
	if code := admin.Post("/api/v1/notification/read-all", nil).Code; code == http.StatusPaymentRequired {
		t.Fatal("marking notifications read is refused while read-only")
	}
	if code := admin.Get("/api/v1/billing/change-preview?package_id=1").Code; code == http.StatusPaymentRequired {
		t.Fatal("billing is refused while read-only")
	}
	staff := app.As(app.SuperAdmin(t)).InTenant(testkit.DefaultTenant, app.DefaultBranch(t, testkit.DefaultTenant))
	staff.Post("/api/v1/role", map[string]any{"name": "Staff made"}).Status(t, http.StatusCreated)
}
