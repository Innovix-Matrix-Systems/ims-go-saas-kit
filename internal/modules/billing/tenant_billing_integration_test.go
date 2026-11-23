package billing_test

import (
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// A tenant Admin manages the organization's own plan: the tenant billing
// routes take tenant permissions (billing.view / billing.manage), which the
// Admin role holds, not the system ones that tenant roles can never hold.
func TestTenantAdminManagesOwnBilling(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)

	var pkg struct {
		ID int64 `json:"id"`
	}
	app.As(staff).Post("/api/v1/packages", map[string]any{
		"name": "Pro", "price": 30000, "billing_cycle": "monthly", "is_active": true,
		"features": []map[string]any{{"feature_key": "users.max", "is_enabled": true, "limit_value": 25}},
	}).Status(t, http.StatusCreated).Data(t, &pkg)

	app.As(admin).Get("/api/v1/billing/payments").Status(t, http.StatusOK)
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pkg.ID, "payment_method": "bank_transfer",
	}).Status(t, http.StatusCreated)

	// A tenant role without the billing permissions is still refused.
	member := app.User(t, testkit.Role(app.Role(t, "Member")))
	app.As(member).Get("/api/v1/billing/payments").Status(t, http.StatusForbidden)
	app.As(member).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pkg.ID, "payment_method": "bank_transfer",
	}).Status(t, http.StatusForbidden)
}
