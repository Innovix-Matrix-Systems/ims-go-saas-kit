package files

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
)

func uploadHeader(t *testing.T, field, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/", &buffer)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse multipart: %v", err)
	}
	return request.MultipartForm.File[field][0]
}

var pngBytes = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64))

func TestFileResourceAndAvatarReplacement(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "files", "personal_access_tokens", "role_has_permissions", "model_has_roles", "users", "roles", "permissions", "branches", "tenants")
	ctx := context.Background()

	for _, statement := range []string{
		`INSERT INTO tenants (id, name, code) VALUES (1, 'Tenant One', 'TEN1')`,
		`INSERT INTO branches (id, tenant_id, name) VALUES (1, 1, 'Main Branch')`,
		`INSERT INTO users (id, tenant_id, branch_id, name, email, password) VALUES (1, 1, 1, 'User One', 'u1@example.com', 'x')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
	dbtest.SyncSequence(t, pool, "users")

	publicDir := t.TempDir()
	cfg := config.Config{
		App:     config.App{URL: "https://api.test"},
		Storage: config.Storage{PublicDir: publicDir, PrivateDir: t.TempDir()},
	}
	registry, err := storage.New(&cfg, nil)
	if err != nil {
		t.Fatalf("storage.New() error = %v", err)
	}
	tx := database.NewTxManager(&database.Conn{Name: "test", Driver: "pgsql", Pool: pool})
	service := NewService(NewRepository(queries), registry, storage.DiskPublic, tx, nil)

	// Library file: type mapping, disk write, URL, unattached flag.
	pdf := uploadHeader(t, "file", "report.pdf", []byte("%PDF-1.4 hello"))
	file, err := service.Create(ctx, 1, 1, pdf)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if file.Type != TypeDocument || file.Attached() {
		t.Fatalf("created file = %+v", file)
	}
	if _, err := os.Stat(filepath.Join(publicDir, filepath.FromSlash(file.Path))); err != nil {
		t.Fatalf("stored file missing on disk: %v", err)
	}
	if url := service.URL(ctx, *file); url != "https://api.test/storage/"+file.Path {
		t.Fatalf("URL() = %q", url)
	}

	// Disallowed extension is rejected.
	if _, err := service.Create(ctx, 1, 1, uploadHeader(t, "file", "payload.exe", []byte("MZ"))); err == nil {
		t.Fatal("Create(.exe) = nil error")
	}

	// Avatar: single-file collection with atomic replacement.
	if err := service.UpdateAvatar(ctx, 1, 1, 1, uploadHeader(t, "avatar", "me.png", pngBytes)); err != nil {
		t.Fatalf("UpdateAvatar() error = %v", err)
	}
	firstPhoto, err := service.repo.GetByOwner(ctx, OwnerUser, 1, CollectionProfile)
	if err != nil {
		t.Fatalf("GetByOwner() error = %v", err)
	}
	if url, err := service.PhotoURL(ctx, 1); err != nil || !strings.HasPrefix(url, "https://api.test/storage/") {
		t.Fatalf("PhotoURL() = %q, %v", url, err)
	}

	if err := service.UpdateAvatar(ctx, 1, 1, 1, uploadHeader(t, "avatar", "me2.png", pngBytes)); err != nil {
		t.Fatalf("UpdateAvatar(replace) error = %v", err)
	}
	photos, err := service.PhotoURLs(ctx, []int64{1})
	if err != nil || len(photos) != 1 {
		t.Fatalf("PhotoURLs() = %v, %v", photos, err)
	}
	if _, err := os.Stat(filepath.Join(publicDir, filepath.FromSlash(firstPhoto.Path))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded avatar file not removed: %v", err)
	}

	var profileRows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM files WHERE owner_type = 'user' AND owner_id = 1 AND collection = 'profile'").Scan(&profileRows); err != nil {
		t.Fatal(err)
	}
	if profileRows != 1 {
		t.Fatalf("profile rows = %d, want 1", profileRows)
	}

	// Non-image avatars are rejected.
	if err := service.UpdateAvatar(ctx, 1, 1, 1, uploadHeader(t, "avatar", "notes.txt", []byte("hello"))); err == nil {
		t.Fatal("UpdateAvatar(.txt) = nil error")
	}

	// Deleting an attached file frees its owner.
	current, err := service.repo.GetByOwner(ctx, OwnerUser, 1, CollectionProfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, current.ID, true, 0); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := service.repo.GetByOwner(ctx, OwnerUser, 1, CollectionProfile); !isNotFound(err) {
		t.Fatalf("GetByOwner() after delete = %v, want not found", err)
	}
	if url, _ := service.PhotoURL(ctx, 1); url != "" {
		t.Fatalf("PhotoURL() after delete = %q, want empty", url)
	}
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "RESOURCE_NOT_FOUND")
}

// TestFilesTenantIsolation proves the tenant-isolation contract: a tenant-bound
// caller cannot read, list or delete another tenant's files even with the
// granular file permissions, while a tenantless system caller (all_tenants)
// can. See docs/history/codebase-review.md C3.
func TestFilesTenantIsolation(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "files", "personal_access_tokens", "role_has_permissions", "model_has_roles", "users", "roles", "permissions", "branches", "tenants")
	ctx := context.Background()

	for _, statement := range []string{
		`INSERT INTO tenants (id, name, code) VALUES (1, 'Tenant One', 'TEN1'), (2, 'Tenant Two', 'TEN2')`,
		`INSERT INTO users (id, tenant_id, branch_id, name, email, password) VALUES (1, 1, NULL, 'User One', 'u1@example.com', 'x'), (2, 2, NULL, 'User Two', 'u2@example.com', 'x')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
	dbtest.SyncSequence(t, pool, "users")

	cfg := config.Config{App: config.App{URL: "https://api.test"}, Storage: config.Storage{PublicDir: t.TempDir(), PrivateDir: t.TempDir()}}
	registry, err := storage.New(&cfg, nil)
	if err != nil {
		t.Fatalf("storage.New() error = %v", err)
	}
	tx := database.NewTxManager(&database.Conn{Name: "test", Driver: "pgsql", Pool: pool})
	service := NewService(NewRepository(queries), registry, storage.DiskPublic, tx, nil)

	tenantTwoFile, err := service.Create(ctx, 2, 2, uploadHeader(t, "file", "secret.pdf", []byte("%PDF-1.4 tenant two")))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Tenant One caller: scoped reads and deletes.
	if _, err := service.Get(ctx, tenantTwoFile.ID, false, 1); !isNotFound(err) {
		t.Fatalf("Get(cross-tenant) = %v, want not found", err)
	}
	if _, _, err := service.List(ctx, ListFilter{AllTenants: false, TenantID: 1}); err != nil {
		t.Fatalf("List() error = %v", err)
	} else if total, err := service.repo.Count(ctx, ListFilter{AllTenants: false, TenantID: 1}); err != nil || total != 0 {
		t.Fatalf("Count(tenant 1) = %d, %v; want 0 (no cross-tenant rows)", total, err)
	}
	if err := service.Delete(ctx, tenantTwoFile.ID, false, 1); !isNotFound(err) {
		t.Fatalf("Delete(cross-tenant) = %v, want not found", err)
	}

	// System caller (all_tenants): full visibility.
	if _, err := service.Get(ctx, tenantTwoFile.ID, true, 0); err != nil {
		t.Fatalf("Get(all tenants) error = %v", err)
	}
	if err := service.Delete(ctx, tenantTwoFile.ID, true, 0); err != nil {
		t.Fatalf("Delete(all tenants) error = %v", err)
	}
}
