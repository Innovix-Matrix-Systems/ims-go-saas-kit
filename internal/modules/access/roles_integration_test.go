package access_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type role struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Managed     bool         `json:"managed"`
	Permissions []permission `json:"permissions"`
}

type permission struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IsSystem bool   `json:"is_system"`
}

func adminRole(t *testing.T, app *testkit.App, admin *testkit.User) role {
	t.Helper()
	var roles []role
	app.As(admin).Get("/api/v1/role").Status(t, http.StatusOK).Data(t, &roles)
	for _, item := range roles {
		if item.Name == "Admin" {
			if !item.Managed {
				t.Fatalf("Admin role managed = false, want true")
			}
			return item
		}
	}
	t.Fatalf("no Admin role in %+v", roles)
	return role{}
}

func TestOrganizationAdminRoleIsManaged(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	managed := adminRole(t, app, admin)
	path := fmt.Sprintf("/api/v1/role/%d", managed.ID)

	app.As(admin).Patch(path, map[string]any{"name": "Owners"}).Status(t, http.StatusForbidden)
	app.As(admin).Delete(path).Status(t, http.StatusForbidden)
	app.As(admin).Post("/api/v1/role/assign-permission", map[string]any{"id": managed.ID, "permissions": []int64{}}).
		Status(t, http.StatusForbidden)

	var detail role
	app.As(admin).Get(path).Status(t, http.StatusOK).Data(t, &detail)
	if len(detail.Permissions) == 0 {
		t.Fatal("Admin role detail lists no permissions")
	}

	// Another role cannot take the managed name.
	var created role
	app.As(admin).Post("/api/v1/role", map[string]any{"name": "Support"}).Status(t, http.StatusCreated).Data(t, &created)
	if created.Managed {
		t.Fatal("a new role is managed")
	}
	app.As(admin).Patch(fmt.Sprintf("/api/v1/role/%d", created.ID), map[string]any{"name": "Admin"}).
		Status(t, http.StatusUnprocessableEntity)
}

func TestRoleEditorGrantsOnlyWhatTheyHold(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	editor := app.User(t, testkit.Role(app.Role(t, "Role editor", "role.view.all", "role.view", "role.update")))

	// The admin can grant every organization permission; look the ids up there.
	var all []permission
	app.As(admin).Get("/api/v1/role/grantable-permissions").Status(t, http.StatusOK).Data(t, &all)
	ids := map[string]int64{}
	for _, item := range all {
		if item.IsSystem {
			t.Fatalf("grantable list includes system permission %s", item.Name)
		}
		ids[item.Name] = item.ID
	}
	if ids["role.update"] == 0 || ids["branch.create"] == 0 {
		t.Fatalf("admin grantable list misses role.update or branch.create: %v", ids)
	}

	var mine []permission
	app.As(editor).Get("/api/v1/role/grantable-permissions").Status(t, http.StatusOK).Data(t, &mine)
	if len(mine) != 3 {
		t.Fatalf("editor grantable = %+v, want the three permissions they hold", mine)
	}

	var target role
	app.As(admin).Post("/api/v1/role", map[string]any{"name": "Clerk"}).Status(t, http.StatusCreated).Data(t, &target)

	// Held: allowed.
	app.As(editor).Post("/api/v1/role/assign-permission", map[string]any{"id": target.ID, "permissions": []int64{ids["role.view"]}}).
		Status(t, http.StatusOK)
	// Not held: refused, and nothing changes.
	app.As(editor).Post("/api/v1/role/assign-permission", map[string]any{"id": target.ID, "permissions": []int64{ids["role.view"], ids["branch.create"]}}).
		Status(t, http.StatusForbidden)

	// The admin adds branch.create; the editor may still remove it, and keep
	// it while changing other permissions.
	app.As(admin).Post("/api/v1/role/assign-permission", map[string]any{"id": target.ID, "permissions": []int64{ids["role.view"], ids["branch.create"]}}).
		Status(t, http.StatusOK)
	app.As(editor).Post("/api/v1/role/assign-permission", map[string]any{"id": target.ID, "permissions": []int64{ids["branch.create"]}}).
		Status(t, http.StatusOK)

	var detail role
	app.As(editor).Get(fmt.Sprintf("/api/v1/role/%d", target.ID)).Status(t, http.StatusOK).Data(t, &detail)
	if len(detail.Permissions) != 1 || detail.Permissions[0].Name != "branch.create" {
		t.Fatalf("role permissions = %+v, want [branch.create]", detail.Permissions)
	}
}
