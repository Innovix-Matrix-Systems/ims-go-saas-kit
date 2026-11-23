package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
)

func TestFilterRoutes(t *testing.T) {
	routes := []dev.RouteInfo{
		{Method: "GET", Path: "/api/v1/branch"},
		{Method: "DELETE", Path: "/api/v1/branch/{id}"},
		{Method: "GET", Path: "/dev/api/maintenance"},
		{Method: "ANY", Path: "/storage/*"},
	}
	tests := []struct {
		name                     string
		method, path, exceptPath string
		want                     []string
	}{
		{name: "no filter", want: []string{"/api/v1/branch", "/api/v1/branch/{id}", "/dev/api/maintenance", "/storage/*"}},
		{name: "method, any case; ANY matches every method", method: "delete", want: []string{"/api/v1/branch/{id}", "/storage/*"}},
		{name: "path contains", path: "/branch", want: []string{"/api/v1/branch", "/api/v1/branch/{id}"}},
		{name: "except path", exceptPath: "/dev", want: []string{"/api/v1/branch", "/api/v1/branch/{id}", "/storage/*"}},
		{name: "combined", method: "GET", exceptPath: "/dev", want: []string{"/api/v1/branch", "/storage/*"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterRoutes(routes, tt.method, tt.path, tt.exceptPath)
			paths := make([]string, 0, len(got))
			for _, route := range got {
				paths = append(paths, route.Path)
			}
			if strings.Join(paths, " ") != strings.Join(tt.want, " ") {
				t.Errorf("paths = %v, want %v", paths, tt.want)
			}
		})
	}
}

func TestRouteListCommand(t *testing.T) {
	dsn := dbtest.DSN(t) // skips without DATABASE_TEST_DSN; the dependency graph needs it
	t.Setenv("DATABASE_DSN", dsn)
	t.Setenv("APP_ENV_FILE", "")

	out, err := run(t, "route:list", "--path", "/api/v1/branch", "--middleware")
	if err != nil {
		t.Fatalf("route:list: %v\n%s", err, out)
	}
	for _, want := range []string{
		"METHOD", "MIDDLEWARE",
		"POST", "/api/v1/branch", "branch.create", "branches.(Handler).store",
		"can:branch.create", "maintenance", "5 route(s).",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/api/v1/user") {
		t.Errorf("--path filter let other routes through:\n%s", out)
	}

	out, err = run(t, "route:list", "--path", "/api/v1/branch", "--method", "DELETE", "--json")
	if err != nil {
		t.Fatalf("route:list --json: %v\n%s", err, out)
	}
	var report dev.RouteReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if len(report.Routes) != 1 || report.Routes[0].Path != "/api/v1/branch/{id}" ||
		len(report.Routes[0].Permissions) != 1 || report.Routes[0].Permissions[0] != "branch.delete" {
		t.Errorf("json routes = %+v, want DELETE /api/v1/branch/{id} needing branch.delete", report.Routes)
	}
	if len(report.Global) == 0 {
		t.Error("json global middleware is empty")
	}
}
