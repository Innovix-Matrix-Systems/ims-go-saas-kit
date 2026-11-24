package app_test

import (
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// routesWithoutPermission lists every /api/v1 route that deliberately needs
// no catalog permission: public endpoints and self-service endpoints that act
// only on the caller's own account. Everything else must answer 403 to an
// authenticated user with zero permissions.
//
// Permission checks live in handlers (docs/architecture/decisions.md, decision 23), so this
// test is the safety net that a new handler forgot its MustCan: adding a
// route either adds a permission check or an entry here, on purpose.
var routesWithoutPermission = map[string]bool{
	// public
	"POST /api/v1/login":                           true,
	"POST /api/v1/verify-otp":                      true,
	"GET /api/v1/social/providers":                 true,
	"POST /api/v1/social/{provider}/login":         true,
	"POST /api/v1/forgot-password":                 true,
	"POST /api/v1/reset-password":                  true,
	"POST /api/v1/email/verification-notification": true,
	"POST /api/v1/email/verify":                    true,
	"POST /api/v1/invitation/accept":               true,
	"POST /api/v1/signup":                          true, // creates a new organization, rate-limited per IP
	"POST /api/v1/invitation/preview":              true, // needs the invitation link (token + e-mail)
	"POST /api/v1/email/change/confirm":            true, // needs the link mailed to the new address
	"POST /api/v1/user/profile/change-email":       true, // the caller's own address
	"GET /api/v1/public/packages":                  true,
	"GET /api/v1/public/tenant":                    true, // tenant from X-Tenant-Code, no sign-in
	"POST /api/v1/billing/{gateway}/callback":      true,
	"GET /api/v1/billing/{gateway}/callback":       true,
	// self-service: the caller's own session, profile and inbox
	"POST /api/v1/logout":                                 true,
	"GET /api/v1/sessions":                                true, // the caller's own sessions
	"GET /api/v1/two-factor":                              true, // the caller's own two-factor
	"POST /api/v1/two-factor":                             true,
	"DELETE /api/v1/two-factor":                           true,
	"POST /api/v1/two-factor/confirm":                     true,
	"POST /api/v1/two-factor/recovery-codes":              true,
	"POST /api/v1/sessions/revoke-others":                 true,
	"DELETE /api/v1/sessions/{id}":                        true,
	"GET /api/v1/user/privacy/deletion":                   true, // the caller's own account deletion
	"POST /api/v1/user/privacy/deletion":                  true,
	"DELETE /api/v1/user/privacy/deletion":                true,
	"POST /api/v1/user/privacy/export":                    true, // the caller's own data export
	"GET /api/v1/user/profile":                            true,
	"GET /api/v1/user/profile/permissions":                true, // the caller's own permissions
	"GET /api/v1/user/connections":                        true, // the caller's own MCP connections
	"DELETE /api/v1/user/connections/{id}":                true,
	"POST /api/v1/user/profile/update":                    true,
	"POST /api/v1/user/profile/update-avatar":             true,
	"POST /api/v1/user/profile/change-password":           true,
	"PATCH /api/v1/internal/user/profile/update":          true,
	"PATCH /api/v1/internal/user/profile/change-password": true,
	"POST /api/v1/internal/user/profile/update-avatar":    true,
	"PATCH /api/v1/user/switch-branch/{id}":               true,
	"GET /api/v1/permission/user":                         true,
	"GET /api/v1/system/info":                             true,
	"GET /api/v1/system/echo":                             true,
	"POST /api/v1/system/echo":                            true,
	"GET /api/v1/settings":                                true, // resolved, non-secret settings for clients
	"GET /api/v1/notification/feed":                       true,
	"POST /api/v1/notification/read-all":                  true,
	"POST /api/v1/notification/{id}/read":                 true,
	"GET /api/v1/notification/preferences":                true,
	"PUT /api/v1/notification/preferences":                true,
	"GET /api/v1/notification/unsubscribe":                true, // public: the sealed token names the user
	"POST /api/v1/notification/unsubscribe":               true,
	// owner-scoped: the service only returns the caller's own records
	"GET /api/v1/data-processing/jobs/user":                   true,
	"GET /api/v1/data-processing/job/{jobId}":                 true,
	"GET /api/v1/data-processing/job/{jobId}/download":        true,
	"POST /api/v1/data-processing/job/{jobId}/retry":          true, // the job owner (checked in the handler)
	"DELETE /api/v1/data-processing/job/{jobId}":              true, // the job owner (checked in the handler)
	"GET /api/v1/announcements/live":                          true, // every member; only their organization's live announcements
	"POST /api/v1/announcements/{id}/events":                  true, // every member; their own view, click or dismissal
	"GET /api/v1/dashboard":                                   true, // every member; each section checks its own permission
	"GET /api/v1/internal/dashboard":                          true, // staff; each section checks its own permission
	"POST /api/v1/internal/data-processing/job/{jobId}/retry": true, // staff, their own job (checked in the handler)
	"DELETE /api/v1/internal/data-processing/job/{jobId}":     true, // staff, their own job (checked in the handler)
	"GET /api/v1/files/{id}":                                  true, // creator or file.view
	"DELETE /api/v1/files/{id}":                               true, // creator or file.delete
	"GET /api/v1/files/{id}/download":                         true, // same rule as show
	"GET /api/v1/internal/files/{id}":                         true, // creator or file.internal.view
	"DELETE /api/v1/internal/files/{id}":                      true, // creator or file.internal.delete
	"GET /api/v1/internal/files/{id}/download":                true, // same rule as show
	"GET /api/v1/feature-flags":                               true,
	"GET /api/v1/billing/subscription/status":                 true,
	"GET /api/v1/onboarding":                                  true, // the caller's own organization's setup
	"POST /api/v1/onboarding/complete":                        true, // only the organization's owner (checked in the service)
	"GET /api/v1/organization":                                true, // every member reads their own organization
	"POST /api/v1/organization/transfer-ownership":            true, // only the owner (checked in the service)
}

// TestEveryProtectedRouteRequiresAPermission logs in a tenant user that holds
// no roles at all and calls every /api/v1 route. Each one must refuse with 403
// unless it is listed in routesWithoutPermission.
func TestEveryProtectedRouteRequiresAPermission(t *testing.T) {

	kit := testkit.New(t)
	cfg := kit.Config
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, pool := kit.Deps, kit.Pool
	if err := seeder.Run(t.Context(), cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (tenant_id, branch_id, active_branch_id, name, email, password, is_active, email_verified_at)
		 VALUES (1, 1, 1, 'No Perms', 'no-perms@routes.test', $1, 1, now())`, string(hash),
	); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := loginToken(t, router, "no-perms@routes.test", "secret123", "routes")

	mux, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router %T does not implement chi.Routes", router)
	}
	type route struct{ method, pattern string }
	var routes []route
	if err := chi.Walk(mux, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(pattern, "/api/v1/") {
			routes = append(routes, route{method, pattern})
		}
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].pattern+routes[i].method < routes[j].pattern+routes[j].method })
	if len(routes) < 80 {
		t.Fatalf("collected %d routes, want >= 80 — the walk broke", len(routes))
	}

	for _, rt := range routes {
		key := rt.method + " " + rt.pattern
		if routesWithoutPermission[key] {
			continue
		}
		t.Run(key, func(t *testing.T) {
			path := rt.pattern
			for _, param := range []string{"id", "key", "gateway"} {
				path = strings.ReplaceAll(path, "{"+param+"}", "1")
			}
			body := ""
			if rt.method == http.MethodPost || rt.method == http.MethodPatch || rt.method == http.MethodPut {
				body = "{}"
			}
			rec := apiRequest(t, router, rt.method, path, body, token, nil)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403 (body: %.200s)", rec.Code, rec.Body.String())
			}
		})
	}
}
