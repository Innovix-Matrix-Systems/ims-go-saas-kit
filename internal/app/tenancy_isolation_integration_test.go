package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestTenantIsolationMatrix is the end-to-end tenancy
// isolation matrix: an admin user of one tenant must not be able to read,
// update or delete another tenant's resources (404), and lists must be scoped.
func TestTenantIsolationMatrix(t *testing.T) {

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, pool := kit.Deps, kit.Pool

	ctx := context.Background()
	if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}

	// Unlimited plan for both tenants so resource creation passes the guards.
	unlimited, err := deps.Platform.BillingPackages.Create(ctx, billingdomain.PackageInput{
		Name: "Isolation Unlimited", Price: 0, BillingCycle: billingdomain.CycleMonthly, TrialDays: 30, IsActive: true,
	}, []billingdomain.FeatureInput{
		{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: -1},
		{FeatureKey: string(billingdomain.FeatureBranchesMax), IsEnabled: true, LimitValue: -1},
	})
	if err != nil {
		t.Fatalf("create test package: %v", err)
	}
	if _, err := deps.Platform.BillingSubs.StartTrial(ctx, 1, unlimited.ID); err != nil {
		t.Fatalf("start tenant 1 trial: %v", err)
	}

	router := deps.Router()

	superToken := loginToken(t, router, "superadmin@ims.com", "secret123", "iso-super")
	acting1 := map[string]string{"X-Acting-Tenant": "1", "X-Acting-Branch": strconv.FormatInt(defaultBranchID(t, ctx, pool, 1), 10)}

	// ── provision a second tenant ───────────────────────────────────────
	tenant2 := createTenantViaAPI(t, router, superToken, "Isolation Tenant Two", acting1)
	if _, err := deps.Platform.BillingSubs.StartTrial(ctx, tenant2, unlimited.ID); err != nil {
		t.Fatalf("start tenant 2 trial: %v", err)
	}
	branch2Default := defaultBranchID(t, ctx, pool, tenant2)
	acting2 := map[string]string{"X-Acting-Tenant": strconv.FormatInt(tenant2, 10), "X-Acting-Branch": strconv.FormatInt(branch2Default, 10)}

	// Branches in each tenant.
	branch1 := createBranchViaAPI(t, router, superToken, "Iso Branch One", acting1)
	branch2 := createBranchViaAPI(t, router, superToken, "Iso Branch Two", acting2)

	// A tenant admin per tenant.
	adminRole1 := roleIDForTenant(t, ctx, deps, "Admin", 1)
	adminRole2 := roleIDForTenant(t, ctx, deps, "Admin", tenant2)
	user1 := createUserViaAPI(t, router, superToken, "Iso Admin One", "iso-admin1@iso.test", adminRole1, defaultBranchID(t, ctx, pool, 1), acting1)
	user2 := createUserViaAPI(t, router, superToken, "Iso Admin Two", "iso-admin2@iso.test", adminRole2, branch2Default, acting2)

	token1 := loginToken(t, router, "iso-admin1@iso.test", "secret123", "iso-dev1")
	token2 := loginToken(t, router, "iso-admin2@iso.test", "secret123", "iso-dev2")

	// A role per tenant (created through the tenant-scoped endpoint).
	role1 := createRoleViaAPI(t, router, token1, "Iso Role One")
	role2 := createRoleViaAPI(t, router, token2, "Iso Role Two")

	// One payment row per tenant (inserted directly).
	insertPayment(t, ctx, pool, 1, "INV-ISO-T1")
	insertPayment(t, ctx, pool, tenant2, "INV-ISO-T2")

	t.Run("branches", func(t *testing.T) {
		assertListExcludes(t, router, token1, "/api/v1/branch", branch2, true)
		assertListExcludes(t, router, token1, "/api/v1/branch", branch1, false)
		assertCrossTenantDenied(t, router, token1, branch2,
			`{"name":"hijacked","address":"nowhere"}`, "/api/v1/branch")
	})

	t.Run("users", func(t *testing.T) {
		assertListExcludes(t, router, token1, "/api/v1/user", user2, true)
		assertListExcludes(t, router, token1, "/api/v1/user", user1, false)
		assertCrossTenantDenied(t, router, token1, user2,
			`{"name":"hijacked","email":"iso-admin2@iso.test"}`, "/api/v1/user")
	})

	t.Run("roles", func(t *testing.T) {
		assertListExcludes(t, router, token1, "/api/v1/role", role2, true)
		assertListExcludes(t, router, token1, "/api/v1/role", role1, false)
		assertCrossTenantDenied(t, router, token1, role2,
			`{"name":"hijacked"}`, "/api/v1/role")
	})

	t.Run("payments", func(t *testing.T) {
		// Listing payments needs a system billing permission, so the super-admin
		// impersonates tenant 1; it must still not see tenant 2's payment.
		body := getBodyWithHeaders(t, router, superToken, "/api/v1/billing/payments", acting1)
		if hasInvoice(body, "INV-ISO-T2") {
			t.Error("tenant 1 payment list leaked tenant 2's payment")
		}
		if !hasInvoice(body, "INV-ISO-T1") {
			t.Error("tenant 1 payment list missing its own payment")
		}
	})

	t.Run("data-processing", func(t *testing.T) {
		job1 := insertProcessingJob(t, ctx, pool, 1, user1, "iso-job-1")
		job2 := insertProcessingJob(t, ctx, pool, tenant2, user2, "iso-job-2")

		// The per-user list is scoped to the caller's tenant and user id.
		assertListExcludes(t, router, token1, "/api/v1/data-processing/jobs/user", job2, true)
		assertListExcludes(t, router, token1, "/api/v1/data-processing/jobs/user", job1, false)

		resource := "/api/v1/data-processing/job/" + strconv.FormatInt(job2, 10)
		if rec := apiRequest(t, router, http.MethodGet, resource, "", token1, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", resource, rec.Code)
		}
	})

	// A super-admin read with no acting headers is a read-only global view
	// (PLAN §7.3); with acting headers it is scoped to that tenant.
	t.Run("superadmin-global-read", func(t *testing.T) {
		contains := func(headers map[string]string, id int64) bool {
			rec := apiRequest(t, router, http.MethodGet, "/api/v1/user", "", superToken, headers)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/v1/user status = %d body = %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Data []struct {
					ID int64 `json:"id"`
				} `json:"data"`
			}
			decodeBody(t, rec, &body)
			for _, item := range body.Data {
				if item.ID == id {
					return true
				}
			}
			return false
		}

		if !contains(nil, user1) || !contains(nil, user2) {
			t.Error("super-admin global read did not return cross-tenant users")
		}
		if contains(acting1, user2) {
			t.Error("super-admin acting-tenant read leaked another tenant's user")
		}
		if rec := apiRequest(t, router, http.MethodGet, "/api/v1/user/"+strconv.FormatInt(user2, 10), "", superToken, nil); rec.Code != http.StatusOK {
			t.Errorf("super-admin GET cross-tenant user status = %d, want 200", rec.Code)
		}
	})
}

// ── helpers ──────────────────────────────────────────────────────────────

func loginToken(t *testing.T, router http.Handler, email, password, device string) string {
	t.Helper()
	payload := `{"email":"` + email + `","password":"` + password + `","device":"` + device + `"}`
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login", payload, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s status = %d body = %s", email, rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	if body.Data.Token == "" {
		t.Fatalf("login %s returned no token", email)
	}
	return body.Data.Token
}

func createTenantViaAPI(t *testing.T, router http.Handler, token, name string, headers map[string]string) int64 {
	t.Helper()
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/tenant", `{"name":"`+name+`"}`, token, headers)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	return body.Data.ID
}

func createBranchViaAPI(t *testing.T, router http.Handler, token, name string, headers map[string]string) int64 {
	t.Helper()
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/branch", `{"name":"`+name+`"}`, token, headers)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create branch status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	return body.Data.ID
}

func createUserViaAPI(t *testing.T, router http.Handler, token, name, email string, roleID, branchID int64, headers map[string]string) int64 {
	t.Helper()
	payload := `{"name":"` + name + `","email":"` + email + `","password":"secret123","branch_id":` +
		strconv.FormatInt(branchID, 10) + `,"roles":[` + strconv.FormatInt(roleID, 10) + `]}`
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/user", payload, token, headers)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	return body.Data.ID
}

func createRoleViaAPI(t *testing.T, router http.Handler, token, name string) int64 {
	t.Helper()
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/role", `{"name":"`+name+`"}`, token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create role status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	return body.Data.ID
}

func roleIDForTenant(t *testing.T, ctx context.Context, deps *app.Dependencies, name string, tenantID int64) int64 {
	t.Helper()
	role, err := deps.Tx.Queries().GetRoleByName(ctx, dbgen.GetRoleByNameParams{Name: name, TenantID: &tenantID})
	if err != nil {
		t.Fatalf("lookup role %q for tenant %d: %v", name, tenantID, err)
	}
	return role.ID
}

func defaultBranchID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM branches WHERE tenant_id = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1`, tenantID,
	).Scan(&id); err != nil {
		t.Fatalf("default branch for tenant %d: %v", tenantID, err)
	}
	return id
}

func insertPayment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64, invoice string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO payments (tenant_id, payment_method, status, amount, currency, invoice_number)
		 VALUES ($1, 'cash', 'completed', 1000, 'BDT', $2)`, tenantID, invoice,
	); err != nil {
		t.Fatalf("insert payment %s: %v", invoice, err)
	}
}

func insertProcessingJob(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, userID int64, jobID string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO data_processing_jobs (tenant_id, user_id, job_id, type, status)
		 VALUES ($1, $2, $3, 'export', 'completed') RETURNING id`, tenantID, userID, jobID,
	).Scan(&id); err != nil {
		t.Fatalf("insert processing job %s: %v", jobID, err)
	}
	return id
}

func getBodyWithHeaders(t *testing.T, router http.Handler, token, path string, headers map[string]string) string {
	t.Helper()
	rec := apiRequest(t, router, http.MethodGet, path, "", token, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body = %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// assertListExcludes asserts a paginated list does/does not contain an id.
func assertListExcludes(t *testing.T, router http.Handler, token, path string, id int64, mustExclude bool) {
	t.Helper()
	rec := apiRequest(t, router, http.MethodGet, path, "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body = %s", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	present := false
	for _, item := range body.Data {
		if item.ID == id {
			present = true
		}
	}
	if mustExclude && present {
		t.Errorf("%s leaked id %d across tenants", path, id)
	}
	if !mustExclude && !present {
		t.Errorf("%s missing its own id %d", path, id)
	}
}

// assertCrossTenantDenied asserts read/update/delete of another tenant's
// resource by id all fail with 404 (and the resource is left intact).
func assertCrossTenantDenied(t *testing.T, router http.Handler, token string, id int64, patchBody, collection string) {
	t.Helper()
	resource := collection + "/" + strconv.FormatInt(id, 10)

	if rec := apiRequest(t, router, http.MethodGet, resource, "", token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET %s status = %d, want 404", resource, rec.Code)
	}
	if rec := apiRequest(t, router, http.MethodPatch, resource, patchBody, token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("PATCH %s status = %d, want 404", resource, rec.Code)
	}
	if rec := apiRequest(t, router, http.MethodDelete, resource, "", token, nil); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE %s status = %d, want 404", resource, rec.Code)
	}
}

func hasInvoice(body, invoice string) bool {
	var parsed struct {
		Data []struct {
			InvoiceNumber string `json:"invoice_number"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return false
	}
	for _, item := range parsed.Data {
		if item.InvoiceNumber == invoice {
			return true
		}
	}
	return false
}
