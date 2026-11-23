package app_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// Self-service sessions (phase 26.3): users list and sign out their own
// sessions; the optional new sign-in alert (docs/guides/authentication.md).

type sessionRow struct {
	ID        int64   `json:"id"`
	Device    string  `json:"device"`
	IP        *string `json:"ip"`
	UserAgent *string `json:"user_agent"`
	Browser   string  `json:"browser"`
	Current   bool    `json:"current"`
}

const chromeOnWindows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// signIn signs in on a device with a user agent (and, behind the trusted
// test proxy, a client IP) and returns the token.
func signIn(t *testing.T, app *testkit.App, email, device, ip string) string {
	t.Helper()
	client := app.Guest().Header("User-Agent", chromeOnWindows)
	if ip != "" {
		client = client.Header("X-Forwarded-For", ip)
	}
	var body struct {
		Token string `json:"token"`
	}
	client.Post("/api/v1/login", map[string]any{"email": email, "password": testkit.Password, "device": device}).
		Status(t, http.StatusOK).Data(t, &body)
	return body.Token
}

func TestUsersManageTheirOwnSessions(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	other := app.User(t)
	web := &testkit.User{ID: user.ID, Token: signIn(t, app, user.Email, "web", "")}
	phone := &testkit.User{ID: user.ID, Token: signIn(t, app, user.Email, "phone", "")}

	var sessions []sessionRow
	app.As(web).Get("/api/v1/sessions").Status(t, http.StatusOK).Data(t, &sessions)
	if len(sessions) != 3 { // testkit's own sign-in, web and phone
		t.Fatalf("sessions = %+v, want 3", sessions)
	}
	byDevice := map[string]sessionRow{}
	for _, session := range sessions {
		byDevice[session.Device] = session
	}
	if !byDevice["web"].Current || byDevice["phone"].Current || byDevice["web"].Browser != "Chrome 131 on Windows" || byDevice["web"].IP == nil {
		t.Fatalf("sessions = %+v, want web current with its browser and IP", sessions)
	}

	// Another user's session is not found, and so is an unknown id.
	app.As(other).Delete("/api/v1/sessions/"+strconv.FormatInt(byDevice["phone"].ID, 10)).Status(t, http.StatusNotFound)
	app.As(phone).Get("/api/v1/sessions").Status(t, http.StatusOK)

	// Sign the phone out from the web session.
	app.As(web).Delete("/api/v1/sessions/"+strconv.FormatInt(byDevice["phone"].ID, 10)).Status(t, http.StatusNoContent)
	app.As(phone).Get("/api/v1/sessions").Status(t, http.StatusUnauthorized)

	// Everywhere else: only the web session survives.
	app.As(web).Post("/api/v1/sessions/revoke-others", nil).Status(t, http.StatusNoContent)
	app.As(user).Get("/api/v1/sessions").Status(t, http.StatusUnauthorized)
	app.As(web).Get("/api/v1/sessions").Status(t, http.StatusOK).Data(t, &sessions)
	if len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("after revoke-others = %+v, want only the current session", sessions)
	}

	if n := countRows(t, app, `SELECT count(*) FROM activity_log
		WHERE action = 'auth.session_revoked' AND actor_id = $1`, user.ID); n != 2 {
		t.Fatalf("auth.session_revoked rows = %d, want 2", n)
	}

	// Platform accounts (no tenant) use the same routes.
	var mine []sessionRow
	app.As(app.SuperAdmin(t)).Get("/api/v1/sessions").Status(t, http.StatusOK).Data(t, &mine)
	if len(mine) == 0 {
		t.Fatal("the super admin has no sessions listed")
	}
}

func TestPruneTokensDropsExpiredOnes(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	ctx := context.Background()
	if _, err := app.Pool.Exec(ctx, `UPDATE personal_access_tokens SET expires_at = now() - interval '1 minute'
		WHERE tokenable_id = $1`, user.ID); err != nil {
		t.Fatalf("expire tokens: %v", err)
	}
	deleted, err := app.Deps.Platform.Sessions.PruneTokens(ctx, 0)
	if err != nil || deleted < 1 {
		t.Fatalf("PruneTokens() = %d, %v; want at least the expired token", deleted, err)
	}
	if n := countRows(t, app, `SELECT count(*) FROM personal_access_tokens WHERE tokenable_id = $1`, user.ID); n != 0 {
		t.Fatalf("tokens left = %d, want 0", n)
	}
}

func TestNewSignInAlert(t *testing.T) {
	t.Setenv("HTTP_TRUSTED_PROXIES", "192.0.2.0/24") // httptest's peer: X-Forwarded-For is honoured
	app := testkit.New(t)
	user := app.User(t)
	ctx := context.Background()
	alerts := func() int {
		return countRows(t, app, `SELECT count(*) FROM deliveries WHERE template = 'new-sign-in' AND user_id = $1`, user.ID)
	}

	// Off by default.
	signIn(t, app, user.Email, "web", "198.51.100.7")
	if n := alerts(); n != 0 {
		t.Fatalf("alerts with the setting off = %d", n)
	}

	if err := app.Deps.Platform.Settings.Set(ctx, tenancy.Scope{}, "security.new_sign_in_alert", true); err != nil {
		t.Fatalf("enable the alert: %v", err)
	}
	signIn(t, app, user.Email, "web", "198.51.100.7") // a known IP: no alert
	if n := alerts(); n != 0 {
		t.Fatalf("alerts for a known IP = %d", n)
	}
	signIn(t, app, user.Email, "laptop", "203.0.113.24") // a new IP: one alert
	if n := alerts(); n != 1 {
		t.Fatalf("alerts for a new IP = %d, want 1", n)
	}
	signIn(t, app, user.Email, "laptop", "203.0.113.24") // now known
	if n := alerts(); n != 1 {
		t.Fatalf("alerts after the IP became known = %d, want 1", n)
	}
}
