package announcements_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type detail struct {
	ID       int64  `json:"id"`
	Audience string `json:"audience"`
	State    string `json:"state"`
	Tenants  []struct {
		ID int64 `json:"id"`
	} `json:"tenants"`
	Totals struct {
		Views      int64 `json:"views"`
		Clicks     int64 `json:"clicks"`
		Dismissals int64 `json:"dismissals"`
	} `json:"totals"`
}

type live struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	CTAURL   *string `json:"cta_url"`
	Kind     string  `json:"kind"`
	Dismiss  bool    `json:"dismissible"`
	CTALabel *string `json:"cta_label"`
}

func liveIDs(t *testing.T, app *testkit.App, user *testkit.User) map[int64]bool {
	t.Helper()
	var items []live
	app.As(user).Get("/api/v1/announcements/live").Status(t, http.StatusOK).Data(t, &items)
	ids := map[int64]bool{}
	for _, item := range items {
		ids[item.ID] = true
	}
	return ids
}

// One announcement reaches its audience: the listed organizations or all of
// them, only while published and inside its window.
func TestAnnouncementAudienceAndWindow(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	member := app.User(t)
	otherTenant := app.Tenant(t, "Announcement Other")
	other := app.User(t, testkit.InTenant(otherTenant))

	var targeted, everyone, draft, later, ended detail
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{
		"title": "New reports module", "kind": "promotion", "cta_label": "Learn more", "cta_url": "https://example.com/reports",
		"tenant_ids": []int64{testkit.DefaultTenant},
	}).Status(t, http.StatusCreated).Data(t, &targeted)
	if targeted.Audience != "tenants" || len(targeted.Tenants) != 1 || targeted.State != "live" {
		t.Fatalf("targeted = %+v", targeted)
	}
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{"title": "Maintenance tonight", "kind": "warning"}).
		Status(t, http.StatusCreated).Data(t, &everyone)
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{"title": "Not yet", "kind": "info", "status": "draft"}).
		Status(t, http.StatusCreated).Data(t, &draft)
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{
		"title": "Next week", "kind": "info", "starts_at": time.Now().Add(48 * time.Hour).Format(time.RFC3339),
	}).Status(t, http.StatusCreated).Data(t, &later)
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{
		"title": "Over", "kind": "info",
		"starts_at": time.Now().Add(-48 * time.Hour).Format(time.RFC3339), "ends_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}).Status(t, http.StatusCreated).Data(t, &ended)
	if later.State != "scheduled" || ended.State != "ended" || draft.State != "draft" {
		t.Fatalf("states: later %s, ended %s, draft %s", later.State, ended.State, draft.State)
	}

	mine := liveIDs(t, app, member)
	if !mine[targeted.ID] || !mine[everyone.ID] || mine[draft.ID] || mine[later.ID] || mine[ended.ID] {
		t.Fatalf("member sees %v", mine)
	}
	theirs := liveIDs(t, app, other)
	if theirs[targeted.ID] || !theirs[everyone.ID] {
		t.Fatalf("other organization sees %v", theirs)
	}
	app.As(other).Post(fmt.Sprintf("/api/v1/announcements/%d/events", targeted.ID), map[string]any{"type": "view"}).Status(t, http.StatusNotFound)

	// Listing: one row per announcement, however many organizations it reaches.
	var listed []struct {
		ID          int64 `json:"id"`
		TenantCount int64 `json:"tenant_count"`
	}
	app.As(staff).Get("/api/v1/internal/announcements?per_page=100").Status(t, http.StatusOK).Data(t, &listed)
	if len(listed) != 5 {
		t.Fatalf("five announcements, one row each; got %d", len(listed))
	}

	app.As(member).Get("/api/v1/internal/announcements").Status(t, http.StatusForbidden)
	app.As(member).Post("/api/v1/internal/announcements", map[string]any{"title": "x", "kind": "info"}).Status(t, http.StatusForbidden)
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{"title": "Half a button", "kind": "info", "cta_label": "Go"}).
		Status(t, http.StatusUnprocessableEntity)
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{"title": "Bad link", "kind": "info", "cta_label": "Go", "cta_url": "javascript:alert(1)"}).
		Status(t, http.StatusUnprocessableEntity)
}

// Views, clicks and dismissals count people once; a dismissed announcement
// leaves that person's banner; one that cannot be dismissed refuses it.
func TestAnnouncementEvents(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	member := app.User(t)
	colleague := app.User(t)

	var promo, pinned detail
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{
		"title": "Try the new app", "kind": "promotion", "cta_label": "Open", "cta_url": "https://example.com",
	}).Status(t, http.StatusCreated).Data(t, &promo)
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{"title": "Read this", "kind": "critical", "dismissible": false}).
		Status(t, http.StatusCreated).Data(t, &pinned)

	events := func(user *testkit.User, id int64, kinds ...string) {
		for _, kind := range kinds {
			app.As(user).Post(fmt.Sprintf("/api/v1/announcements/%d/events", id), map[string]any{"type": kind}).Status(t, http.StatusNoContent)
		}
	}
	events(member, promo.ID, "view", "click", "dismiss")
	// A repeat (another tab, a reload) counts the person once.
	if err := app.Deps.Platform.Announcements.Track(context.Background(), testkit.DefaultTenant, member.ID, promo.ID, "view"); err != nil {
		t.Fatal(err)
	}
	events(colleague, promo.ID, "view")
	app.As(member).Post(fmt.Sprintf("/api/v1/announcements/%d/events", pinned.ID), map[string]any{"type": "dismiss"}).Status(t, http.StatusConflict)
	app.As(member).Post(fmt.Sprintf("/api/v1/announcements/%d/events", promo.ID), map[string]any{"type": "share"}).Status(t, http.StatusUnprocessableEntity)

	if ids := liveIDs(t, app, member); ids[promo.ID] || !ids[pinned.ID] {
		t.Fatalf("after dismissing, member sees %v", ids)
	}
	if ids := liveIDs(t, app, colleague); !ids[promo.ID] {
		t.Fatalf("a colleague still sees it: %v", ids)
	}

	var got detail
	app.As(staff).Get(fmt.Sprintf("/api/v1/internal/announcements/%d", promo.ID)).Status(t, http.StatusOK).Data(t, &got)
	if got.Totals.Views != 2 || got.Totals.Clicks != 1 || got.Totals.Dismissals != 1 {
		t.Fatalf("totals = %+v, want 2 views, 1 click, 1 dismissal", got.Totals)
	}
	var byTenant []struct {
		ID    int64 `json:"id"`
		Views int64 `json:"views"`
	}
	app.As(staff).Get(fmt.Sprintf("/api/v1/internal/announcements/%d/organizations?per_page=10", promo.ID)).Status(t, http.StatusOK).Data(t, &byTenant)
	if len(byTenant) != 1 || byTenant[0].ID != testkit.DefaultTenant || byTenant[0].Views != 2 {
		t.Fatalf("by organization = %+v", byTenant)
	}
	app.As(staff).Get(fmt.Sprintf("/api/v1/internal/announcements/%d/organizations?search=nothing-like-this", promo.ID)).Status(t, http.StatusOK).Data(t, &byTenant)
	if len(byTenant) != 0 {
		t.Fatalf("a search with no match = %+v", byTenant)
	}
	app.As(member).Get(fmt.Sprintf("/api/v1/internal/announcements/%d/organizations", promo.ID)).Status(t, http.StatusForbidden)

	app.As(staff).Delete(fmt.Sprintf("/api/v1/internal/announcements/%d", promo.ID)).Status(t, http.StatusNoContent)
	app.As(staff).Get(fmt.Sprintf("/api/v1/internal/announcements/%d", promo.ID)).Status(t, http.StatusNotFound)
}

// Channels deliver an announcement once, into each audience organization's
// notifications.
func TestAnnouncementDelivery(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	member := app.User(t)
	outsider := app.User(t, testkit.InTenant(app.Tenant(t, "Not Told")))

	var created detail
	app.As(staff).Post("/api/v1/internal/announcements", map[string]any{
		"title": "Price change in March", "kind": "warning", "channels": []string{"database"}, "tenant_ids": []int64{testkit.DefaultTenant},
	}).Status(t, http.StatusCreated).Data(t, &created)

	ctx := context.Background()
	sent, err := app.Deps.Platform.Announcements.DeliverDue(ctx)
	if err != nil || sent != 1 {
		t.Fatalf("deliver = %d, %v", sent, err)
	}
	if again, err := app.Deps.Platform.Announcements.DeliverDue(ctx); err != nil || again != 0 {
		t.Fatalf("delivered twice: %d, %v", again, err)
	}

	count := func(user *testkit.User) int {
		var feed []struct {
			EventType string `json:"event_type"`
			Title     string `json:"title"`
		}
		app.As(user).Get("/api/v1/notification/feed?limit=50").Status(t, http.StatusOK).Data(t, &feed)
		n := 0
		for _, item := range feed {
			if item.EventType == "platform.announcement" && item.Title == "Price change in March" {
				n++
			}
		}
		return n
	}
	if got := count(member); got != 1 {
		t.Fatalf("member got %d notifications, want 1", got)
	}
	if got := count(outsider); got != 0 {
		t.Fatalf("an organization outside the audience got %d", got)
	}
}
