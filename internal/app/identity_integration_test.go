package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

func apiRequest(t *testing.T, router http.Handler, method, path, body, token string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response (%d %s): %v", rec.Code, rec.Body.String(), err)
	}
}

func TestIdentityEndToEnd(t *testing.T) {

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := kit.Deps

	ctx := context.Background()
	runSeed := func() {
		t.Helper()
		if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
			t.Fatalf("seeder.Run() error = %v", err)
		}
	}
	runSeed()
	runSeed() // idempotency

	// Give the default tenant an unlimited trial so resource creation passes the
	// billing feature guard (users.max / branches.max).
	unlimited, err := deps.Platform.BillingPackages.Create(ctx, billingdomain.PackageInput{
		Name: "Test Unlimited", Price: 0, BillingCycle: billingdomain.CycleMonthly, TrialDays: 30, IsActive: true,
	}, []billingdomain.FeatureInput{
		{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: -1},
		{FeatureKey: string(billingdomain.FeatureBranchesMax), IsEnabled: true, LimitValue: -1},
	})
	if err != nil {
		t.Fatalf("create test package: %v", err)
	}
	if _, err := deps.Platform.BillingSubs.StartTrial(ctx, 1, unlimited.ID); err != nil {
		t.Fatalf("start tenant trial: %v", err)
	}

	router := deps.Router()

	// ── login as the seeded super admin ─────────────────────────────────
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"secret123","device":"suite-device"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Status string `json:"status"`
			Token  string `json:"token"`
			User   struct {
				Email string `json:"email"`
				Roles []struct {
					Name string `json:"name"`
				} `json:"roles"`
			} `json:"user"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	if login.Data.Status != "AUTH_SUCCESS" || login.Data.Token == "" {
		t.Fatalf("login payload = %+v", login.Data)
	}
	if len(login.Data.User.Roles) != 1 || login.Data.User.Roles[0].Name != "Super-Admin" {
		t.Fatalf("super admin roles = %+v", login.Data.User.Roles)
	}
	superToken := login.Data.Token

	// ── profile ─────────────────────────────────────────────────────────
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/user/profile", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile status = %d body = %s", rec.Code, rec.Body.String())
	}
	var profile struct {
		Data struct {
			Email    string `json:"email"`
			IsActive bool   `json:"is_active"`
		} `json:"data"`
	}
	decodeBody(t, rec, &profile)
	if profile.Data.Email != "superadmin@ims.com" || !profile.Data.IsActive {
		t.Fatalf("profile payload = %+v", profile.Data)
	}

	acting := map[string]string{"X-Acting-Tenant": "1", "X-Acting-Branch": "1"}

	// ── tenant-scoped writes without acting headers are forbidden ───────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/branch", `{"name":"Denied Branch"}`, superToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("write without acting headers status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── global (tenant-exempt) management works without acting headers ──
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/tenant", `{"name":"Global Tenant"}`, superToken, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("global tenant create status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── tenant creation with acting headers ─────────────────────────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/tenant", `{"name":"Acme Corp"}`, superToken, acting)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body = %s", rec.Code, rec.Body.String())
	}
	var tenant struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &tenant)
	if tenant.Data.ID == 0 {
		t.Fatal("created tenant id missing")
	}

	// ── branch creation under the acting tenant ─────────────────────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/branch",
		`{"name":"Dhaka HQ","address":"Gulshan 1"}`, superToken, acting)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create branch status = %d body = %s", rec.Code, rec.Body.String())
	}
	var branch struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &branch)
	if branch.Data.ID == 0 {
		t.Fatal("created branch id missing")
	}

	// ── create a tenant admin user with the tenant Admin role ───────────
	one := int64(1)
	adminRole, err := deps.Tx.Queries().GetRoleByName(ctx, dbgen.GetRoleByNameParams{Name: "Admin", TenantID: &one})
	if err != nil {
		t.Fatalf("lookup Admin role: %v", err)
	}
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/user",
		`{"name":"Acme Admin","email":"admin@acme.test","password":"secret123","branch_id":1,"roles":[`+strconv.FormatInt(adminRole.ID, 10)+`]}`,
		superToken, acting)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── the tenant admin can log in and list users ──────────────────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"admin@acme.test","password":"secret123","device":"admin-device"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant admin login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var adminLogin struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &adminLogin)
	adminToken := adminLogin.Data.Token
	if adminToken == "" {
		t.Fatal("tenant admin token missing")
	}

	rec = apiRequest(t, router, http.MethodGet, "/api/v1/user", "", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant admin list users status = %d body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	decodeBody(t, rec, &list)
	if list.Meta.Total != 1 {
		t.Fatalf("tenant user list total = %d, want 1", list.Meta.Total)
	}

	// ── tenant admin is denied system-level actions ─────────────────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/tenant", `{"name":"Nope"}`, adminToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant admin tenant.create status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── logout revokes the token ────────────────────────────────────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/logout", `{"device":"admin-device"}`, adminToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/user/profile", "", adminToken, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d body = %s", rec.Code, rec.Body.String())
	}
}
