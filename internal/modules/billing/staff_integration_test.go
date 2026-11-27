package billing_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// The staff subscriptions panel over HTTP: staff list every organization's
// subscription with names and counts; organization members cannot reach it.
func TestStaffSubscriptionPanelAPI(t *testing.T) {
	app := testkit.New(t)
	staff := app.As(app.SuperAdmin(t))
	other := app.Tenant(t, "Beta Studio")

	var rows []struct {
		Organization struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"organization"`
		Package struct {
			Name string `json:"name"`
		} `json:"package"`
		Status     string `json:"status"`
		PriceCycle string `json:"price_cycle"`
		ReadOnly   bool   `json:"read_only"`
	}
	staff.Get("/api/v1/billing/internal/subscriptions?view=current&search=beta").Status(t, http.StatusOK).Data(t, &rows)
	if len(rows) != 1 || rows[0].Organization.ID != other || rows[0].Organization.Name != "Beta Studio" || rows[0].Package.Name == "" || rows[0].PriceCycle == "" {
		t.Fatalf("current rows for search=beta = %+v, want Beta Studio with its plan", rows)
	}

	// A page past the last row still reports the real total.
	var beyond struct {
		Data []any `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	staff.Get("/api/v1/billing/internal/subscriptions?view=current&page=50").Status(t, http.StatusOK).JSON(t, &beyond)
	if len(beyond.Data) != 0 || beyond.Meta.Total < 2 {
		t.Fatalf("page 50 = %d rows, total %d; want no rows and the real total", len(beyond.Data), beyond.Meta.Total)
	}

	var counts struct {
		Current int64 `json:"current"`
		All     int64 `json:"all"`
	}
	staff.Get("/api/v1/billing/internal/subscriptions/summary").Status(t, http.StatusOK).Data(t, &counts)
	if counts.Current < 2 || counts.All < counts.Current {
		t.Fatalf("counts = %+v, want at least both organizations", counts)
	}

	staff.Get("/api/v1/billing/internal/subscriptions?view=everything").Status(t, http.StatusUnprocessableEntity)
	staff.Get("/api/v1/billing/internal/subscriptions?package_id=abc").Status(t, http.StatusUnprocessableEntity)

	// Payments: one organization's only, with tenant_id.
	var pkg struct {
		ID int64 `json:"id"`
	}
	staff.Post("/api/v1/packages", map[string]any{
		"name": "Paid", "price": 50000, "billing_cycle": "monthly", "is_active": true,
		"features": []map[string]any{{"feature_key": "users.max", "is_enabled": true, "limit_value": 10}},
	}).Status(t, http.StatusCreated).Data(t, &pkg)
	for _, payer := range []*testkit.Client{app.As(app.User(t)), app.As(app.User(t, testkit.InTenant(other)))} {
		payer.Post("/api/v1/billing/subscribe", map[string]any{
			"package_id": pkg.ID, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "STAFF-LIST"},
		}).Status(t, http.StatusCreated)
	}
	var payments []struct {
		TenantID int64 `json:"tenant_id"`
	}
	staff.Get("/api/v1/billing/internal/payments?tenant_id="+strconv.FormatInt(other, 10)).Status(t, http.StatusOK).Data(t, &payments)
	if len(payments) != 1 || payments[0].TenantID != other {
		t.Fatalf("payments for Beta Studio = %+v, want its one payment", payments)
	}
	staff.Get("/api/v1/billing/internal/payments?tenant_id=0").Status(t, http.StatusUnprocessableEntity)

	// An organization's own admin sees neither.
	admin := app.As(app.User(t))
	admin.Get("/api/v1/billing/internal/subscriptions").Status(t, http.StatusForbidden)
	admin.Get("/api/v1/billing/internal/subscriptions/summary").Status(t, http.StatusForbidden)
}
