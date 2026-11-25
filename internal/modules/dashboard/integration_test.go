package dashboard_test

import (
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

type series struct {
	Points []struct {
		Date  string `json:"date"`
		Value int64  `json:"value"`
	} `json:"points"`
	Total         int64 `json:"total"`
	PreviousTotal int64 `json:"previous_total"`
}

type tenantDashboard struct {
	Period struct {
		Days     int    `json:"days"`
		TimeZone string `json:"time_zone"`
	} `json:"period"`
	Members *struct {
		Total int64  `json:"total"`
		Added series `json:"added"`
	} `json:"members"`
	Activity *struct {
		Events     series `json:"events"`
		TopActions []struct {
			Action string `json:"action"`
			Count  int64  `json:"count"`
		} `json:"top_actions"`
	} `json:"activity"`
	Files *struct {
		Files  int64 `json:"files"`
		ByType []struct {
			Type  string `json:"type"`
			Files int64  `json:"files"`
		} `json:"by_type"`
	} `json:"files"`
	Branches *int64 `json:"branches"`
	Roles    *int64 `json:"roles"`
}

// An organization's dashboard counts its own members and activity only, and
// each section needs its permission.
func TestTenantDashboard(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	other := app.User(t, testkit.InTenant(app.Tenant(t, "Dashboard Other")))
	app.As(admin).Post("/api/v1/role", map[string]any{"name": "Dashboard Role"}).Status(t, http.StatusCreated)

	var mine tenantDashboard
	app.As(admin).Get("/api/v1/dashboard?days=7&tz=Asia/Dhaka").Status(t, http.StatusOK).Data(t, &mine)
	if mine.Period.Days != 7 || mine.Period.TimeZone != "Asia/Dhaka" {
		t.Fatalf("period = %+v", mine.Period)
	}
	if mine.Members == nil || mine.Activity == nil || mine.Files == nil || mine.Branches == nil || mine.Roles == nil {
		t.Fatalf("an admin sees every section: %+v", mine)
	}
	if len(mine.Members.Added.Points) != 7 || len(mine.Activity.Events.Points) != 7 {
		t.Fatalf("one point per day: %d, %d", len(mine.Members.Added.Points), len(mine.Activity.Events.Points))
	}
	if mine.Members.Total < 1 || mine.Members.Added.Total < 1 || mine.Activity.Events.Total < 1 {
		t.Fatalf("members %+v, activity total %d", mine.Members, mine.Activity.Events.Total)
	}
	found := false
	for _, action := range mine.Activity.TopActions {
		found = found || action.Action == "role.created"
	}
	if !found {
		t.Fatalf("top actions %+v miss role.created", mine.Activity.TopActions)
	}

	if mine.Files.ByType == nil {
		t.Fatal("by_type is a list, even when empty")
	}

	var theirs tenantDashboard
	app.As(other).Get("/api/v1/dashboard").Status(t, http.StatusOK).Data(t, &theirs)
	if theirs.Period.Days != 30 || theirs.Members == nil || theirs.Members.Total != 1 {
		t.Fatalf("another organization counts only its own: %+v", theirs.Members)
	}

	viewer := app.User(t, testkit.Role(app.Role(t, "Dashboard Viewer", "activity.view")))
	var limited tenantDashboard
	app.As(viewer).Get("/api/v1/dashboard").Status(t, http.StatusOK).Data(t, &limited)
	if limited.Activity == nil || limited.Members != nil || limited.Files != nil || limited.Branches != nil || limited.Roles != nil {
		t.Fatalf("only the activity section: %+v", limited)
	}

	app.As(admin).Get("/api/v1/dashboard?days=12").Status(t, http.StatusUnprocessableEntity)
	app.As(admin).Get("/api/v1/dashboard?tz=Mars/Base").Status(t, http.StatusUnprocessableEntity)

	staff := app.SuperAdmin(t)
	app.As(staff).Get("/api/v1/dashboard").Status(t, http.StatusForbidden)
	app.As(staff).InTenant(testkit.DefaultTenant, app.DefaultBranch(t, testkit.DefaultTenant)).
		Get("/api/v1/dashboard").Status(t, http.StatusOK)
}

// Staff see the platform: organizations, users, activity, revenue and plans;
// organization members cannot.
func TestPlatformDashboard(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)
	app.Tenant(t, "Dashboard New Org")

	var out struct {
		Organizations *struct {
			Total  int64  `json:"total"`
			Added  series `json:"added"`
			Recent []struct {
				Name string `json:"name"`
			} `json:"recent"`
		} `json:"organizations"`
		Users    *struct{ Total int64 } `json:"users"`
		Activity *series                `json:"activity"`
		Revenue  *struct {
			Currencies []any `json:"currencies"`
			Pending    []any `json:"pending"`
		} `json:"revenue"`
		Plans []any `json:"plans"`
	}
	app.As(staff).Get("/api/v1/internal/dashboard?days=90").Status(t, http.StatusOK).Data(t, &out)
	if out.Organizations == nil || out.Users == nil || out.Activity == nil || out.Revenue == nil || out.Plans == nil {
		t.Fatalf("a super admin sees every section: %+v", out)
	}
	if out.Organizations.Total < 2 || out.Organizations.Added.Total < 1 || len(out.Organizations.Added.Points) != 90 {
		t.Fatalf("organizations = %+v", out.Organizations)
	}
	if len(out.Organizations.Recent) == 0 || out.Organizations.Recent[0].Name != "Dashboard New Org" {
		t.Fatalf("recent = %+v", out.Organizations.Recent)
	}
	if out.Users.Total < 1 {
		t.Fatalf("users = %+v", out.Users)
	}

	app.As(admin).Get("/api/v1/internal/dashboard").Status(t, http.StatusForbidden)
}
