package onboarding_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func signUpBody(email string) map[string]any {
	return map[string]any{
		"name": "Nadia Islam", "email": email, "password": "a long passphrase",
		"organization_name": "Acme Digital", "phone": "+8801711000001", "accept_terms": true,
	}
}

func TestSignUpCreatesOrganizationAndUnverifiedOwner(t *testing.T) {
	app := testkit.New(t)
	var created struct {
		Message string `json:"message"`
	}
	app.Guest().Post("/api/v1/signup", signUpBody("nadia@acme.test")).Status(t, http.StatusAccepted).Data(t, &created)
	if created.Message == "" {
		t.Fatal("sign-up answered without a message")
	}

	var tenantID, ownerID int64
	var verified *string
	if err := app.Pool.QueryRow(context.Background(), `SELECT u.tenant_id, u.id, u.email_verified_at::text FROM users u
		WHERE u.email = 'nadia@acme.test'`).Scan(&tenantID, &ownerID, &verified); err != nil {
		t.Fatalf("owner not created: %v", err)
	}
	if verified != nil {
		t.Fatal("the owner must verify the address before signing in")
	}
	var roleName string
	if err := app.Pool.QueryRow(context.Background(), `SELECT r.name FROM roles r
		JOIN model_has_roles m ON m.role_id = r.id WHERE m.model_id = $1`, ownerID).Scan(&roleName); err != nil || roleName != "Admin" {
		t.Fatalf("owner role = %q, %v; want Admin", roleName, err)
	}
	var subscriptions, agreements int
	_ = app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM subscriptions WHERE tenant_id = $1 AND status IN ('active','trial')`, tenantID).Scan(&subscriptions)
	_ = app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM user_agreements WHERE user_id = $1`, ownerID).Scan(&agreements)
	if subscriptions != 1 || agreements != 2 {
		t.Fatalf("subscriptions = %d, agreements = %d; want 1 and 2", subscriptions, agreements)
	}

	// Signing in waits for the verified address.
	app.Guest().Post("/api/v1/login", map[string]any{"email": "nadia@acme.test", "password": "a long passphrase", "device": "t"}).
		Status(t, http.StatusForbidden)
	link, _ := app.Queued(t, "email-verify")["link"].(string)
	parsed, err := url.Parse(link)
	if err != nil || parsed.Query().Get("token") == "" {
		t.Fatalf("verification link = %q", link)
	}
	app.Guest().Post("/api/v1/email/verify", map[string]any{"token": parsed.Query().Get("token")}).Status(t, http.StatusOK)
	token := app.Login(t, "nadia@acme.test", "a long passphrase")
	owner := &testkit.User{ID: ownerID, Email: "nadia@acme.test", Token: token}

	var progress struct {
		Completed bool   `json:"completed"`
		IsOwner   bool   `json:"is_owner"`
		Source    string `json:"source"`
	}
	app.As(owner).Get("/api/v1/onboarding").Status(t, http.StatusOK).Data(t, &progress)
	if progress.Completed || !progress.IsOwner || progress.Source != "signup" {
		t.Fatalf("progress = %+v", progress)
	}
	app.As(owner).Post("/api/v1/onboarding/complete", map[string]any{"skipped_steps": []string{"invite"}}).
		Status(t, http.StatusOK).Data(t, &progress)
	if !progress.Completed {
		t.Fatal("setup not completed")
	}
	app.As(owner).Post("/api/v1/onboarding/complete", map[string]any{"skipped_steps": []string{"nonsense"}}).
		Status(t, http.StatusUnprocessableEntity)

	// Another organization's member sees their own organization, never this one.
	other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))
	app.As(other).Get("/api/v1/onboarding").Status(t, http.StatusNotFound)
	app.As(other).Post("/api/v1/onboarding/complete", map[string]any{}).Status(t, http.StatusNotFound)
}

func TestSignUpNeverRevealsExistingAccounts(t *testing.T) {
	app := testkit.New(t)
	existing := app.User(t, testkit.Email("taken@acme.test"))
	app.Guest().Post("/api/v1/signup", signUpBody(existing.Email)).Status(t, http.StatusAccepted)

	var tenants int
	_ = app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM tenants WHERE name = 'Acme Digital'`).Scan(&tenants)
	if tenants != 0 {
		t.Fatalf("an organization was created for an existing address (%d)", tenants)
	}
	data := app.Queued(t, "signup-account-exists")
	if data["login_url"] == nil || data["reset_url"] == nil {
		t.Fatalf("account-exists mail data = %v", data)
	}
}

func TestSignUpRefusals(t *testing.T) {
	app := testkit.New(t)
	body := signUpBody("refused@acme.test")
	body["accept_terms"] = false
	app.Guest().Post("/api/v1/signup", body).Status(t, http.StatusUnprocessableEntity)

	body = signUpBody("refused@acme.test")
	body["password"] = "password123"
	app.Guest().Post("/api/v1/signup", body).Status(t, http.StatusUnprocessableEntity)

	// A configured package that does not exist: unavailable, nothing created.
	staff := app.SuperAdmin(t)
	app.As(staff).Patch("/api/v1/internal/settings/signup.package_id", map[string]any{"value": 999999}).Status(t, http.StatusOK)
	app.Guest().Post("/api/v1/signup", signUpBody("refused@acme.test")).Status(t, http.StatusServiceUnavailable)

	app.As(staff).Patch("/api/v1/internal/settings/signup.enabled", map[string]any{"value": false}).Status(t, http.StatusOK)
	app.Guest().Post("/api/v1/signup", signUpBody("refused@acme.test")).Status(t, http.StatusForbidden)

	var users int
	_ = app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email = 'refused@acme.test'`).Scan(&users)
	if users != 0 {
		t.Fatalf("a refused sign-up created a user (%d)", users)
	}
}
