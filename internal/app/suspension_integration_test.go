package app_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// Suspended tenants: is_active false locks a tenant's users out
// of every authenticated route and of sign-in; logout stays open and platform
// accounts can still act inside the tenant (docs/guides/tenancy.md).

func setTenantActive(t *testing.T, app *testkit.App, tenantID int64, name string, active bool) {
	t.Helper()
	app.As(app.SuperAdmin(t)).
		Patch("/api/v1/tenant/"+strconv.FormatInt(tenantID, 10), map[string]any{"name": name, "is_active": active}).
		Status(t, http.StatusOK)
}

func requireErrorCode(t *testing.T, res *testkit.Response, status int, code string) {
	t.Helper()
	var problem struct {
		ErrorCode string `json:"error_code"`
	}
	res.Status(t, status).JSON(t, &problem)
	if problem.ErrorCode != code {
		t.Fatalf("error_code = %q, want %q", problem.ErrorCode, code)
	}
}

func TestSuspendedTenantIsLockedOut(t *testing.T) {
	app := testkit.New(t)
	tenantID := app.Tenant(t, "Acme")
	user := app.User(t, testkit.InTenant(tenantID))
	other := app.User(t) // the seeded tenant is not affected
	var phone struct {
		Token string `json:"token"`
	}
	app.Guest().Post("/api/v1/login", map[string]any{"email": user.Email, "password": testkit.Password, "device": "phone"}).
		Status(t, http.StatusOK).Data(t, &phone)
	second := &testkit.User{ID: user.ID, Token: phone.Token}

	app.As(user).Get("/api/v1/branch").Status(t, http.StatusOK)

	setTenantActive(t, app, tenantID, "Acme", false)

	// Tenant routes, exempt routes and sign-in are refused.
	requireErrorCode(t, app.As(user).Get("/api/v1/branch"), http.StatusForbidden, "TENANT_SUSPENDED")
	requireErrorCode(t, app.As(user).Get("/api/v1/files/1"), http.StatusForbidden, "TENANT_SUSPENDED")
	requireErrorCode(t,
		app.Guest().Post("/api/v1/login", map[string]any{"email": user.Email, "password": testkit.Password, "device": "web"}),
		http.StatusForbidden, "TENANT_SUSPENDED")
	// A wrong password still answers like any wrong password (no enumeration).
	requireErrorCode(t,
		app.Guest().Post("/api/v1/login", map[string]any{"email": user.Email, "password": "wrong-password", "device": "web"}),
		http.StatusUnauthorized, "INVALID_CREDENTIALS")

	// Other tenants and platform support work keep going.
	app.As(other).Get("/api/v1/branch").Status(t, http.StatusOK)
	app.As(app.SuperAdmin(t)).InTenant(tenantID, app.DefaultBranch(t, tenantID)).
		Get("/api/v1/branch").Status(t, http.StatusOK)
	app.As(app.SuperAdmin(t)).Header("X-Acting-User", strconv.FormatInt(user.ID, 10)).
		Get("/api/v1/branch").Status(t, http.StatusOK)

	// Logout stays open so a client can clean up its token.
	app.As(user).Post("/api/v1/logout", map[string]any{"device": "testkit"}).Status(t, http.StatusNoContent)

	// Reactivating restores access with the tokens the users already hold.
	setTenantActive(t, app, tenantID, "Acme", true)
	app.As(second).Get("/api/v1/branch").Status(t, http.StatusOK)

	suspended := countRows(t, app, `SELECT count(*) FROM activity_log
		WHERE action = 'tenant.suspended' AND tenant_id IS NULL AND subject_id = $1`, tenantID)
	reactivated := countRows(t, app, `SELECT count(*) FROM activity_log
		WHERE action = 'tenant.reactivated' AND tenant_id IS NULL AND subject_id = $1`, tenantID)
	if suspended != 1 || reactivated != 1 {
		t.Fatalf("activity: suspended=%d reactivated=%d, want 1 and 1", suspended, reactivated)
	}

	// Saving the tenant without changing is_active records nothing new.
	setTenantActive(t, app, tenantID, "Acme Ltd", true)
	if n := countRows(t, app, `SELECT count(*) FROM activity_log WHERE action LIKE 'tenant.%' AND subject_id = $1`, tenantID); n != 2 {
		t.Fatalf("tenant activity rows = %d, want 2", n)
	}
}

func TestLapsedTenantsAreSuspendedByBilling(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	lapsed := app.Tenant(t, "Lapsed")
	recent := app.Tenant(t, "Recently expired")
	pending := app.Tenant(t, "Renewal pending")
	user := app.User(t, testkit.InTenant(lapsed))

	expire := func(tenantID int64, ago time.Duration) {
		t.Helper()
		_, err := app.Pool.Exec(ctx, `UPDATE subscriptions SET status = 'expired', expires_at = $2 WHERE tenant_id = $1`,
			tenantID, time.Now().Add(-ago))
		if err != nil {
			t.Fatalf("expire subscription: %v", err)
		}
	}
	expire(lapsed, 5*24*time.Hour)
	expire(recent, 24*time.Hour)
	expire(pending, 5*24*time.Hour)
	expire(testkit.DefaultTenant, 30*24*time.Hour) // the default tenant is never suspended
	if _, err := app.Pool.Exec(ctx, `INSERT INTO subscriptions (tenant_id, package_id, status)
		SELECT tenant_id, package_id, 'pending' FROM subscriptions WHERE tenant_id = $1 LIMIT 1`, pending); err != nil {
		t.Fatalf("pending subscription: %v", err)
	}

	suspended, err := app.Deps.Platform.BillingSubs.SuspendLapsedTenants(ctx, 3*24*time.Hour, testkit.DefaultTenant)
	if err != nil {
		t.Fatalf("SuspendLapsedTenants: %v", err)
	}
	if len(suspended) != 1 || suspended[0].ID != lapsed {
		t.Fatalf("suspended = %+v, want only tenant %d", suspended, lapsed)
	}
	requireErrorCode(t, app.As(user).Get("/api/v1/branch"), http.StatusForbidden, "TENANT_SUSPENDED")
	if n := countRows(t, app, `SELECT count(*) FROM activity_log
		WHERE action = 'tenant.suspended' AND subject_id = $1 AND changes->>'reason' = 'subscription_expired'`, lapsed); n != 1 {
		t.Fatalf("billing suspension activity rows = %d, want 1", n)
	}

	// A second run finds nothing new.
	again, err := app.Deps.Platform.BillingSubs.SuspendLapsedTenants(ctx, 3*24*time.Hour, testkit.DefaultTenant)
	if err != nil || len(again) != 0 {
		t.Fatalf("second run = %+v, %v; want none", again, err)
	}
}

func TestSuspendedTenantAgentIsRefused(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	tenantID := app.Tenant(t, "Acme")
	user := app.User(t, testkit.InTenant(tenantID))
	agent := app.Agent(t, user)
	agent.Call(t, "list_branches", map[string]any{}).OK(t)

	setTenantActive(t, app, tenantID, "Acme", false)
	agent.Call(t, "list_branches", map[string]any{}).Refused(t, "TENANT_SUSPENDED")
}
