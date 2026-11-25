package app_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// queuedLocale is the language a queued delivery of a template was written in.
func queuedLocale(t *testing.T, app *testkit.App, template string) string {
	t.Helper()
	var locale string
	err := app.Pool.QueryRow(context.Background(), `SELECT coalesce(args->'payload'->'message'->'to'->>'locale', '') FROM river_job
		WHERE args->>'kind' = 'delivery.send' AND args->'payload'->'message'->>'template' = $1
		ORDER BY id DESC LIMIT 1`, template).Scan(&locale)
	if err != nil {
		t.Fatalf("no queued %q delivery: %v", template, err)
	}
	return locale
}

// problemTitle is the title of a 404 problem, in the request's language.
func problemTitle(t *testing.T, client *testkit.Client) string {
	t.Helper()
	var problem struct {
		Title string `json:"title"`
	}
	client.Get("/api/v1/branch/987654321").Status(t, http.StatusNotFound).JSON(t, &problem)
	return problem.Title
}

// TestLanguageLookupOrder: Accept-Language → the account's language → the
// organization's default → APP_LOCALE for API answers, and the account's →
// the organization's for e-mails (internal/app/locale.go).
func TestLanguageLookupOrder(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t, testkit.Name("Rafi Admin"))

	english := problemTitle(t, app.As(admin))
	if english != "Not Found" {
		t.Fatalf("no language anywhere: title = %q, want the APP_LOCALE (English) text", english)
	}

	// Only offered languages are accepted.
	app.As(admin).Patch("/api/v1/settings/tenant/general.default_locale", map[string]any{"value": "zz"}).
		Status(t, http.StatusUnprocessableEntity)
	app.As(admin).Patch("/api/v1/settings/tenant/general.default_locale", map[string]any{"value": "bn"}).
		Status(t, http.StatusOK)

	var organization struct {
		Language *string `json:"language"`
	}
	app.As(admin).Get("/api/v1/organization").Status(t, http.StatusOK).Data(t, &organization)
	if organization.Language == nil || *organization.Language != "bn" {
		t.Fatalf("organization language = %v, want bn", organization.Language)
	}

	bangla := problemTitle(t, app.As(admin))
	if bangla == english || bangla == "" {
		t.Errorf("organization default bn: title = %q, want the Bangla text", bangla)
	}
	if got := problemTitle(t, app.As(admin).Header("Accept-Language", "en")); got != english {
		t.Errorf("Accept-Language: en wins over the organization: title = %q", got)
	}

	// A new member's invitation is written in the organization's language.
	app.As(admin).Post("/api/v1/user", map[string]any{
		"name": "Mina Invitee", "email": "mina@acme.test", "send_invitation": true,
	}).Status(t, http.StatusCreated)
	if got := queuedLocale(t, app, "invitation"); got != "bn" {
		t.Errorf("invitation language = %q, want the organization's (bn)", got)
	}

	// The account's own choice beats the organization's.
	app.As(admin).Post("/api/v1/user/profile/update", map[string]any{"locale": "en"}).Status(t, http.StatusOK)
	if got := problemTitle(t, app.As(admin)); got != english {
		t.Errorf("account language en over organization bn: title = %q", got)
	}
}
