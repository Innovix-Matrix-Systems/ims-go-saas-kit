package notifications_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func feedIDs(t *testing.T, app *testkit.App, user *testkit.User) map[int64]bool {
	t.Helper()
	var rows []struct {
		ID int64 `json:"id"`
	}
	app.As(user).Get("/api/v1/notification/feed?per_page=100").Status(t, http.StatusOK).Data(t, &rows)
	ids := map[int64]bool{}
	for _, row := range rows {
		ids[row.ID] = true
	}
	return ids
}

func TestFeedFollowsTheBranch(t *testing.T) {
	app := testkit.New(t)
	tenantID := app.Tenant(t, "Branch Feed Co")
	home := app.DefaultBranch(t, tenantID)
	admin := app.User(t, testkit.InTenant(tenantID))
	var second struct {
		ID int64 `json:"id"`
	}
	app.As(admin).Post("/api/v1/branch", map[string]any{"name": "Second"}).Status(t, http.StatusCreated).Data(t, &second)
	clerk := app.User(t, testkit.InTenant(tenantID), testkit.Role(roleWithout(t, app, tenantID)))

	notify := func(branch int64, title string) int64 {
		var created struct {
			ID int64 `json:"id"`
		}
		app.As(app.SuperAdmin(t)).Post("/api/v1/internal/notification", map[string]any{
			"event_type": "platform.announcement", "priority": "INFO", "title": title, "tenant_id": tenantID,
			"targets": []map[string]any{{"type": "BRANCH", "id": branch}},
		}).Status(t, http.StatusCreated).Data(t, &created)
		return created.ID
	}
	homeNotice, secondNotice := notify(home, "Home branch"), notify(second.ID, "Second branch")

	// Every branch until the admin picks one.
	if ids := feedIDs(t, app, admin); !ids[homeNotice] || !ids[secondNotice] {
		t.Fatalf("all branches: got %v, want both notices", ids)
	}
	app.As(admin).Patch(fmt.Sprintf("/api/v1/user/switch-branch/%d", second.ID), nil).Status(t, http.StatusOK)
	if ids := feedIDs(t, app, admin); ids[homeNotice] || !ids[secondNotice] {
		t.Fatalf("second branch: got %v, want its notice only", ids)
	}
	// A notice of another branch cannot be marked read while in this one.
	app.As(admin).Post(fmt.Sprintf("/api/v1/notification/%d/read", homeNotice), nil).Status(t, http.StatusNotFound)
	app.As(admin).Delete("/api/v1/user/switch-branch").Status(t, http.StatusOK)
	if ids := feedIDs(t, app, admin); !ids[homeNotice] || !ids[secondNotice] {
		t.Fatalf("back to all branches: got %v, want both", ids)
	}

	// Someone who may not switch sees their own branch's notices only.
	if ids := feedIDs(t, app, clerk); !ids[homeNotice] || ids[secondNotice] {
		t.Fatalf("clerk: got %v, want the home branch notice only", ids)
	}
}

// roleWithout creates a role in the tenant that may read notifications but
// not switch branches.
func roleWithout(t *testing.T, app *testkit.App, tenantID int64) string {
	t.Helper()
	const name = "Clerk"
	var role struct {
		ID int64 `json:"id"`
	}
	app.As(app.SuperAdmin(t)).InTenant(tenantID, app.DefaultBranch(t, tenantID)).
		Post("/api/v1/role", map[string]any{"name": name}).Status(t, http.StatusCreated).Data(t, &role)
	return name
}
