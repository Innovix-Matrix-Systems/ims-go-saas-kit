package dev_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func TestTenantsPanel(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	app := testkit.New(t)
	super := app.As(app.SuperAdmin(t))
	second := app.Tenant(t, "Second Clinic")
	app.User(t, testkit.InTenant(second))

	var page struct {
		Data []struct {
			ID                 int64   `json:"id"`
			Name               string  `json:"name"`
			SubscriptionStatus *string `json:"subscription_status"`
			Users              int64   `json:"users"`
			Branches           int64   `json:"branches"`
		} `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	super.Get("/dev/api/tenants?search=Second").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != second {
		t.Fatalf("search = %+v", page)
	}
	row := page.Data[0]
	if row.Users != 1 || row.Branches != 1 || row.SubscriptionStatus == nil || *row.SubscriptionStatus != "trial" {
		t.Fatalf("row = %+v", row)
	}
	super.Get("/dev/api/tenants?subscription=none").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 0 {
		t.Fatalf("tenants without subscription = %d, want 0", page.Meta.Total)
	}
	super.Get("/dev/api/tenants?subscription=bogus").Status(t, http.StatusUnprocessableEntity)

	var detail struct {
		Features []struct {
			Key   string `json:"key"`
			Limit *int64 `json:"limit"`
			Used  *int64 `json:"used"`
		} `json:"features"`
		Subscriptions []struct {
			PackageName string `json:"package_name"`
		} `json:"subscriptions"`
	}
	super.Get(fmt.Sprintf("/dev/api/tenants/%d", second)).Status(t, http.StatusOK).Data(t, &detail)
	if len(detail.Subscriptions) != 1 || detail.Subscriptions[0].PackageName != "Testkit Unlimited" {
		t.Fatalf("subscriptions = %+v", detail.Subscriptions)
	}
	for _, feature := range detail.Features {
		if feature.Key == "users.max" && (feature.Limit != nil || feature.Used == nil || *feature.Used != 1) {
			t.Fatalf("users.max = %+v, want 1 used of unlimited", feature)
		}
	}
	super.Get("/dev/api/tenants/999999").Status(t, http.StatusNotFound)
}

func TestLogSearchAndSettingPlacement(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	app := testkit.New(t)
	super := app.As(app.SuperAdmin(t))

	super.Post("/dev/api/simulate-events", `{"logs":2,"level":"warn","exception":false}`).Status(t, http.StatusOK)
	var logs struct {
		Data []struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	// Telemetry is flushed in the background: wait for it.
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		super.Get("/dev/api/logs?search=dev-simulator").Status(t, http.StatusOK).JSON(t, &logs)
		if len(logs.Data) >= 2 {
			break
		}
	}
	if len(logs.Data) < 2 {
		t.Fatalf("search by attribute found %d logs", len(logs.Data))
	}
	super.Get("/dev/api/logs?search=no-such-text-anywhere").Status(t, http.StatusOK).JSON(t, &logs)
	if len(logs.Data) != 0 {
		t.Fatalf("unmatched search returned %d logs", len(logs.Data))
	}

	super.Post("/dev/api/settings/sync", `{}`).Status(t, http.StatusOK)
	var groups []struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	super.Get("/dev/api/settings/groups").Status(t, http.StatusOK).Data(t, &groups)
	ids := map[string]int64{}
	order := []int64{}
	for _, group := range groups {
		ids[group.Slug] = group.ID
		order = append([]int64{group.ID}, order...) // reversed
	}

	// Move notification.enabled into General, before system.name.
	super.Put(fmt.Sprintf("/dev/api/settings/groups/%d/keys", ids["general"]),
		`{"keys":["notification.enabled","system.name"]}`).Status(t, http.StatusOK)
	var items []struct {
		Key       string `json:"key"`
		SortOrder int    `json:"sort_order"`
		Group     struct {
			Slug string `json:"slug"`
		} `json:"group"`
	}
	super.Get("/dev/api/settings").Status(t, http.StatusOK).Data(t, &items)
	position := map[string]int{}
	for index, item := range items {
		if item.Key == "notification.enabled" && item.Group.Slug != "general" {
			t.Fatalf("notification.enabled is in %s, want general", item.Group.Slug)
		}
		position[item.Key] = index
	}
	if position["notification.enabled"] > position["system.name"] {
		t.Fatalf("order = %v, want notification.enabled first", position)
	}

	super.Put("/dev/api/settings/groups/order", map[string]any{"ids": order}).Status(t, http.StatusOK)
	super.Get("/dev/api/settings/groups").Status(t, http.StatusOK).Data(t, &groups)
	if groups[0].ID != order[0] {
		t.Fatalf("first group = %d, want %d", groups[0].ID, order[0])
	}
	super.Put(fmt.Sprintf("/dev/api/settings/groups/%d/keys", ids["general"]), `{"keys":["no.such.key"]}`).
		Status(t, http.StatusNotFound)
}
