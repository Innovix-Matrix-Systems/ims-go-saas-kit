package billing_test

import (
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type usage struct {
	Limits []struct {
		FeatureKey string `json:"feature_key"`
		Limit      int64  `json:"limit"`
		Used       int64  `json:"used"`
	} `json:"limits"`
	StorageBytes int64 `json:"storage_bytes"`
}

// An organization sees its own use of each plan limit; other organizations'
// members and branches do not count.
func TestTenantUsage(t *testing.T) {
	app := testkit.New(t)
	app.User(t) // a member of the default tenant, which must not count below
	tenant := app.Tenant(t, "Usage Co")
	admin := app.User(t, testkit.InTenant(tenant))
	app.User(t, testkit.InTenant(tenant))
	app.As(admin).Post("/api/v1/branch", map[string]any{"name": "Second"}).Status(t, http.StatusCreated)

	var got usage
	app.As(admin).Get("/api/v1/billing/usage").Status(t, http.StatusOK).Data(t, &got)
	byKey := map[string][2]int64{}
	for _, item := range got.Limits {
		byKey[item.FeatureKey] = [2]int64{item.Limit, item.Used}
	}
	if byKey["users.max"] != [2]int64{-1, 2} {
		t.Fatalf("users.max = %v, want unlimited with 2 used", byKey["users.max"])
	}
	if byKey["branches.max"] != [2]int64{-1, 2} {
		t.Fatalf("branches.max = %v, want unlimited with 2 used", byKey["branches.max"])
	}
	if byKey["storage.mb"] != [2]int64{0, 0} {
		t.Fatalf("storage.mb = %v, want not in the plan and nothing used", byKey["storage.mb"])
	}

	member := app.User(t, testkit.Role(app.Role(t, "Member")))
	app.As(member).Get("/api/v1/billing/usage").Status(t, http.StatusForbidden)
}
