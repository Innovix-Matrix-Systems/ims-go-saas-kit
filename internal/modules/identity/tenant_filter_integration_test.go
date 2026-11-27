package identity_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// Staff narrow the users list to one organization with tenant_id (the console
// links there from an organization); an organization's own members always see
// only theirs, whatever tenant_id they send.
func TestUsersListByOrganization(t *testing.T) {
	app := testkit.New(t)
	other := app.Tenant(t, "Beta Studio")
	beta := app.User(t, testkit.InTenant(other), testkit.Name("Beta Member"))
	home := app.User(t, testkit.Name("Home Member"))

	var users []struct {
		ID       int64  `json:"id"`
		TenantID *int64 `json:"tenant_id"`
	}
	staff := app.As(app.SuperAdmin(t))
	staff.Get("/api/v1/user?tenant_id="+strconv.FormatInt(other, 10)).Status(t, http.StatusOK).Data(t, &users)
	if len(users) == 0 {
		t.Fatal("staff see no members of Beta Studio")
	}
	for _, user := range users {
		if user.TenantID == nil || *user.TenantID != other {
			t.Fatalf("staff list for Beta Studio has user %d of tenant %v", user.ID, user.TenantID)
		}
	}
	found := false
	for _, user := range users {
		found = found || user.ID == beta.ID
	}
	if !found {
		t.Fatalf("staff list for Beta Studio misses its member %d", beta.ID)
	}
	staff.Get("/api/v1/user?tenant_id=abc").Status(t, http.StatusUnprocessableEntity)

	// A member asking for another organization still gets only their own.
	app.As(home).Get("/api/v1/user?tenant_id="+strconv.FormatInt(other, 10)).Status(t, http.StatusOK).Data(t, &users)
	for _, user := range users {
		if user.TenantID == nil || *user.TenantID != testkit.DefaultTenant {
			t.Fatalf("a member's list with another tenant_id returned user %d of tenant %v", user.ID, user.TenantID)
		}
	}
}
