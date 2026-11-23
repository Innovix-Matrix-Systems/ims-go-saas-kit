package activity_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type feedPage struct {
	Data []entry `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
}

type facets struct {
	Actions []struct {
		Value string `json:"value"`
		Total int64  `json:"total"`
	} `json:"actions"`
	Actors []struct {
		ID    int64 `json:"id"`
		Total int64 `json:"total"`
	} `json:"actors"`
	LastDay int64 `json:"last_day"`
}

// The feed filters by action, time and text, reports the total, and the
// facets describe only the reader's own tenant.
func TestActivityFiltersTotalsAndFacets(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))

	app.As(admin).Post("/api/v1/role", map[string]any{"name": "Auditors"}).Status(t, http.StatusCreated)
	app.As(admin).Post("/api/v1/role", map[string]any{"name": "Cashiers"}).Status(t, http.StatusCreated)
	app.As(other).Post("/api/v1/role", map[string]any{"name": "Elsewhere"}).Status(t, http.StatusCreated)

	var page feedPage
	app.As(admin).Get("/api/v1/activity?action=role.created").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 2 || len(page.Data) != 2 {
		t.Fatalf("role.created: total %d, rows %d, want 2 and 2", page.Meta.Total, len(page.Data))
	}

	app.As(admin).Get("/api/v1/activity?search=cashiers").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 1 || page.Data[0].Action != "role.created" {
		t.Fatalf("search cashiers = %+v, want the one role.created row", page)
	}

	future := url.QueryEscape(time.Now().Add(time.Hour).Format(time.RFC3339))
	app.As(admin).Get(fmt.Sprintf("/api/v1/activity?from=%s", future)).Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 0 {
		t.Fatalf("from the future: total %d, want 0", page.Meta.Total)
	}
	app.As(admin).Get("/api/v1/activity?from=yesterday").Status(t, http.StatusUnprocessableEntity)

	var got facets
	app.As(admin).Get("/api/v1/activity/facets").Status(t, http.StatusOK).Data(t, &got)
	var roleCreated int64
	for _, item := range got.Actions {
		if item.Value == "role.created" {
			roleCreated = item.Total
		}
	}
	if roleCreated != 2 {
		t.Fatalf("facet role.created = %d, want 2 (tenant B's role must not count)", roleCreated)
	}
	if got.LastDay < 2 {
		t.Fatalf("last_day = %d, want at least 2", got.LastDay)
	}
	for _, actor := range got.Actors {
		if actor.ID == other.ID {
			t.Fatal("facets list an actor from another tenant")
		}
	}
}
