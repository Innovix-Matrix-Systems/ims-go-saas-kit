package app_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type profilePermissions struct {
	SuperAdmin  bool     `json:"super_admin"`
	Permissions []string `json:"permissions"`
	ActingAs    bool     `json:"acting_as"`
}

// Staff acting as a user (X-Acting-User) read that user's permissions, so a
// client can show the user's own screens; without the header they read their
// own. /permission/user cannot do this: it is tenant-exempt and refuses the
// header.
func TestProfilePermissionsFollowActingUser(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	member := app.User(t, testkit.Role(app.Role(t, "Viewer", "branch.view.all")))

	var own profilePermissions
	app.As(staff).Get("/api/v1/user/profile/permissions").Status(t, http.StatusOK).Data(t, &own)
	if !own.SuperAdmin || own.ActingAs {
		t.Fatalf("staff's own = %+v, want super admin, not acting", own)
	}

	var acting profilePermissions
	actingAs(app.As(staff), member).Get("/api/v1/user/profile/permissions").Status(t, http.StatusOK).Data(t, &acting)
	if acting.SuperAdmin || !acting.ActingAs || !slices.Equal(acting.Permissions, []string{"branch.view.all"}) {
		t.Fatalf("acting as the member = %+v, want only branch.view.all", acting)
	}
}

// platform=true lists staff accounts only; a tenant admin gets nobody.
func TestUserListPlatformFilter(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)

	var users []struct {
		ID       int64  `json:"id"`
		TenantID *int64 `json:"tenant_id"`
	}
	app.As(staff).Get("/api/v1/user?platform=true&per_page=100").Status(t, http.StatusOK).Data(t, &users)
	if len(users) == 0 {
		t.Fatal("no staff accounts listed")
	}
	for _, user := range users {
		if user.TenantID != nil {
			t.Fatalf("user %d of tenant %d listed as staff", user.ID, *user.TenantID)
		}
	}
	app.As(admin).Get("/api/v1/user?platform=true").Status(t, http.StatusOK).Data(t, &users)
	if len(users) != 0 {
		t.Fatalf("a tenant admin sees %d staff accounts, want 0", len(users))
	}
}

// The approval list tells a reviewer who pays, how much and for which plan.
func TestApprovalListCarriesDetails(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)

	var pkg struct {
		ID int64 `json:"id"`
	}
	app.As(staff).Post("/api/v1/packages", map[string]any{
		"name": "Pro Review", "price": 30000, "billing_cycle": "monthly", "is_active": true,
		"features": []map[string]any{{"feature_key": "users.max", "is_enabled": true, "limit_value": 25}},
	}).Status(t, http.StatusCreated).Data(t, &pkg)
	app.As(admin).Post("/api/v1/billing/subscribe", map[string]any{
		"package_id": pkg.ID, "payment_method": "bank_transfer", "payment_data": map[string]any{"reference_number": "REF-42"},
	}).Status(t, http.StatusCreated)

	var approvals []struct {
		Status  string `json:"status"`
		Details *struct {
			TenantName  string `json:"tenant_name"`
			PackageName string `json:"package_name"`
			Amount      int64  `json:"amount"`
		} `json:"details"`
	}
	app.As(staff).Get("/api/v1/billing/internal/payment-approvals?status=pending").Status(t, http.StatusOK).Data(t, &approvals)
	if len(approvals) != 1 || approvals[0].Details == nil {
		t.Fatalf("approvals = %+v, want one with details", approvals)
	}
	if d := approvals[0].Details; d.PackageName != "Pro Review" || d.Amount != 30000 || d.TenantName == "" {
		t.Fatalf("details = %+v", d)
	}
}

// Staff broadcast an announcement: to every active organization by default,
// or to the ones they pick. Members see it in their feed; others do not.
func TestStaffBroadcastsAnnouncement(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	listeners := app.Tenant(t, "Listeners")
	member := app.User(t, testkit.InTenant(listeners))
	other := app.User(t, testkit.InTenant(app.Tenant(t, "Others")))

	titles := func(user *testkit.User) []string {
		var feed []struct {
			Title     string `json:"title"`
			EventType string `json:"event_type"`
		}
		app.As(user).Get("/api/v1/notification/feed").Status(t, http.StatusOK).Data(t, &feed)
		out := []string{}
		for _, item := range feed {
			if item.EventType == "platform.announcement" {
				out = append(out, item.Title)
			}
		}
		return out
	}

	var sent struct {
		Organizations int `json:"organizations"`
	}
	app.As(staff).Post("/api/v1/internal/notification/broadcast", map[string]any{
		"priority": "INFO", "title": "Planned maintenance", "body": "Saturday 02:00 to 03:00.",
	}).Status(t, http.StatusCreated).Data(t, &sent)
	if sent.Organizations < 3 {
		t.Fatalf("reached %d organizations, want every active one (at least 3)", sent.Organizations)
	}
	app.As(staff).Post("/api/v1/internal/notification/broadcast", map[string]any{
		"priority": "WARNING", "title": "Only for Listeners", "tenant_ids": []int64{listeners},
	}).Status(t, http.StatusCreated)

	if got := titles(member); !slices.Contains(got, "Planned maintenance") || !slices.Contains(got, "Only for Listeners") {
		t.Fatalf("member's announcements = %v", got)
	}
	if got := titles(other); !slices.Contains(got, "Planned maintenance") || slices.Contains(got, "Only for Listeners") {
		t.Fatalf("other organization's announcements = %v", got)
	}
	app.As(member).Post("/api/v1/internal/notification/broadcast", map[string]any{"priority": "INFO", "title": "x"}).
		Status(t, http.StatusForbidden)

	// A staff account's own feed holds no organization's announcements.
	var staffFeed []struct {
		Title string `json:"title"`
	}
	app.As(staff).Get("/api/v1/internal/notification/feed").Status(t, http.StatusOK).Data(t, &staffFeed)
	app.As(staff).Get("/api/v1/notification/feed").Status(t, http.StatusOK)
	for _, item := range staffFeed {
		if item.Title == "Planned maintenance" || item.Title == "Only for Listeners" {
			t.Fatalf("staff feed shows the organizations' announcement %q", item.Title)
		}
	}

	// Staff list the announcements they sent, without other notifications.
	var page struct {
		Data []struct {
			EventType string `json:"event_type"`
		} `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	app.As(staff).Get("/api/v1/internal/notification?event_type=platform.announcement&per_page=100").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != int64(sent.Organizations+1) {
		t.Fatalf("announcements listed = %d, want %d", page.Meta.Total, sent.Organizations+1)
	}
	for _, item := range page.Data {
		if item.EventType != "platform.announcement" {
			t.Fatalf("listed a %s notification", item.EventType)
		}
	}
}
