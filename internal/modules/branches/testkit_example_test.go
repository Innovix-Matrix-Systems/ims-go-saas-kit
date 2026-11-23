package branches_test

import (
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestBranchCreate is the testkit example from the developer guide.
func TestBranchCreate(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	var branch struct {
		Name string `json:"name"`
	}
	app.As(admin).Post("/api/v1/branch", `{"name":"Dhaka"}`).Status(t, http.StatusCreated).Data(t, &branch)
	if branch.Name != "Dhaka" {
		t.Fatalf("name = %q", branch.Name)
	}
	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer", "branch.view.all")))
	app.As(viewer).Post("/api/v1/branch", `{"name":"Sylhet"}`).Status(t, http.StatusForbidden)
}

// TestBranchToolsForAgents is the MCP example: an operation marked x-mcp is
// tested as a tool, next to its REST test, with the same rules.
func TestBranchToolsForAgents(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	admin := app.User(t)
	agent := app.Agent(t, admin, testkit.AllowChanges())

	var branch struct {
		Name     string `json:"name"`
		TenantID int64  `json:"tenant_id"`
	}
	agent.Call(t, "create_branch", map[string]any{"body": map[string]any{"name": "Khulna"}}).OK(t).Data(t, &branch)
	if branch.Name != "Khulna" || branch.TenantID != admin.TenantID {
		t.Fatalf("branch = %+v", branch)
	}
	agent.Call(t, "create_branch", map[string]any{"body": map[string]any{}}).Refused(t, "VALIDATION_FAILED")

	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer", "branch.view.all")))
	app.Agent(t, viewer, testkit.AllowChanges()).
		Call(t, "create_branch", map[string]any{"body": map[string]any{"name": "Sylhet"}}).
		Refused(t, "INSUFFICIENT_PERMISSIONS")
}
