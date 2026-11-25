package app_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"testing"

	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// TestIdentityWriteRules covers the identity invariants end to end:
// role changes need the role-assign permission, nobody changes their own
// roles, a self password change signs out other devices, and the users.max
// quota holds under concurrent creates.
func TestIdentityWriteRules(t *testing.T) {

	kit := testkit.New(t)
	cfg := kit.Config
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, pool := kit.Deps, kit.Pool
	ctx := context.Background()
	if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	unlimited, err := deps.Platform.BillingPackages.Create(ctx, billingdomain.PackageInput{
		Name: "Rules Unlimited", Price: 0, BillingCycle: billingdomain.CycleMonthly, TrialDays: 30, IsActive: true,
	}, []billingdomain.FeatureInput{
		{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: -1},
		{FeatureKey: string(billingdomain.FeatureBranchesMax), IsEnabled: true, LimitValue: -1},
	})
	if err != nil {
		t.Fatalf("create package: %v", err)
	}
	if _, err := deps.Platform.BillingSubs.StartTrial(ctx, 1, unlimited.ID); err != nil {
		t.Fatalf("start trial: %v", err)
	}

	router := deps.Router()
	superToken := loginToken(t, router, "superadmin@ims.com", "secret123", "rules-super")
	branchID := defaultBranchID(t, ctx, pool, 1)
	acting1 := map[string]string{"X-Acting-Tenant": "1", "X-Acting-Branch": strconv.FormatInt(branchID, 10)}

	adminRole := roleIDForTenant(t, ctx, deps, "Admin", 1)
	adminID := createUserViaAPI(t, router, superToken, "Rules Admin", "rules-admin@rules.test", adminRole, branchID, acting1)
	adminToken := loginToken(t, router, "rules-admin@rules.test", "secret123", "rules-admin")

	// An "editor" role holding user.update + user.view but not user.role.assign.
	editorRole := createRoleViaAPI(t, router, adminToken, "Rules Editor")
	if _, err := pool.Exec(ctx,
		`INSERT INTO role_has_permissions (role_id, permission_id)
		 SELECT $1, id FROM permissions WHERE name IN ('user.update', 'user.view')`, editorRole,
	); err != nil {
		t.Fatalf("grant editor permissions: %v", err)
	}
	editorID := createUserViaAPI(t, router, superToken, "Rules Editor", "rules-editor@rules.test", editorRole, branchID, acting1)
	editorToken := loginToken(t, router, "rules-editor@rules.test", "secret123", "rules-editor")
	targetID := createUserViaAPI(t, router, superToken, "Rules Target", "rules-target@rules.test", editorRole, branchID, acting1)

	userPath := func(id int64) string { return "/api/v1/user/" + strconv.FormatInt(id, 10) }

	t.Run("role change needs user.role.assign", func(t *testing.T) {
		body := fmt.Sprintf(`{"name":"Rules Target","email":"rules-target@rules.test","roles":[%d]}`, adminRole)
		if rec := apiRequest(t, router, http.MethodPatch, userPath(targetID), body, editorToken, nil); rec.Code != http.StatusForbidden {
			t.Errorf("editor PATCH with roles status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
		plain := `{"name":"Rules Target Renamed","email":"rules-target@rules.test"}`
		if rec := apiRequest(t, router, http.MethodPatch, userPath(targetID), plain, editorToken, nil); rec.Code != http.StatusOK {
			t.Errorf("editor PATCH without roles status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("stale updated_at answers 409", func(t *testing.T) {
		rec := apiRequest(t, router, http.MethodGet, userPath(targetID), "", adminToken, nil)
		var shown struct {
			Data struct {
				UpdatedAt string `json:"updated_at"`
			} `json:"data"`
		}
		decodeBody(t, rec, &shown)
		fresh := fmt.Sprintf(`{"name":"Rules Target v2","email":"rules-target@rules.test","updated_at":%q}`, shown.Data.UpdatedAt)
		if rec := apiRequest(t, router, http.MethodPatch, userPath(targetID), fresh, adminToken, nil); rec.Code != http.StatusOK {
			t.Fatalf("fresh PATCH status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		// Same (now stale) updated_at again: someone else's change won.
		stale := fmt.Sprintf(`{"name":"Rules Target v3","email":"rules-target@rules.test","updated_at":%q}`, shown.Data.UpdatedAt)
		if rec := apiRequest(t, router, http.MethodPatch, userPath(targetID), stale, adminToken, nil); rec.Code != http.StatusConflict {
			t.Errorf("stale PATCH status = %d, want 409 (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("nobody changes their own roles", func(t *testing.T) {
		body := fmt.Sprintf(`{"name":"Rules Admin","email":"rules-admin@rules.test","roles":[%d]}`, editorRole)
		if rec := apiRequest(t, router, http.MethodPatch, userPath(adminID), body, adminToken, nil); rec.Code != http.StatusForbidden {
			t.Errorf("self PATCH roles status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
		assign := fmt.Sprintf(`{"user_id":%d,"roles":[%d]}`, adminID, editorRole)
		if rec := apiRequest(t, router, http.MethodPost, "/api/v1/user/assign-role", assign, adminToken, nil); rec.Code != http.StatusForbidden {
			t.Errorf("self assign-role status = %d, want 403 (%s)", rec.Code, rec.Body.String())
		}
		assignOther := fmt.Sprintf(`{"user_id":%d,"roles":[%d]}`, editorID, editorRole)
		if rec := apiRequest(t, router, http.MethodPost, "/api/v1/user/assign-role", assignOther, adminToken, nil); rec.Code != http.StatusOK {
			t.Errorf("assign-role on another user status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("self password change signs out other devices", func(t *testing.T) {
		other := loginToken(t, router, "rules-editor@rules.test", "secret123", "rules-editor-phone")
		body := `{"current_password":"secret123","password":"newsecret456"}`
		if rec := apiRequest(t, router, http.MethodPost, "/api/v1/user/profile/change-password", body, editorToken, nil); rec.Code >= 300 {
			t.Fatalf("change password status = %d (%s)", rec.Code, rec.Body.String())
		}
		if rec := apiRequest(t, router, http.MethodGet, "/api/v1/user/profile", "", other, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("other device after password change status = %d, want 401", rec.Code)
		}
		if rec := apiRequest(t, router, http.MethodGet, "/api/v1/user/profile", "", editorToken, nil); rec.Code != http.StatusOK {
			t.Errorf("current device after password change status = %d, want 200", rec.Code)
		}
	})

	t.Run("tenantless accounts can sign out", func(t *testing.T) {
		token := loginToken(t, router, "superadmin@ims.com", "secret123", "rules-logout")
		if rec := apiRequest(t, router, http.MethodPost, "/api/v1/logout", `{"device":"rules-logout"}`, token, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("logout status = %d, want 204 (%s)", rec.Code, rec.Body.String())
		}
		if rec := apiRequest(t, router, http.MethodGet, "/api/v1/user/profile", "", token, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("token after logout status = %d, want 401", rec.Code)
		}
	})

	t.Run("users.max holds under concurrent creates", func(t *testing.T) {
		var current int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE tenant_id = 1 AND deleted_at IS NULL`).Scan(&current); err != nil {
			t.Fatalf("count users: %v", err)
		}
		const allowed = 3
		limited, err := deps.Platform.BillingPackages.Create(ctx, billingdomain.PackageInput{
			Name: "Rules Limited", Price: 0, BillingCycle: billingdomain.CycleMonthly, TrialDays: 30, IsActive: true,
		}, []billingdomain.FeatureInput{
			{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: current + allowed},
			{FeatureKey: string(billingdomain.FeatureBranchesMax), IsEnabled: true, LimitValue: -1},
		})
		if err != nil {
			t.Fatalf("create limited package: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE subscriptions SET package_id = $1 WHERE tenant_id = 1`, limited.ID); err != nil {
			t.Fatalf("switch package: %v", err)
		}

		const attempts = 10
		var wg sync.WaitGroup
		codes := make([]int, attempts)
		for i := range attempts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body := fmt.Sprintf(`{"name":"Quota %d","email":"quota-%d@rules.test","password":"secret123"}`, i, i)
				codes[i] = apiRequest(t, router, http.MethodPost, "/api/v1/user", body, adminToken, nil).Code
			}()
		}
		wg.Wait()

		created := 0
		for _, code := range codes {
			if code == http.StatusCreated {
				created++
			}
		}
		var after int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE tenant_id = 1 AND deleted_at IS NULL`).Scan(&after); err != nil {
			t.Fatalf("count users: %v", err)
		}
		if created != allowed || after != current+allowed {
			t.Errorf("created = %d (codes %v), users %d -> %d; want exactly %d new", created, codes, current, after, allowed)
		}
	})
}
