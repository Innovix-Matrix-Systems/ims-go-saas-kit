package app_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type profilePermissions struct {
	SuperAdmin  bool     `json:"super_admin"`
	Permissions []string `json:"permissions"`
	ActingAs    bool     `json:"acting_as"`
}

// Staff acting as a user (X-Acting-User) read that user's permissions, so a
// client can show the user's own screens; without the header they read their
// own. /permission/user cannot do this: it is tenant-exempt and refuses the
// header.
func TestProfilePermissionsFollowActingUser(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	member := app.User(t, testkit.Role(app.Role(t, "Viewer", "branch.view.all")))

	var own profilePermissions
	app.As(staff).Get("/api/v1/user/profile/permissions").Status(t, http.StatusOK).Data(t, &own)
	if !own.SuperAdmin || own.ActingAs {
		t.Fatalf("staff's own = %+v, want super admin, not acting", own)
	}

	var acting profilePermissions
	actingAs(app.As(staff), member).Get("/api/v1/user/profile/permissions").Status(t, http.StatusOK).Data(t, &acting)
	if acting.SuperAdmin || !acting.ActingAs || !slices.Equal(acting.Permissions, []string{"branch.view.all"}) {
		t.Fatalf("acting as the member = %+v, want only branch.view.all", acting)
	}
}

// platform=true lists staff accounts only; a tenant admin gets nobody.
func TestUserListPlatformFilter(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)

	var users []struct {
		ID       int64  `json:"id"`
		TenantID *int64 `json:"tenant_id"`
	}
	app.As(staff).Get("/api/v1/user?platform=true&per_page=100").Status(t, http.StatusOK).Data(t, &users)
	if len(users) == 0 {
		t.Fatal("no staff accounts listed")
	}
	for _, user := range users {
		if user.TenantID != nil {
			t.Fatalf("user %d of tenant %d listed as staff", user.ID, *user.TenantID)
		}
	}
	app.As(admin).Get("/api/v1/user?platform=true").Status(t, http.StatusOK).Data(t, &users)
	if len(users) != 0 {
		t.Fatalf("a tenant admin sees %d staff accounts, want 0", len(users))
	}
}

// The approval list tells a reviewer who pays, how much and for which plan.
func TestApprovalListCarriesDetails(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)

	var pkg struct {
		ID int64 `json:"id"`
	}
	app.As(staff).Post("/api/v1/packages", map[string]any{
		"name": "Pro Review", "price": 30000, "billing_cycle": "monthly", "is_active": true,
		"features": []map[string]any{{"feature_key": "users.max", "is_enabled": true, "limit_value": 25}},
	}).Status(t, http.StatusCreated).Data(t, &pkg)
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pkg.ID, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "REF-42"},
	}).Status(t, http.StatusCreated)

	var approvals []struct {
		Status  string `json:"status"`
		Details *struct {
			TenantName  string `json:"tenant_name"`
			PackageName string `json:"package_name"`
			Amount      int64  `json:"amount"`
		} `json:"details"`
	}
	app.As(staff).Get("/api/v1/billing/internal/payment-approvals?status=pending").Status(t, http.StatusOK).Data(t, &approvals)
	if len(approvals) != 1 || approvals[0].Details == nil {
		t.Fatalf("approvals = %+v, want one with details", approvals)
	}
	if d := approvals[0].Details; d.PackageName != "Pro Review" || d.Amount != 30000 || d.TenantName == "" {
		t.Fatalf("details = %+v", d)
	}
}
