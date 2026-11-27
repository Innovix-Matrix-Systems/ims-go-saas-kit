package app_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// TestReadOnlyAllowlistNamesRoutes: every write a read-only organization may
// still make names a real route, so a typo or a renamed route fails here
// instead of quietly locking members out of paying or exporting.
func TestReadOnlyAllowlistNamesRoutes(t *testing.T) {
	kit := testkit.New(t)
	mux, ok := kit.Deps.Router().(chi.Routes)
	if !ok {
		t.Fatal("the router does not implement chi.Routes")
	}
	routes := map[string]bool{}
	if err := chi.Walk(mux, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+pattern] = true
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	for _, route := range app.ReadOnlyAllowed {
		if !routes[route] {
			t.Errorf("read-only allowlist names %q, which is not a route", route)
		}
	}
}
