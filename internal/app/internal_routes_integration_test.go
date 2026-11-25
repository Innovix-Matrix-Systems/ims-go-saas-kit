package app_test

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// TestInternalRoutesRejectTenantScopedCallers is the regression net
// (docs/history/codebase-review.md C4): a tenant-bound identity — even one holding
// every tenant-grantable permission — must be rejected with 403 on EVERY
// /api/v1/internal/* route. "Internal" is a property of the account, proven
// server-side by the tenancy.InternalOnly gate; it cannot be asserted by
// being routed into the tenant-exempt group. This test would have caught
// C1–C3.
func TestInternalRoutesRejectTenantScopedCallers(t *testing.T) {

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := kit.Deps

	if err := seeder.Run(t.Context(), cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}

	router := deps.Router()

	// A tenant-bound administrator of the default tenant: the role seeded by
	// CreateDefaultRolesForTenant holds every tenant-grantable permission, so
	// any 403 below comes from the internal gate, not a missing permission.
	token := createTenantAdminAndLogin(t, router, deps)

	internalRoutes := collectInternalRoutes(t, router)
	// The tenants family is mounted in the same system-only subgroup but keeps
	// its historical (non-/internal) paths — cover it too.
	internalRoutes = append(internalRoutes,
		internalRoute{method: http.MethodGet, path: "/api/v1/tenant"},
		internalRoute{method: http.MethodPost, path: "/api/v1/tenant", body: "{}"},
		internalRoute{method: http.MethodGet, path: "/api/v1/tenant/1"},
		internalRoute{method: http.MethodPatch, path: "/api/v1/tenant/1", body: "{}"},
		internalRoute{method: http.MethodDelete, path: "/api/v1/tenant/1"},
	)
	if len(internalRoutes) < 25 {
		t.Fatalf("collected %d internal routes, want >= 25 — the walk broke", len(internalRoutes))
	}

	for _, route := range internalRoutes {
		method, path := route.method, route.path
		t.Run(method+" "+path, func(t *testing.T) {
			rec := apiRequest(t, router, method, path, route.body, token, nil)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
			}
			var problem struct {
				ErrorCode string `json:"error_code"`
			}
			decodeBody(t, rec, &problem)
			if problem.ErrorCode != "FORBIDDEN" {
				t.Errorf("error_code = %q, want FORBIDDEN", problem.ErrorCode)
			}
		})
	}
}

// createTenantAdminAndLogin provisions a tenant-bound admin of the default
// tenant directly in the database (the tenant-management API lives behind
// system-only internal routes — exactly the surface under test) and logs in
// for a tenant-scoped token.
func createTenantAdminAndLogin(t *testing.T, router http.Handler, deps *app.Dependencies) string {
	t.Helper()
	ctx := t.Context()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	adminRoleID := roleIDForTenant(t, ctx, deps, "Admin", 1)

	var userID int64
	if err := dbtest.Pool(t).QueryRow(ctx,
		`INSERT INTO users (tenant_id, branch_id, active_branch_id, name, email, password, is_active, email_verified_at)
		 VALUES (1, 1, 1, 'Gate Admin', 'internal-gate-admin@iso.test', $1, 1, now())
		 RETURNING id`, string(hash),
	).Scan(&userID); err != nil {
		t.Fatalf("create tenant admin: %v", err)
	}
	if _, err := dbtest.Pool(t).Exec(ctx,
		`INSERT INTO model_has_roles (model_type, model_id, role_id) VALUES ('user', $1, $2)`,
		userID, adminRoleID,
	); err != nil {
		t.Fatalf("assign admin role: %v", err)
	}
	return loginToken(t, router, "internal-gate-admin@iso.test", "secret123", "internal-gate")
}

type internalRoute struct {
	method string
	path   string
	body   string
}

// collectInternalRoutes walks the live router and returns every
// /api/v1/internal/* route with URL params substituted by plausible values.
func collectInternalRoutes(t *testing.T, router http.Handler) []internalRoute {
	t.Helper()
	mux, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router %T does not implement chi.Routes", router)
	}
	var routes []internalRoute
	if err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/internal/") {
			return nil
		}
		path := route
		for _, param := range []string{"id", "key", "jobId"} {
			path = strings.ReplaceAll(path, "{"+param+"}", "1")
		}
		if strings.Contains(path, "{") {
			t.Errorf("unsubstituted route param in %s", route)
		}
		body := ""
		if method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut {
			body = "{}"
		}
		routes = append(routes, internalRoute{method: method, path: path, body: body})
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return routes
}
