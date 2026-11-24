package dev_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func TestInspectorAndCommandsPanels(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	app := testkit.New(t)
	super := app.As(app.SuperAdmin(t))

	var routes struct {
		Global []string `json:"global"`
		Routes []struct {
			Method      string   `json:"method"`
			Path        string   `json:"path"`
			Permissions []string `json:"permissions"`
			Middleware  []string `json:"middleware"`
		} `json:"routes"`
	}
	super.Get("/dev/api/inspector/routes").Status(t, http.StatusOK).Data(t, &routes)
	found := false
	for _, route := range routes.Routes {
		if route.Method == "POST" && route.Path == "/api/v1/branch" {
			found = len(route.Permissions) == 1 && route.Permissions[0] == "branch.create" &&
				strings.Contains(strings.Join(route.Middleware, ","), "tenant.resolve")
		}
	}
	if !found || len(routes.Global) == 0 {
		t.Fatalf("POST /api/v1/branch not reported with can:branch.create (global %v)", routes.Global)
	}

	var config []struct {
		Name    string `json:"name"`
		Entries []struct {
			Key    string `json:"key"`
			Value  string `json:"value"`
			Masked bool   `json:"masked"`
		} `json:"entries"`
	}
	super.Get("/dev/api/inspector/config").Status(t, http.StatusOK).Data(t, &config)
	for _, section := range config {
		for _, entry := range section.Entries {
			if strings.HasSuffix(entry.Key, ".dsn") && entry.Value != "" && !entry.Masked {
				t.Fatalf("%s not masked: %s", entry.Key, entry.Value)
			}
			if strings.Contains(entry.Value, testkit.Password) {
				t.Fatalf("%s leaks a password", entry.Key)
			}
		}
	}

	var access struct {
		DeclaredNotStored []string `json:"declared_not_stored"`
	}
	super.Get("/dev/api/inspector/access").Status(t, http.StatusOK).Data(t, &access)
	if len(access.DeclaredNotStored) != 0 {
		t.Fatalf("seeded catalog drift: %v", access.DeclaredNotStored)
	}
	var packages struct {
		Packages []struct {
			Name string `json:"name"`
		} `json:"packages"`
	}
	super.Get("/dev/api/inspector/packages").Status(t, http.StatusOK).Data(t, &packages)
	if !strings.Contains(func() string {
		names := []string{}
		for _, item := range packages.Packages {
			names = append(names, item.Name)
		}
		return strings.Join(names, ",")
	}(), "github.com/go-chi/chi/v5") {
		t.Fatal("go.mod packages missing")
	}

	// Commands: run one (the ':' may arrive escaped), read it back.
	var run struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Output string `json:"output"`
	}
	super.Post("/dev/api/commands/permission%3Async/run", `{}`).Status(t, http.StatusOK).Data(t, &run)
	if run.Status != "ok" || !strings.Contains(run.Output, "Synced") {
		t.Fatalf("permission:sync run = %+v", run)
	}
	super.Post("/dev/api/commands/backup:run/run", `{"options":{"nope":"1"}}`).Status(t, http.StatusUnprocessableEntity)
	super.Post("/dev/api/commands/shell/run", `{}`).Status(t, http.StatusNotFound)

	// A tenant admin has no dev.view.
	app.As(app.User(t)).Get("/dev/api/commands").Status(t, http.StatusForbidden)
}
