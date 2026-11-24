package auth_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func tokenFrom(t *testing.T, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil || parsed.Query().Get("token") == "" {
		t.Fatalf("link without a token: %q", link)
	}
	return parsed.Query().Get("token")
}

func TestInvitationPreview(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t, testkit.Name("Rafi Admin"))
	app.As(admin).Post("/api/v1/user", map[string]any{
		"name": "Mina Invitee", "email": "mina@acme.test", "send_invitation": true,
	}).Status(t, http.StatusCreated)
	token := tokenFrom(t, app.Queued(t, "invitation")["link"].(string))

	var preview struct {
		Name             string `json:"name"`
		Email            string `json:"email"`
		OrganizationName string `json:"organization_name"`
		InviterName      string `json:"inviter_name"`
	}
	app.Guest().Post("/api/v1/invitation/preview", map[string]any{"email": "mina@acme.test", "token": token}).
		Status(t, http.StatusOK).Data(t, &preview)
	if preview.Name != "Mina Invitee" || preview.InviterName != "Rafi Admin" || preview.OrganizationName == "" {
		t.Fatalf("preview = %+v", preview)
	}
	// The preview does not use the link up; a wrong address learns nothing.
	app.Guest().Post("/api/v1/invitation/preview", map[string]any{"email": "someone@else.test", "token": token}).
		Status(t, http.StatusUnprocessableEntity)
	app.Guest().Post("/api/v1/invitation/accept", map[string]any{
		"email": "mina@acme.test", "token": token, "password": "a long passphrase", "password_confirmation": "a long passphrase",
	}).Status(t, http.StatusOK)
	app.Guest().Post("/api/v1/invitation/preview", map[string]any{"email": "mina@acme.test", "token": token}).
		Status(t, http.StatusUnprocessableEntity)
}

func TestChangeOwnEmail(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t, testkit.Email("old@acme.test"))
	taken := app.User(t, testkit.Email("taken@acme.test"))

	app.As(user).Post("/api/v1/user/profile/change-email", map[string]any{"password": "wrong-password", "email": "new@acme.test"}).
		Status(t, http.StatusUnprocessableEntity)
	app.As(user).Post("/api/v1/user/profile/change-email", map[string]any{"password": testkit.Password, "email": taken.Email}).
		Status(t, http.StatusUnprocessableEntity)
	app.As(user).Post("/api/v1/user/profile/change-email", map[string]any{"password": testkit.Password, "email": "new@acme.test"}).
		Status(t, http.StatusAccepted)

	// Nothing changes until the link is opened.
	app.As(user).Get("/api/v1/user/profile").Status(t, http.StatusOK)
	token := tokenFrom(t, app.Queued(t, "email-change")["link"].(string))
	app.Guest().Post("/api/v1/email/change/confirm", map[string]any{"token": token}).Status(t, http.StatusOK)

	// Signed out everywhere; the new address signs in, the old one does not.
	app.As(user).Get("/api/v1/user/profile").Status(t, http.StatusUnauthorized)
	app.Login(t, "new@acme.test", testkit.Password)
	app.Guest().Post("/api/v1/login", map[string]any{"email": "old@acme.test", "password": testkit.Password, "device": "t"}).
		Status(t, http.StatusUnauthorized)
	if notice := app.Queued(t, "email-changed"); notice["new_email"] == nil {
		t.Fatalf("old address not told: %v", notice)
	}
	// The change is in the activity log, with both addresses masked.
	var changes string
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT changes::text FROM activity_log WHERE action = 'user.email_changed' AND subject_id = $1`, user.ID).Scan(&changes); err != nil {
		t.Fatalf("user.email_changed entry: %v", err)
	}
	if strings.Contains(changes, "old@acme.test") || strings.Contains(changes, "new@acme.test") {
		t.Errorf("the entry keeps an address unmasked: %s", changes)
	}
	app.Guest().Post("/api/v1/email/change/confirm", map[string]any{"token": token}).Status(t, http.StatusUnprocessableEntity)
}

func TestProfileLocale(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	var profile struct {
		Name   string  `json:"name"`
		Locale *string `json:"locale"`
	}
	app.As(user).Get("/api/v1/user/profile").Status(t, http.StatusOK).Data(t, &profile)
	if profile.Locale != nil {
		t.Fatalf("locale = %q, want none", *profile.Locale)
	}
	app.As(user).Post("/api/v1/user/profile/update", map[string]any{"name": profile.Name, "locale": "bn"}).
		Status(t, http.StatusOK).Data(t, &profile)
	if profile.Locale == nil || *profile.Locale != "bn" {
		t.Fatalf("locale after update = %v", profile.Locale)
	}
	app.As(user).Post("/api/v1/user/profile/update", map[string]any{"name": profile.Name}).Status(t, http.StatusOK).Data(t, &profile)
	if profile.Locale == nil || *profile.Locale != "bn" {
		t.Fatal("an update without locale must keep it")
	}
}
