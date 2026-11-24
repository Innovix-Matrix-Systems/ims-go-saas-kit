package activity_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type entry struct {
	Action    string          `json:"action"`
	SubjectID int64           `json:"subject_id"`
	TenantID  int64           `json:"tenant_id"`
	Changes   json.RawMessage `json:"changes"`
	Actor     *struct {
		ID int64 `json:"id"`
	} `json:"actor"`
}

// Creating a user in tenant A writes one entry that A's admin sees and B's
// admin does not.
func TestActivityIsTenantScoped(t *testing.T) {
	app := testkit.New(t)
	adminA := app.User(t)
	adminB := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))

	created := app.User(t, testkit.Role(app.Role(t, "Member")))

	var feedA []entry
	app.As(adminA).Get("/api/v1/activity?subject_type=user").Status(t, http.StatusOK).Data(t, &feedA)
	found := 0
	for _, item := range feedA {
		if item.Action == "user.created" && item.SubjectID == created.ID {
			found++
			if item.TenantID != testkit.DefaultTenant || item.Actor == nil || item.Actor.ID != 1 {
				t.Errorf("entry = %+v; want tenant %d, actor super admin", item, testkit.DefaultTenant)
			}
		}
	}
	if found != 1 {
		t.Fatalf("tenant A sees %d user.created entries for user %d, want 1", found, created.ID)
	}

	var feedB []entry
	app.As(adminB).Get("/api/v1/activity").Status(t, http.StatusOK).Data(t, &feedB)
	for _, item := range feedB {
		if item.SubjectID == created.ID || item.TenantID == testkit.DefaultTenant {
			t.Fatalf("tenant B sees tenant A's activity: %+v", item)
		}
	}
}

func TestActivityRedactsAndPaginates(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)

	// Two branch changes, one page of size 1 each.
	app.As(admin).Post("/api/v1/branch", `{"name":"Dhaka"}`).Status(t, http.StatusCreated)
	app.As(admin).Post("/api/v1/branch", `{"name":"Chattogram"}`).Status(t, http.StatusCreated)

	var page struct {
		Data []entry `json:"data"`
		Meta struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"meta"`
	}
	app.As(admin).Get("/api/v1/activity?subject_type=branch&per_page=1").Status(t, http.StatusOK).JSON(t, &page)
	if len(page.Data) != 1 || page.Data[0].Action != "branch.created" || page.Meta.NextCursor == nil {
		t.Fatalf("first page = %+v", page)
	}
	first := string(page.Data[0].Changes)
	app.As(admin).Get("/api/v1/activity?subject_type=branch&per_page=1&cursor="+*page.Meta.NextCursor).
		Status(t, http.StatusOK).JSON(t, &page)
	if len(page.Data) != 1 || string(page.Data[0].Changes) == first {
		t.Fatalf("second page = %+v (first changes %s)", page, first)
	}

	// A plain user without activity.view is refused.
	user := app.User(t, testkit.Role(app.Role(t, "Member")))
	app.As(user).Get("/api/v1/activity").Status(t, http.StatusForbidden)

	// Password changes are logged without the password.
	app.As(admin).Post("/api/v1/user/change-password", map[string]any{"user_id": user.ID, "password": "another-pass-9"}).
		Status(t, http.StatusOK)
	var feed []entry
	app.As(admin).Get("/api/v1/activity?subject_type=user").Status(t, http.StatusOK).Data(t, &feed)
	if len(feed) == 0 || feed[0].Action != "user.password_changed" || feed[0].SubjectID != user.ID || string(feed[0].Changes) != "{}" {
		t.Fatalf("newest user entry = %+v; want an empty user.password_changed for %d", feed[0], user.ID)
	}
}
