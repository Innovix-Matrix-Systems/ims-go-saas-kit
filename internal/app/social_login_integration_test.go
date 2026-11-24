package app_test

import (
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// Social login and the login activity. The providers' own checks
// are unit-tested in internal/authn/social against fake providers; these
// tests cover the routes and what a login leaves in the activity log.

func TestSocialLoginOffByDefault(t *testing.T) {
	app := testkit.New(t)

	var providers []map[string]any
	app.Guest().Get("/api/v1/social/providers").Status(t, http.StatusOK).Data(t, &providers)
	if len(providers) != 0 {
		t.Fatalf("providers = %v, want none with SOCIAL_PROVIDERS empty", providers)
	}
	app.Guest().Post("/api/v1/social/google/login", map[string]any{"id_token": "x", "device": "web"}).
		Status(t, http.StatusNotFound)
}

func TestSocialProvidersListed(t *testing.T) {
	t.Setenv("SOCIAL_PROVIDERS", "google,github")
	t.Setenv("SOCIAL_GOOGLE_CLIENT_IDS", "web-client")
	t.Setenv("SOCIAL_GITHUB_CLIENT_ID", "id")
	t.Setenv("SOCIAL_GITHUB_CLIENT_SECRET", "secret")
	app := testkit.New(t)

	var providers []struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	app.Guest().Get("/api/v1/social/providers").Status(t, http.StatusOK).Data(t, &providers)
	if len(providers) != 2 || providers[0].Name != "google" || providers[1].DisplayName != "GitHub" {
		t.Fatalf("providers = %+v, want google then GitHub", providers)
	}
	// A request without the provider's proof never reaches the provider.
	app.Guest().Post("/api/v1/social/github/login", map[string]any{"device": "web"}).
		Status(t, http.StatusUnprocessableEntity)
}

func TestPasswordLoginIsRecorded(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t, testkit.Email("login-audit@example.com")) // signs in once

	app.Guest().Post("/api/v1/login", map[string]any{
		"email": user.Email, "password": "wrong-password", "device": "web",
	}).Status(t, http.StatusUnauthorized)

	ok := countRows(t, app, `SELECT count(*) FROM activity_log
		WHERE action = 'auth.login' AND actor_id = $1 AND tenant_id = $2
		  AND changes->>'method' = 'password' AND changes->>'status' = 'ok'`, user.ID, testkit.DefaultTenant)
	failed := countRows(t, app, `SELECT count(*) FROM activity_log
		WHERE action = 'auth.login' AND actor_id IS NULL AND tenant_id IS NULL
		  AND changes->>'email' = $1 AND changes->>'error' = 'INVALID_CREDENTIALS'`, user.Email)
	if ok != 1 || failed != 1 {
		t.Fatalf("auth.login rows: ok = %d, failed = %d; want 1 and 1", ok, failed)
	}
}
