package files_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type uploaded struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

// upload posts a small PDF to path as client and returns the response.
func upload(t *testing.T, client *testkit.Client, path string) *testkit.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "report.pdf")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("%PDF-1.4 testkit")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return client.Header("Content-Type", writer.FormDataContentType()).Post(path, body.Bytes())
}

// fileTenant reads the stored tenant of a file (nil = global).
func fileTenant(t *testing.T, app *testkit.App, id int64) *int64 {
	t.Helper()
	var tenantID *int64
	if err := app.Pool.QueryRow(context.Background(), `SELECT tenant_id FROM files WHERE id = $1`, id).Scan(&tenantID); err != nil {
		t.Fatalf("read file %d tenant: %v", id, err)
	}
	return tenantID
}

// The tenant family stamps the effective tenant: the caller's own, or the
// acting tenant when the super admin impersonates. Without acting headers a
// platform upload is refused instead of silently becoming global.
func TestTenantUploadStampsEffectiveTenant(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	super := app.SuperAdmin(t)
	tenantB := app.Tenant(t, "Tenant B")

	var own uploaded
	upload(t, app.As(admin), "/api/v1/files").Status(t, http.StatusCreated).Data(t, &own)
	if got := fileTenant(t, app, own.ID); got == nil || *got != testkit.DefaultTenant {
		t.Fatalf("tenant admin upload tenant = %v, want %d", got, testkit.DefaultTenant)
	}

	var acting uploaded
	upload(t, app.As(super).InTenant(tenantB, app.DefaultBranch(t, tenantB)), "/api/v1/files").
		Status(t, http.StatusCreated).Data(t, &acting)
	if got := fileTenant(t, app, acting.ID); got == nil || *got != tenantB {
		t.Fatalf("impersonated upload tenant = %v, want %d", got, tenantB)
	}

	upload(t, app.As(super), "/api/v1/files").Status(t, http.StatusForbidden)
}

// The internal family uploads global files and reads across tenants; tenant
// accounts never reach it, and tenant reads never see another tenant's file.
func TestInternalFilesAreGlobalAndPlatformOnly(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	super := app.SuperAdmin(t)
	adminB := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))

	var global uploaded
	upload(t, app.As(super), "/api/v1/internal/files").Status(t, http.StatusCreated).Data(t, &global)
	if got := fileTenant(t, app, global.ID); got != nil {
		t.Fatalf("internal upload tenant = %d, want global (NULL)", *got)
	}

	var own uploaded
	upload(t, app.As(admin), "/api/v1/files").Status(t, http.StatusCreated).Data(t, &own)

	// Internal reads span every tenant.
	app.As(super).Get(fmt.Sprintf("/api/v1/internal/files/%d", own.ID)).Status(t, http.StatusOK)
	var all []uploaded
	app.As(super).Get("/api/v1/internal/files").Status(t, http.StatusOK).Data(t, &all)
	if len(all) != 2 {
		t.Fatalf("internal list = %d files, want 2", len(all))
	}

	// Tenant accounts are refused on the internal family.
	upload(t, app.As(admin), "/api/v1/internal/files").Status(t, http.StatusForbidden)
	app.As(admin).Get("/api/v1/internal/files").Status(t, http.StatusForbidden)

	// Tenant reads see only their own tenant: no global file, no tenant A file for B.
	var listed []uploaded
	app.As(admin).Get("/api/v1/files").Status(t, http.StatusOK).Data(t, &listed)
	if len(listed) != 1 || listed[0].ID != own.ID {
		t.Fatalf("tenant list = %+v, want only file %d", listed, own.ID)
	}
	app.As(admin).Get(fmt.Sprintf("/api/v1/files/%d", global.ID)).Status(t, http.StatusNotFound)
	app.As(adminB).Get(fmt.Sprintf("/api/v1/files/%d", own.ID)).Status(t, http.StatusNotFound)
	app.As(adminB).Delete(fmt.Sprintf("/api/v1/files/%d", own.ID)).Status(t, http.StatusNotFound)

	// A tenant admin deletes their tenant's file; the super admin deletes the global one internally.
	app.As(admin).Delete(fmt.Sprintf("/api/v1/files/%d", own.ID)).Status(t, http.StatusNoContent)
	app.As(super).Delete(fmt.Sprintf("/api/v1/internal/files/%d", global.ID)).Status(t, http.StatusNoContent)
}

// uploadLogo posts a small PNG as the organization's logo.
func uploadLogo(t *testing.T, client *testkit.Client, name string) *testkit.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("logo", name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("\x89PNG\r\n\x1a\n" + string(make([]byte, 64)))); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return client.Header("Content-Type", writer.FormDataContentType()).Post("/api/v1/organization/logo", body.Bytes())
}

// An organization uploads one logo at a time: a public image listed with its
// files, replaced by the next upload, invisible to other organizations, and
// only for members who may edit the organization.
func TestOrganizationLogo(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	other := app.User(t, testkit.InTenant(app.Tenant(t, "Logo Other")))

	var first, second uploaded
	uploadLogo(t, app.As(admin), "logo.png").Status(t, http.StatusCreated).Data(t, &first)
	if first.URL == "" || first.URL[:4] != "http" {
		t.Fatalf("the logo has an absolute public URL, got %q", first.URL)
	}
	if tenant := fileTenant(t, app, first.ID); tenant == nil || *tenant != testkit.DefaultTenant {
		t.Fatalf("the logo belongs to the organization, tenant = %v", tenant)
	}
	uploadLogo(t, app.As(admin), "logo2.png").Status(t, http.StatusCreated).Data(t, &second)

	var listed []struct {
		ID       int64 `json:"id"`
		Attached bool  `json:"attached"`
	}
	app.As(admin).Get("/api/v1/files?attached=true").Status(t, http.StatusOK).Data(t, &listed)
	logos := 0
	for _, file := range listed {
		if file.ID == first.ID {
			t.Fatal("the replaced logo is gone")
		}
		if file.ID == second.ID {
			logos++
		}
	}
	if logos != 1 {
		t.Fatalf("the new logo is listed with the files: %+v", listed)
	}
	app.As(other).Get(fmt.Sprintf("/api/v1/files/%d", second.ID)).Status(t, http.StatusNotFound)

	// The upload is the organization's logo: its own view and the staff list show it.
	var org struct {
		Branding struct {
			LogoURL *string `json:"logo_url"`
		} `json:"branding"`
	}
	app.As(admin).Get("/api/v1/organization").Status(t, http.StatusOK).Data(t, &org)
	if org.Branding.LogoURL == nil || *org.Branding.LogoURL != second.URL {
		t.Fatalf("organization logo = %v, want %s", org.Branding.LogoURL, second.URL)
	}
	var tenants []struct {
		ID       int64 `json:"id"`
		Branding *struct {
			LogoURL *string `json:"logo_url"`
		} `json:"branding"`
	}
	app.As(app.SuperAdmin(t)).Get("/api/v1/tenant?per_page=100").Status(t, http.StatusOK).Data(t, &tenants)
	seen := false
	for _, tenant := range tenants {
		if tenant.ID == testkit.DefaultTenant {
			seen = tenant.Branding != nil && tenant.Branding.LogoURL != nil && *tenant.Branding.LogoURL == second.URL
		}
	}
	if !seen {
		t.Fatalf("the staff list shows the logo: %+v", tenants)
	}

	viewer := app.User(t, testkit.Role(app.Role(t, "Logo Viewer", "file.view.all")))
	uploadLogo(t, app.As(viewer), "logo.png").Status(t, http.StatusForbidden)
}
