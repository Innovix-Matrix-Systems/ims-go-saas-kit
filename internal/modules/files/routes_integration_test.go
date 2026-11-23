package files_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
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
