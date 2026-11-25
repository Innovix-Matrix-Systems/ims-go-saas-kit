package identity_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// memberIDs lists the ids the member list returns to the user.
func memberIDs(t *testing.T, app *testkit.App, user *testkit.User) map[int64]bool {
	t.Helper()
	var rows []struct {
		ID int64 `json:"id"`
	}
	app.As(user).Get("/api/v1/user?per_page=100").Status(t, http.StatusOK).Data(t, &rows)
	ids := map[int64]bool{}
	for _, row := range rows {
		ids[row.ID] = true
	}
	return ids
}

func TestMemberListFollowsTheBranch(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	tenantID := app.Tenant(t, "Branch Scope Co")
	home := app.DefaultBranch(t, tenantID)
	admin := app.User(t, testkit.InTenant(tenantID))
	var second struct {
		ID int64 `json:"id"`
	}
	app.As(admin).Post("/api/v1/branch", map[string]any{"name": "Second"}).Status(t, http.StatusCreated).Data(t, &second)
	member := app.User(t, testkit.InTenant(tenantID))
	if _, err := app.Pool.Exec(ctx, `UPDATE users SET branch_id = $1 WHERE id = $2`, second.ID, member.ID); err != nil {
		t.Fatal(err)
	}

	// An admin who has not picked a branch sees every branch.
	if ids := memberIDs(t, app, admin); !ids[admin.ID] || !ids[member.ID] {
		t.Fatalf("all branches: got %v, want the admin and the member", ids)
	}

	// Switched to the second branch: its members only.
	app.As(admin).Patch(fmt.Sprintf("/api/v1/user/switch-branch/%d", second.ID), nil).Status(t, http.StatusOK)
	if ids := memberIDs(t, app, admin); ids[admin.ID] || !ids[member.ID] {
		t.Fatalf("second branch: got %v, want the member only", ids)
	}

	// Switched to the home branch: not the second branch's member.
	app.As(admin).Patch(fmt.Sprintf("/api/v1/user/switch-branch/%d", home), nil).Status(t, http.StatusOK)
	if ids := memberIDs(t, app, admin); !ids[admin.ID] || ids[member.ID] {
		t.Fatalf("home branch: got %v, want the admin, not the member", ids)
	}

	// Back to every branch.
	var cleared struct {
		ActiveBranchID *int64 `json:"active_branch_id"`
	}
	app.As(admin).Delete("/api/v1/user/switch-branch").Status(t, http.StatusOK).Data(t, &cleared)
	if cleared.ActiveBranchID != nil {
		t.Fatalf("active_branch_id = %d, want none", *cleared.ActiveBranchID)
	}
	if ids := memberIDs(t, app, admin); !ids[admin.ID] || !ids[member.ID] {
		t.Fatalf("back to all branches: got %v, want both", ids)
	}

	// Staff acting in one branch see that branch's members.
	var acting []struct {
		ID int64 `json:"id"`
	}
	app.As(app.SuperAdmin(t)).InTenant(tenantID, second.ID).Get("/api/v1/user?per_page=100").Status(t, http.StatusOK).Data(t, &acting)
	for _, row := range acting {
		if row.ID != member.ID {
			t.Fatalf("acting in the second branch listed user %d", row.ID)
		}
	}
}

func TestMemberListOfSomeoneWhoCannotSwitch(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	admin := app.User(t)
	viewer := app.User(t, testkit.Role(app.Role(t, "Member Viewer", "user.view", "user.view.all")))
	var second struct {
		ID int64 `json:"id"`
	}
	app.As(admin).Post("/api/v1/branch", map[string]any{"name": "Elsewhere"}).Status(t, http.StatusCreated).Data(t, &second)
	other := app.User(t)
	if _, err := app.Pool.Exec(ctx, `UPDATE users SET branch_id = $1 WHERE id = $2`, second.ID, other.ID); err != nil {
		t.Fatal(err)
	}

	// Without branch.view.all and user.branch.switch: their own branch only.
	if ids := memberIDs(t, app, viewer); !ids[viewer.ID] || ids[other.ID] {
		t.Fatalf("got %v, want their own branch only", ids)
	}
	app.As(viewer).Delete("/api/v1/user/switch-branch").Status(t, http.StatusForbidden)
}
