package auth_test

import (
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// Logout revokes the token that made the request, not every token with the
// same device name: two browsers both called "Web · Chrome on Windows" must
// not sign each other out.
func TestLogoutRevokesOnlyTheCallingToken(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	first, second := *user, *user
	first.Token = app.Login(t, user.Email, testkit.Password)
	second.Token = app.Login(t, user.Email, testkit.Password)

	// The same device name both logins used; older clients still send it.
	app.As(&first).Post("/api/v1/logout", map[string]any{"device": "testkit"}).Status(t, http.StatusNoContent)
	app.As(&first).Get("/api/v1/user/profile").Status(t, http.StatusUnauthorized)
	app.As(&second).Get("/api/v1/user/profile").Status(t, http.StatusOK)

	// No body at all is fine too.
	app.As(&second).Post("/api/v1/logout", nil).Status(t, http.StatusNoContent)
	app.As(&second).Get("/api/v1/user/profile").Status(t, http.StatusUnauthorized)
}
