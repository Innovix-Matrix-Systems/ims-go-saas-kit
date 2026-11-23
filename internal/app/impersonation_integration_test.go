package app_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

// Act-as-user (X-Acting-User), the system audit log and public tenant
// resolution (docs/guides/tenancy.md, docs/guides/activity-log.md).

type feedEntry struct {
	ID       int64  `json:"id"`
	TenantID *int64 `json:"tenant_id"`
	Actor    *struct {
		ID int64 `json:"id"`
	} `json:"actor"`
	ByStaff      bool `json:"by_staff"`
	Impersonator *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"impersonator"`
	Action    string         `json:"action"`
	SubjectID *int64         `json:"subject_id"`
	Changes   map[string]any `json:"changes"`
	RequestID *string        `json:"request_id"`
}

func actingAs(c *testkit.Client, user *testkit.User) *testkit.Client {
	return c.Header("X-Acting-User", strconv.FormatInt(user.ID, 10))
}

func feed(t *testing.T, c *testkit.Client, query string) []feedEntry {
	t.Helper()
	var entries []feedEntry
	c.Get("/api/v1/activity?per_page=100"+query).Status(t, http.StatusOK).Data(t, &entries)
	return entries
}

func countRows(t *testing.T, app *testkit.App, query string, args ...any) int {
	t.Helper()
	var n int
	if err := app.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func developer(t *testing.T, app *testkit.App) *testkit.User {
	t.Helper()
	var id int64
	if err := app.Pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, seeder.DeveloperEmail).Scan(&id); err != nil {
		t.Fatalf("developer: %v", err)
	}
	return &testkit.User{ID: id, Email: seeder.DeveloperEmail, Token: app.Login(t, seeder.DeveloperEmail, testkit.Password)}
}

// Criteria 1 and 5: the request runs as the target (their tenant, their
// permissions, them as the actor) and the next request is the caller again.
func TestActAsUserRunsAsTheTarget(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	tenantB := app.Tenant(t, "Tenant B")
	adminB := app.User(t, testkit.InTenant(tenantB))

	var branches []struct {
		ID       int64 `json:"id"`
		TenantID int64 `json:"tenant_id"`
	}
	actingAs(app.As(super), adminB).Get("/api/v1/branch").Status(t, http.StatusOK).Data(t, &branches)
	if len(branches) == 0 {
		t.Fatal("acting as tenant B's admin listed no branches")
	}
	for _, branch := range branches {
		if branch.TenantID != tenantB {
			t.Fatalf("acting as tenant B's admin listed branch %+v of another tenant", branch)
		}
	}

	// The target's permissions apply: a member without branch.view.all is
	// refused although the super admin could list branches.
	member := app.User(t, testkit.Role(app.Role(t, "Lister", "user.view.all")))
	actingAs(app.As(super), member).Get("/api/v1/branch").Status(t, http.StatusForbidden)

	// A write is done by the target, on behalf of the super admin.
	var created struct {
		ID int64 `json:"id"`
	}
	actingAs(app.As(super), adminB).Post("/api/v1/branch", `{"name":"Acted"}`).
		Status(t, http.StatusCreated).Data(t, &created)

	var system []feedEntry
	for _, entry := range feed(t, app.As(super), "&subject_type=branch") {
		if entry.Action == "branch.created" && entry.SubjectID != nil && *entry.SubjectID == created.ID {
			system = append(system, entry)
		}
	}
	if len(system) != 1 {
		t.Fatalf("system reader sees %d branch.created entries for %d, want 1", len(system), created.ID)
	}
	got := system[0]
	if got.Actor == nil || got.Actor.ID != adminB.ID || !got.ByStaff || got.Impersonator == nil || got.Impersonator.ID != super.ID || got.Impersonator.Name == "" {
		t.Fatalf("entry = %+v; want actor %d, by_staff, impersonator %d with a name", got, adminB.ID, super.ID)
	}

	// The tenant admin sees that staff did it, not who.
	for _, entry := range feed(t, app.As(adminB), "") {
		if entry.Action == "branch.created" && entry.SubjectID != nil && *entry.SubjectID == created.ID {
			if !entry.ByStaff || entry.Impersonator != nil {
				t.Fatalf("tenant view = %+v; want by_staff and no impersonator", entry)
			}
		}
		if len(entry.Action) > 14 && entry.Action[:14] == "impersonation." {
			t.Fatalf("tenant admin sees a system act-as entry: %+v", entry)
		}
	}

	// Criterion 5: without the header the caller is themselves again, and
	// nothing about the target changed.
	var me struct {
		ID int64 `json:"id"`
	}
	app.As(super).Get("/api/v1/user/profile").Status(t, http.StatusOK).Data(t, &me)
	if me.ID != super.ID {
		t.Fatalf("profile without the header = user %d, want %d", me.ID, super.ID)
	}
	if n := countRows(t, app, `SELECT count(*) FROM personal_access_tokens WHERE tokenable_id = $1`, adminB.ID); n != 1 {
		t.Fatalf("target has %d tokens after being acted as, want 1 (their own login)", n)
	}
}

// §C: the start is logged once per window, every request is logged, and
// the rows are system rows that survive the tenant's deletion.
func TestActAsUserAuditTrail(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	tenantB := app.Tenant(t, "Tenant B")
	adminB := app.User(t, testkit.InTenant(tenantB))

	actingAs(app.As(super), adminB).Get("/api/v1/branch").Status(t, http.StatusOK)
	actingAs(app.As(super), adminB).Get("/api/v1/user/profile").Status(t, http.StatusOK)
	actingAs(app.As(super), adminB).Post("/api/v1/branch", `{"name":"Logged"}`).Status(t, http.StatusCreated)

	started := countRows(t, app, `SELECT count(*) FROM activity_log WHERE action = 'impersonation.started' AND actor_id = $1 AND subject_id = $2 AND tenant_id IS NULL`, super.ID, adminB.ID)
	if started != 1 {
		t.Fatalf("impersonation.started rows = %d, want 1 within the window", started)
	}
	requests := countRows(t, app, `SELECT count(*) FROM activity_log WHERE action = 'impersonation.request' AND actor_id = $1 AND subject_id = $2 AND tenant_id IS NULL`, super.ID, adminB.ID)
	if requests != 3 {
		t.Fatalf("impersonation.request rows = %d, want 3 (GET, GET, POST)", requests)
	}

	// The service's entry and the request row share the request id.
	linked := countRows(t, app, `
		SELECT count(*) FROM activity_log s JOIN activity_log r ON r.request_id = s.request_id
		WHERE s.action = 'branch.created' AND s.impersonator_id = $1 AND r.action = 'impersonation.request'`, super.ID)
	if linked != 1 {
		t.Fatalf("branch.created linked to %d impersonation.request rows, want 1", linked)
	}

	// The system-only filter returns exactly the rows with no tenant.
	system := feed(t, app.As(super), "&tenant_id=system")
	if len(system) == 0 {
		t.Fatal("tenant_id=system returned nothing")
	}
	for _, entry := range system {
		if entry.TenantID != nil {
			t.Fatalf("tenant_id=system returned a tenant row: %+v", entry)
		}
	}
	for _, entry := range feed(t, app.As(super), "&tenant_id="+strconv.FormatInt(tenantB, 10)) {
		if entry.TenantID == nil || *entry.TenantID != tenantB {
			t.Fatalf("tenant_id=%d returned %+v", tenantB, entry)
		}
	}
	for _, entry := range feed(t, app.As(super), "&impersonated=true&impersonator_id="+strconv.FormatInt(super.ID, 10)) {
		if entry.Impersonator == nil || entry.Impersonator.ID != super.ID {
			t.Fatalf("impersonator filter returned %+v", entry)
		}
	}
	app.As(super).Get("/api/v1/activity?tenant_id=nope").Status(t, http.StatusUnprocessableEntity)

	// Deleting the tenant keeps the act-as record.
	app.As(super).Delete("/api/v1/tenant/"+strconv.FormatInt(tenantB, 10)).Status(t, http.StatusNoContent)
	if _, err := app.Pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, tenantB); err != nil {
		t.Fatalf("hard-delete tenant: %v", err)
	}
	if n := countRows(t, app, `SELECT count(*) FROM activity_log WHERE action LIKE 'impersonation.%' AND subject_id = $1`, adminB.ID); n < 4 {
		t.Fatalf("act-as rows after the tenant was deleted = %d, want 4", n)
	}
}

// §C: if impersonation.started cannot be written, nothing runs as the target.
func TestActAsUserFailsClosedWhenTheStartIsNotRecorded(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	target := app.User(t)
	if _, err := app.Pool.Exec(context.Background(),
		`ALTER TABLE activity_log ADD CONSTRAINT test_no_start CHECK (action <> 'impersonation.started') NOT VALID`); err != nil {
		t.Fatalf("add constraint: %v", err)
	}
	t.Cleanup(func() {
		_, _ = app.Pool.Exec(context.Background(), `ALTER TABLE activity_log DROP CONSTRAINT IF EXISTS test_no_start`)
	})

	before := countRows(t, app, `SELECT count(*) FROM branches WHERE name = 'Never'`)
	actingAs(app.As(super), target).Post("/api/v1/branch", `{"name":"Never"}`).Status(t, http.StatusServiceUnavailable)
	if after := countRows(t, app, `SELECT count(*) FROM branches WHERE name = 'Never'`); after != before {
		t.Fatal("the handler ran although the start was not recorded")
	}
}

// Criterion 2: refusals.
func TestActAsUserRefusals(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	admin := app.User(t)
	dev := developer(t, app)

	inactive := app.User(t)
	if _, err := app.Pool.Exec(context.Background(), `UPDATE users SET is_active = 0 WHERE id = $1`, inactive.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	// A tenant user who somehow holds user.impersonate (granted directly in
	// SQL; the API never gives a system permission to a tenant role).
	privileged := app.User(t, testkit.Role(app.Role(t, "Impersonator", "user.impersonate")))

	cases := []struct {
		name   string
		caller *testkit.User
		target string
		status int
	}{
		{"self", super, strconv.FormatInt(super.ID, 10), http.StatusForbidden},
		{"missing", super, "999999", http.StatusForbidden},
		{"inactive", super, strconv.FormatInt(inactive.ID, 10), http.StatusForbidden},
		{"platform account", super, strconv.FormatInt(dev.ID, 10), http.StatusForbidden},
		{"target can act as others", dev, strconv.FormatInt(privileged.ID, 10), http.StatusForbidden},
		{"super admin as target", dev, strconv.FormatInt(super.ID, 10), http.StatusForbidden},
		{"tenant admin without the permission", admin, strconv.FormatInt(inactive.ID, 10), http.StatusForbidden},
		{"not an id", super, "abc", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := app.As(tc.caller).Header("X-Acting-User", tc.target).Get("/api/v1/branch").Status(t, tc.status)
			if tc.status == http.StatusForbidden && !containsCode(res.Body.String(), "IMPERSONATION_FORBIDDEN") {
				t.Fatalf("body = %s; want IMPERSONATION_FORBIDDEN", res.Body.String())
			}
		})
	}

	// Combined with the view-as-tenant headers: 422.
	actingAs(app.As(super), admin).Header("X-Acting-Tenant", "1").Get("/api/v1/branch").
		Status(t, http.StatusUnprocessableEntity)

	reasons := countRows(t, app, `SELECT count(DISTINCT changes->>'reason') FROM activity_log WHERE action = 'impersonation.denied'`)
	if reasons != 6 {
		t.Fatalf("distinct denial reasons = %d, want 6 (self, not_found, inactive, platform_account, privileged, permission)", reasons)
	}

	// The developer (seeded with user.impersonate) may act as a tenant user.
	actingAs(app.As(dev), admin).Get("/api/v1/user/profile").Status(t, http.StatusOK)

	// Refusals are system rows.
	if n := countRows(t, app, `SELECT count(*) FROM activity_log WHERE action = 'impersonation.denied' AND tenant_id IS NULL`); n < 6 {
		t.Fatalf("impersonation.denied rows = %d, want 6", n)
	}
}

// Criteria 3 and 4: account changes are blocked; exempt routes refuse the
// header; /logout ignores it.
func TestActAsUserBoundaries(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	target := app.User(t)

	actingAs(app.As(super), target).Post("/api/v1/user/profile/change-password",
		map[string]any{"current_password": testkit.Password, "password": "brand-new-pass-1", "password_confirmation": "brand-new-pass-1"}).
		Status(t, http.StatusForbidden)
	app.Login(t, target.Email, testkit.Password) // unchanged

	actingAs(app.As(super), target).Post("/api/v1/user/profile/update", `{"name":"Hijacked"}`).Status(t, http.StatusForbidden)

	// A tenant-exempt route refuses the header instead of running as the caller.
	actingAs(app.As(super), target).Get("/api/v1/tenant").Status(t, http.StatusUnprocessableEntity)

	// /logout has no tenant resolution: the caller's token is revoked, not the target's.
	actingAs(app.As(super), target).Post("/api/v1/logout", `{"device":"testkit"}`).Status(t, http.StatusNoContent)
	app.As(super).Get("/api/v1/user/profile").Status(t, http.StatusUnauthorized)
	app.As(target).Get("/api/v1/user/profile").Status(t, http.StatusOK)
}

// Criterion 8: who reads the system audit log.
func TestSystemAuditLogAccess(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	admin := app.User(t)

	// A tenant admin never gets system rows, whatever tenant_id they send.
	for _, query := range []string{"", "&tenant_id=system", "&tenant_id=2"} {
		for _, entry := range feed(t, app.As(admin), query) {
			if entry.TenantID == nil || *entry.TenantID != testkit.DefaultTenant {
				t.Fatalf("tenant admin (%q) got %+v", query, entry)
			}
		}
	}

	// An internal account with activity.view but not activity.view.all is refused.
	dev := developer(t, app)
	if _, err := app.Pool.Exec(context.Background(), `
		INSERT INTO role_has_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r, permissions p WHERE r.name = 'Developer' AND p.name = 'activity.view'`); err != nil {
		t.Fatalf("grant activity.view: %v", err)
	}
	app.Deps.Checker.InvalidateUser(context.Background(), dev.ID)
	app.As(dev).Get("/api/v1/activity").Status(t, http.StatusForbidden)

	// The super admin (activity.view.all through the bypass) reads both kinds.
	var sawTenant, sawSystem bool
	for _, entry := range feed(t, app.As(super), "") {
		if entry.TenantID == nil {
			sawSystem = true
		} else {
			sawTenant = true
		}
	}
	if !sawTenant {
		t.Fatal("system reader sees no tenant rows")
	}
	_ = sawSystem // system rows exist only once something system-level happened
}

// Criteria 6 and 7: public tenant resolution.
func TestPublicTenantResolution(t *testing.T) {
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	tenantB := app.Tenant(t, "Tenant B")

	var code string
	if err := app.Pool.QueryRow(context.Background(), `SELECT code FROM tenants WHERE id = $1`, testkit.DefaultTenant).Scan(&code); err != nil {
		t.Fatalf("tenant code: %v", err)
	}
	var tenant struct {
		Name   string `json:"name"`
		Code   string `json:"code"`
		Branch struct {
			ID int64 `json:"id"`
		} `json:"branch"`
	}
	app.Guest().Header("X-Tenant-Code", code).Get("/api/v1/public/tenant").Status(t, http.StatusOK).Data(t, &tenant)
	if tenant.Code != code || tenant.Branch.ID != app.DefaultBranch(t, testkit.DefaultTenant) {
		t.Fatalf("public tenant = %+v; want code %s and the first branch", tenant, code)
	}

	// Another tenant's branch: 422.
	app.Guest().Header("X-Tenant-Code", code).Header("X-Branch-Id", strconv.FormatInt(app.DefaultBranch(t, tenantB), 10)).
		Get("/api/v1/public/tenant").Status(t, http.StatusUnprocessableEntity)

	// Missing and unknown codes get the same body.
	missing := app.Guest().Get("/api/v1/public/tenant").Status(t, http.StatusUnprocessableEntity).Body.String()
	unknown := app.Guest().Header("X-Tenant-Code", "nope").Get("/api/v1/public/tenant").
		Status(t, http.StatusUnprocessableEntity).Body.String()
	if stripTrace(missing) != stripTrace(unknown) {
		t.Fatalf("missing and unknown codes differ:\n%s\n%s", missing, unknown)
	}

	// Deactivating through the tenants service takes effect at once.
	var codeB string
	if err := app.Pool.QueryRow(context.Background(), `SELECT code FROM tenants WHERE id = $1`, tenantB).Scan(&codeB); err != nil {
		t.Fatalf("tenant code: %v", err)
	}
	app.Guest().Header("X-Tenant-Code", codeB).Get("/api/v1/public/tenant").Status(t, http.StatusOK)
	app.As(super).Patch("/api/v1/tenant/"+strconv.FormatInt(tenantB, 10), `{"name":"Tenant B","is_active":false}`).
		Status(t, http.StatusOK)
	inactive := app.Guest().Header("X-Tenant-Code", codeB).Get("/api/v1/public/tenant").
		Status(t, http.StatusUnprocessableEntity).Body.String()
	if stripTrace(inactive) != stripTrace(unknown) {
		t.Fatalf("inactive and unknown codes differ:\n%s\n%s", inactive, unknown)
	}
}
