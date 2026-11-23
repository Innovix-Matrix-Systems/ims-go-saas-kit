package authz

import "testing"

func TestAdminTenantPermissionsComposition(t *testing.T) {
	permissions := AdminTenantPermissions()
	if len(permissions) == 0 {
		t.Fatal("AdminTenantPermissions() is empty")
	}

	byName := make(map[string]Permission, len(permissions))
	for _, permission := range permissions {
		byName[permission.Name] = permission
	}

	mustHave := []string{
		PermRoleView, PermRoleViewAll, PermRoleCreate, PermRoleUpdate, PermRoleDelete,
		PermBranchView, PermBranchViewAll, PermBranchCreate, PermBranchUpdate, PermBranchDelete,
		PermUserView, PermUserViewAll, PermUserCreate, PermUserUpdate, PermUserDelete,
		PermUserRoleAssign, PermUserBranchSwitch, PermActivityView,
	}
	for _, name := range mustHave {
		if _, ok := byName[name]; !ok {
			t.Errorf("admin permissions missing %q", name)
		}
	}

	mustNotHave := []string{
		PermRoleInternalCreate,
		PermRoleInternalUpdate,
		PermUserInternalCreate,
		PermUserTenantSwitch,
		PermTenantView,
		PermTenantCreate,
	}
	for _, name := range mustNotHave {
		if _, ok := byName[name]; ok {
			t.Errorf("admin permissions unexpectedly contain %q", name)
		}
	}

	for _, permission := range permissions {
		if permission.IsSystem {
			t.Errorf("admin permissions contain system permission %q", permission.Name)
		}
	}
}

func TestCatalogUniqueNames(t *testing.T) {
	seen := map[string]struct{}{}
	for _, permission := range Catalog {
		if _, exists := seen[permission.Name]; exists {
			t.Errorf("duplicate catalog permission %q", permission.Name)
		}
		seen[permission.Name] = struct{}{}
	}
}
