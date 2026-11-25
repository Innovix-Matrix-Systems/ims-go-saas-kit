package branches_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

func TestBranchDeleteKeepsMembersSomewhere(t *testing.T) {
	app := testkit.New(t)
	tenantID := app.Tenant(t, "Branch Co")
	admin := app.User(t, testkit.InTenant(tenantID))
	home := app.DefaultBranch(t, tenantID)

	// The admin belongs to the only branch.
	app.As(admin).Delete(fmt.Sprintf("/api/v1/branch/%d", home)).Status(t, http.StatusConflict)

	var second struct {
		ID int64 `json:"id"`
	}
	app.As(admin).Post("/api/v1/branch", map[string]any{"name": "Second"}).Status(t, http.StatusCreated).Data(t, &second)

	// A member belongs to it: refused.
	member := app.User(t, testkit.InTenant(tenantID))
	if _, err := app.Pool.Exec(context.Background(), `UPDATE users SET branch_id = $1 WHERE id = $2`, second.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	app.As(admin).Delete(fmt.Sprintf("/api/v1/branch/%d", second.ID)).Status(t, http.StatusConflict)

	// Nobody belongs to it, but the admin switched into it: deleted, and the
	// admin is back in their own branch.
	if _, err := app.Pool.Exec(context.Background(), `UPDATE users SET branch_id = $1 WHERE id = $2`, home, member.ID); err != nil {
		t.Fatal(err)
	}
	app.As(admin).Patch(fmt.Sprintf("/api/v1/user/switch-branch/%d", second.ID), nil).Status(t, http.StatusOK)
	app.As(admin).Delete(fmt.Sprintf("/api/v1/branch/%d", second.ID)).Status(t, http.StatusNoContent)

	var active *int64
	if err := app.Pool.QueryRow(context.Background(), `SELECT active_branch_id FROM users WHERE id = $1`, admin.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("active_branch_id = %d after its branch was deleted, want NULL", *active)
	}
}
