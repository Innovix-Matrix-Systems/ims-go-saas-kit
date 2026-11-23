package tenants_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type organization struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	IsOwner bool   `json:"is_owner"`
	Owner   *struct {
		ID int64 `json:"id"`
	} `json:"owner"`
}

func makeOwner(t *testing.T, app *testkit.App, tenantID, userID int64) {
	t.Helper()
	if _, err := app.Pool.Exec(context.Background(), `UPDATE tenants SET owner_user_id = $1 WHERE id = $2`, userID, tenantID); err != nil {
		t.Fatal(err)
	}
}

func TestOrganizationProfileAndOwnership(t *testing.T) {
	app := testkit.New(t)
	tenantID := int64(testkit.DefaultTenant)
	owner := app.User(t)
	admin := app.User(t)
	member := app.User(t, testkit.Role(app.Role(t, "Member")))
	makeOwner(t, app, tenantID, owner.ID)

	var org organization
	app.As(member).Get("/api/v1/organization").Status(t, http.StatusOK).Data(t, &org)
	if org.ID != tenantID || org.IsOwner || org.Owner == nil || org.Owner.ID != owner.ID {
		t.Fatalf("member view = %+v", org)
	}
	app.As(member).Patch("/api/v1/organization", map[string]any{"name": "Nope"}).Status(t, http.StatusForbidden)
	app.As(admin).Patch("/api/v1/organization", map[string]any{"name": "Acme Digital", "phone": "+8801700000000"}).
		Status(t, http.StatusOK).Data(t, &org)
	if org.Name != "Acme Digital" {
		t.Fatalf("name = %q", org.Name)
	}

	// The owner is protected until ownership moves.
	app.As(admin).Delete(fmt.Sprintf("/api/v1/user/%d", owner.ID)).Status(t, http.StatusForbidden)
	app.As(admin).Patch(fmt.Sprintf("/api/v1/user/%d", owner.ID), map[string]any{"name": "Owner", "email": owner.Email, "is_active": false}).
		Status(t, http.StatusForbidden)
	app.As(admin).Delete(fmt.Sprintf("/api/v1/user/%d/two-factor", owner.ID)).Status(t, http.StatusForbidden)
	app.As(owner).Post("/api/v1/user/privacy/deletion", map[string]any{"password": testkit.Password}).
		Status(t, http.StatusConflict)

	// Only the owner transfers, with their password, to a member who can manage the organization.
	app.As(admin).Post("/api/v1/organization/transfer-ownership", map[string]any{"user_id": admin.ID, "password": testkit.Password}).
		Status(t, http.StatusForbidden)
	app.As(owner).Post("/api/v1/organization/transfer-ownership", map[string]any{"user_id": admin.ID, "password": "wrong-password"}).
		Status(t, http.StatusUnprocessableEntity)
	app.As(owner).Post("/api/v1/organization/transfer-ownership", map[string]any{"user_id": member.ID, "password": testkit.Password}).
		Status(t, http.StatusUnprocessableEntity)
	app.As(owner).Post("/api/v1/organization/transfer-ownership", map[string]any{"user_id": admin.ID, "password": testkit.Password}).
		Status(t, http.StatusOK).Data(t, &org)
	if org.Owner == nil || org.Owner.ID != admin.ID {
		t.Fatalf("owner after transfer = %+v", org.Owner)
	}
	// The former owner is now an ordinary admin.
	app.As(admin).Delete(fmt.Sprintf("/api/v1/user/%d", owner.ID)).Status(t, http.StatusNoContent)

	// Another organization sees only itself.
	other := app.User(t, testkit.InTenant(app.Tenant(t, "Other")))
	app.As(other).Get("/api/v1/organization").Status(t, http.StatusOK).Data(t, &org)
	if org.ID == tenantID {
		t.Fatal("cross-tenant organization read")
	}
}

// The organization carries its e-mail branding (logo, colour) for clients to
// show as its mark; unset values are null.
func TestOrganizationBranding(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)

	var out struct {
		Branding struct {
			LogoURL *string `json:"logo_url"`
			Color   *string `json:"color"`
		} `json:"branding"`
	}
	app.As(admin).Patch("/api/v1/settings/tenant/branding.logo_url", map[string]any{"value": "https://example.com/logo.png"}).Status(t, http.StatusOK)
	app.As(admin).Patch("/api/v1/settings/tenant/branding.color", map[string]any{"value": "#0f766e"}).Status(t, http.StatusOK)
	app.As(admin).Get("/api/v1/organization").Status(t, http.StatusOK).Data(t, &out)
	if out.Branding.LogoURL == nil || *out.Branding.LogoURL != "https://example.com/logo.png" || out.Branding.Color == nil || *out.Branding.Color != "#0f766e" {
		t.Fatalf("branding = %+v", out.Branding)
	}

	app.As(admin).Patch("/api/v1/settings/tenant/branding.logo_url", map[string]any{"value": ""}).Status(t, http.StatusOK)
	app.As(admin).Get("/api/v1/organization").Status(t, http.StatusOK).Data(t, &out)
	if out.Branding.LogoURL != nil {
		t.Fatalf("an empty logo is null, got %q", *out.Branding.LogoURL)
	}
}
