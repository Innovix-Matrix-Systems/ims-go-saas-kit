package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestDeveloperRoleProvisioning proves the panel can recover a database that
// was seeded without the hidden Developer role: the role is missing, creating
// an account is refused with an actionable conflict, and one click provisions
// the role with the full catalog before creating a working developer account.
func TestDeveloperRoleProvisioning(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, pool := kit.Deps, kit.Pool
	if err := seeder.Run(context.Background(), cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"secret123","device":"role-provisioning"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	superToken := login.Data.Token
	if superToken == "" {
		t.Fatal("super admin token missing")
	}

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM roles WHERE name = $1 AND tenant_id IS NULL`, authz.DeveloperRole,
	); err != nil {
		t.Fatalf("delete developer role: %v", err)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/developers/stats", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("missing-role stats status = %d body = %s", rec.Code, rec.Body.String())
	}
	var stats struct {
		Data struct {
			RoleExists      bool  `json:"role_exists"`
			RolePermissions int64 `json:"role_permissions"`
		} `json:"data"`
	}
	decodeBody(t, rec, &stats)
	if stats.Data.RoleExists {
		t.Fatalf("role_exists = true after deleting the role")
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/developers",
		`{"name":"Blocked Dev","email":"blocked-dev@example.test","password":"secret123"}`, superToken, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("create developer without role = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/developers/role", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ensure developer role status = %d body = %s", rec.Code, rec.Body.String())
	}
	stats = struct {
		Data struct {
			RoleExists      bool  `json:"role_exists"`
			RolePermissions int64 `json:"role_permissions"`
		} `json:"data"`
	}{}
	decodeBody(t, rec, &stats)
	if !stats.Data.RoleExists || stats.Data.RolePermissions != int64(len(authz.DeveloperPermissions())) {
		t.Fatalf("ensure developer role stats = %+v, want %d permissions", stats.Data, len(authz.DeveloperPermissions()))
	}

	var hidden, system bool
	if err := pool.QueryRow(context.Background(),
		`SELECT hidden, is_system FROM roles WHERE name = $1 AND tenant_id IS NULL`, authz.DeveloperRole,
	).Scan(&hidden, &system); err != nil {
		t.Fatalf("read provisioned role: %v", err)
	}
	if !hidden || !system {
		t.Fatalf("provisioned role hidden=%v system=%v, want both true", hidden, system)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/developers",
		`{"name":"Provisioned Dev","email":"provisioned-dev@example.test","password":"secret123","is_active":true}`,
		superToken, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create developer after provisioning status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"provisioned-dev@example.test","password":"secret123","device":"provisioned-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("provisioned developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var devLogin struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &devLogin)
	if devLogin.Data.Token == "" {
		t.Fatal("provisioned developer token missing")
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", devLogin.Data.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("provisioned developer me status = %d body = %s", rec.Code, rec.Body.String())
	}
}
