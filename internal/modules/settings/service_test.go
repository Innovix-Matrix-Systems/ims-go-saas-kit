package settings

import (
	"context"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	tenantsrepo "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

func TestServiceResolutionAndSync(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"settings", "settings_groups", "personal_access_tokens", "role_has_permissions",
		"model_has_roles", "users", "roles", "permissions", "branches", "tenants",
	)

	store := memory.New("settings-")
	defer store.Close()
	service := NewService(NewRepository(queries), store)
	ctx := context.Background()
	scope := tenancy.Scope{Internal: true}

	// Defaults resolve before any rows exist.
	name, err := service.Get(ctx, scope, "system.name")
	if err != nil || name != "IMS Go API" {
		t.Fatalf("Get default = %v, %v", name, err)
	}

	// Sync creates global rows and groups; running twice is idempotent.
	created, err := service.Sync(ctx)
	if err != nil || created != len(Catalog) {
		t.Fatalf("Sync() = %d, %v; want %d", created, err, len(Catalog))
	}
	created, err = service.Sync(ctx)
	if err != nil || created != 0 {
		t.Fatalf("second Sync() = %d, %v; want 0", created, err)
	}
	groups, err := service.repo.ListGroups(ctx)
	if err != nil || len(groups) != len(Groups) {
		t.Fatalf("ListGroups() = %d, %v; want %d", len(groups), err, len(Groups))
	}

	// Global update is visible and cached values are invalidated.
	if err := service.Set(ctx, scope, "system.name", "Acme Platform"); err != nil {
		t.Fatalf("Set(global) error = %v", err)
	}
	name, err = service.Get(ctx, scope, "system.name")
	if err != nil || name != "Acme Platform" {
		t.Fatalf("Get after set = %v, %v", name, err)
	}

	// Tenant override wins over the global row for a tenant-scoped key.
	tenantID := createTestTenant(t, queries, "settings-test-tenant")
	tenantScope := tenancy.Scope{TenantID: tenantID, BranchID: 1}
	if err := service.Set(ctx, tenantScope, "notification.enabled", false); err != nil {
		t.Fatalf("Set(tenant) error = %v", err)
	}
	if enabled, err := service.GetBool(ctx, tenantScope, "notification.enabled"); err != nil || enabled {
		t.Fatalf("tenant override = %v, %v; want false", enabled, err)
	}

	// A different tenant still sees the global default.
	otherScope := tenancy.Scope{TenantID: tenantID + 100, BranchID: 1}
	if enabled, err := service.GetBool(ctx, otherScope, "notification.enabled"); err != nil || !enabled {
		t.Fatalf("other tenant = %v, %v; want true", enabled, err)
	}

	// A GlobalOnly key rejects tenant writes.
	if err := service.Set(ctx, tenantScope, "system.name", "hijack"); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("Set(GlobalOnly tenant) error = %v; want VALIDATION_FAILED", err)
	}

	// Unknown keys are not found.
	if _, err := service.Get(ctx, scope, "missing.key"); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("Get(unknown) error = %v; want RESOURCE_NOT_FOUND", err)
	}

	// Effective settings carry group metadata and the resolution source.
	effective, err := service.EffectiveSettings(ctx, tenantScope)
	if err != nil {
		t.Fatalf("EffectiveSettings: %v", err)
	}
	var found bool
	for _, item := range effective {
		if item.Key.Name == "notification.enabled" {
			found = true
			if item.Source != "tenant" || item.Group.Slug != "notifications" {
				t.Fatalf("effective notification.enabled = source %q group %q", item.Source, item.Group.Slug)
			}
		}
	}
	if !found {
		t.Fatal("EffectiveSettings did not include notification.enabled")
	}

	// Deleting the override falls back to the global default.
	if err := service.DeleteTarget(ctx, "notification.enabled", Target{Type: TargetTenant, ID: tenantID}); err != nil {
		t.Fatalf("DeleteTarget: %v", err)
	}
	if enabled, err := service.GetBool(ctx, tenantScope, "notification.enabled"); err != nil || !enabled {
		t.Fatalf("after delete = %v, %v; want true", enabled, err)
	}

	// Typed validation and accessors.
	if err := service.Set(ctx, scope, "data_processing_job.cleanup_days", "thirty"); err == nil {
		t.Fatal("Set(string into int) = nil error")
	}
	days, err := service.GetInt(ctx, scope, "data_processing_job.cleanup_days")
	if err != nil || days != 30 {
		t.Fatalf("GetInt default = %d, %v", days, err)
	}
	if err := service.Set(ctx, scope, "data_processing_job.cleanup_days", 7); err != nil {
		t.Fatalf("Set(int) error = %v", err)
	}
	if days, err = service.GetInt(ctx, scope, "data_processing_job.cleanup_days"); err != nil || days != 7 {
		t.Fatalf("GetInt after set = %d, %v", days, err)
	}
}

func TestTenantIsolation(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"settings", "settings_groups", "personal_access_tokens", "role_has_permissions",
		"model_has_roles", "users", "roles", "permissions", "branches", "tenants",
	)
	store := memory.New("settings-iso-")
	defer store.Close()
	service := NewService(NewRepository(queries), store)
	ctx := context.Background()

	tenantA := createTestTenant(t, queries, "settings-iso-a")
	tenantB := createTestTenant(t, queries, "settings-iso-b")
	if tenantA == tenantB {
		t.Fatal("expected distinct tenants")
	}

	if err := service.Set(ctx, tenancy.Scope{TenantID: tenantA, BranchID: 1}, "notification.enabled", false); err != nil {
		t.Fatalf("set tenant A override: %v", err)
	}

	// Tenant B is unaffected by tenant A's override.
	if enabled, err := service.GetBool(ctx, tenancy.Scope{TenantID: tenantB, BranchID: 1}, "notification.enabled"); err != nil || !enabled {
		t.Fatalf("tenant B = %v, %v; want default true", enabled, err)
	}

	// Deleting tenant B's (nonexistent) override is a not-found, not a write.
	err := service.DeleteTarget(ctx, "notification.enabled", Target{Type: TargetTenant, ID: tenantB})
	if !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("DeleteTarget(tenant B) error = %v; want RESOURCE_NOT_FOUND", err)
	}
	// Tenant A's override is still intact.
	if enabled, err := service.GetBool(ctx, tenancy.Scope{TenantID: tenantA, BranchID: 1}, "notification.enabled"); err != nil || enabled {
		t.Fatalf("tenant A after cross delete = %v, %v; want false", enabled, err)
	}
}

func TestSettingsGroupCRUD(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"settings", "settings_groups", "personal_access_tokens", "role_has_permissions",
		"model_has_roles", "users", "roles", "permissions", "branches", "tenants",
	)
	store := memory.New("settings-groups-")
	defer store.Close()
	service := NewService(NewRepository(queries), store)
	ctx := context.Background()

	if _, err := service.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Create a custom group; the slug is derived from the name.
	group, err := service.CreateGroup(ctx, 0, GroupInput{Name: "Reports"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if group.Slug != "reports" || group.IsSystem {
		t.Fatalf("created group = %+v", group)
	}

	// Duplicate slug -> validation error.
	if _, err := service.CreateGroup(ctx, 0, GroupInput{Name: "reports"}); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("duplicate create error = %v; want VALIDATION_FAILED", err)
	}

	// Assign a catalog key to the new group; the effective group follows.
	assigned, err := service.AssignKeyGroup(ctx, "notification.enabled", &group.ID)
	if err != nil {
		t.Fatalf("AssignKeyGroup: %v", err)
	}
	if assigned.Group.Slug != "reports" {
		t.Fatalf("assigned group = %q; want reports", assigned.Group.Slug)
	}

	// Update re-derives the slug; omitted sort_order keeps the existing value.
	updated, err := service.UpdateGroup(ctx, 0, group.ID, GroupInput{Name: "Reports & Exports"})
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if updated.Slug != "reports-exports" || updated.SortOrder != group.SortOrder {
		t.Fatalf("updated group = %+v", updated)
	}

	// Paginated + lightweight lists.
	items, total, err := service.ListGroups(ctx, GroupListFilter{OrderBy: "sort_order", OrderDir: "asc", Limit: 50})
	if err != nil || total < int64(len(Groups))+1 || len(items) != int(total) {
		t.Fatalf("ListGroups = %d items, total %d, err %v", len(items), total, err)
	}
	light, err := service.ListGroupsLight(ctx, "name", "asc")
	if err != nil || len(light) != int(total) {
		t.Fatalf("ListGroupsLight = %d, err %v; want %d", len(light), err, total)
	}

	// Deleting the group clears the assignment; the key falls back to its
	// catalog group.
	if err := service.DeleteGroup(ctx, group.ID); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	effective, err := service.EffectiveForTargetKey(ctx, Target{Type: TargetGlobal}, "notification.enabled")
	if err != nil {
		t.Fatalf("EffectiveForTargetKey: %v", err)
	}
	if effective.Group.Slug != "notifications" {
		t.Fatalf("fallback group = %q; want notifications", effective.Group.Slug)
	}

	// Missing group / unknown key / unknown assignment target.
	if _, err := service.GetGroup(ctx, 999999); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("GetGroup(missing) error = %v; want RESOURCE_NOT_FOUND", err)
	}
	if _, err := service.AssignKeyGroup(ctx, "missing.key", nil); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("AssignKeyGroup(unknown key) error = %v; want RESOURCE_NOT_FOUND", err)
	}
	missing := int64(999999)
	if _, err := service.AssignKeyGroup(ctx, "notification.enabled", &missing); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("AssignKeyGroup(unknown group) error = %v; want RESOURCE_NOT_FOUND", err)
	}
}

func createTestTenant(t *testing.T, queries *dbgen.Queries, code string) int64 {
	t.Helper()
	repo := tenantsrepo.NewRepository(queries)
	tenant, err := repo.Create(context.Background(), tenantsrepo.CreateInput{
		Name:     code,
		Code:     code,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return tenant.ID
}
