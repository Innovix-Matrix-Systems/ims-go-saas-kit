package app_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func TestDeveloperFilesEndToEnd(t *testing.T) {
	t.Setenv("APP_STORAGE_DIR", t.TempDir())
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, pool := kit.Deps, kit.Pool

	ctx := context.Background()
	if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"files-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	token := login.Data.Token
	if token == "" {
		t.Fatal("developer login returned no token")
	}

	var tenantID, tenantUserID int64
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name, code) VALUES ('Acme', 'ACME') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, name, email, password) VALUES ($1, 'Acme Admin', 'admin@acme.test', 'x') RETURNING id`,
		tenantID,
	).Scan(&tenantUserID); err != nil {
		t.Fatalf("insert tenant user: %v", err)
	}

	imageBytes := []byte("\x89PNG\r\n\x1a\n" + "preview-bytes")
	image, err := deps.Platform.Files.Create(ctx, tenantUserID, tenantID, multipartHeader(t, "file", "alpha.png", imageBytes))
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	// The tenantless super admin uploads into Acme (an impersonated upload):
	// the dashboard must report the file's tenant, not the uploader's.
	var superAdminID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = 'superadmin@ims.com'`).Scan(&superAdminID); err != nil {
		t.Fatalf("super admin id: %v", err)
	}
	document, err := deps.Platform.Files.Create(ctx, superAdminID, tenantID, multipartHeader(t, "file", "notes.docx", []byte("PK-document-bytes")))
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/files?tenant_id="+strconv.FormatInt(tenantID, 10), "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list files status = %d body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Data []struct {
			ID          int64   `json:"id"`
			Name        string  `json:"name"`
			TenantID    *int64  `json:"tenant_id"`
			TenantName  *string `json:"tenant_name"`
			UploaderID  *int64  `json:"uploader_id"`
			PreviewKind *string `json:"preview_kind"`
		} `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	decodeBody(t, rec, &list)
	if list.Meta.Total != 2 || len(list.Data) != 2 {
		t.Fatalf("tenant list total = %d rows = %d, want 2/2", list.Meta.Total, len(list.Data))
	}
	for _, item := range list.Data {
		if item.TenantID == nil || *item.TenantID != tenantID || item.TenantName == nil || *item.TenantName != "Acme" {
			t.Fatalf("list item tenant = %+v, want Acme/%d", item, tenantID)
		}
		wantUploader := tenantUserID
		if item.ID == document.ID {
			wantUploader = superAdminID
		}
		if item.UploaderID == nil || *item.UploaderID != wantUploader {
			t.Fatalf("list item uploader = %+v, want %d", item.UploaderID, wantUploader)
		}
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/files?type=image", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list type status = %d body = %s", rec.Code, rec.Body.String())
	}
	decodeBody(t, rec, &list)
	if list.Meta.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != image.ID {
		t.Fatalf("image list = %+v, want only file %d", list, image.ID)
	}
	if list.Data[0].PreviewKind == nil || *list.Data[0].PreviewKind != "image" {
		t.Fatalf("image preview_kind = %v, want image", list.Data[0].PreviewKind)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/files/stats?tenant_id="+strconv.FormatInt(tenantID, 10), "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("file stats status = %d body = %s", rec.Code, rec.Body.String())
	}
	var stats struct {
		Data struct {
			TotalFiles    int64 `json:"total_files"`
			TotalSize     int64 `json:"total_size"`
			Unattached    int64 `json:"unattached_files"`
			TypeBreakdown []struct {
				Type  string `json:"type"`
				Files int64  `json:"files"`
			} `json:"by_type"`
		} `json:"data"`
	}
	decodeBody(t, rec, &stats)
	if stats.Data.TotalFiles != 2 || stats.Data.Unattached != 2 {
		t.Fatalf("file stats = %+v, want 2 unattached files", stats.Data)
	}

	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/files/"+strconv.FormatInt(image.ID, 10)+"/download?inline=1", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("inline download status = %d body = %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("inline content type = %q, want image/png", contentType)
	}
	if disposition := rec.Header().Get("Content-Disposition"); len(disposition) < 6 || disposition[:6] != "inline" {
		t.Fatalf("inline disposition = %q", disposition)
	}
	if !bytes.Equal(rec.Body.Bytes(), imageBytes) {
		t.Fatalf("inline body = %q, want %q", rec.Body.Bytes(), imageBytes)
	}

	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/files/"+strconv.FormatInt(document.ID, 10)+"/download?inline=1", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("document download status = %d body = %s", rec.Code, rec.Body.String())
	}
	if disposition := rec.Header().Get("Content-Disposition"); len(disposition) < 10 || disposition[:10] != "attachment" {
		t.Fatalf("document disposition = %q, want attachment", disposition)
	}

	rec = apiRequest(t, router, http.MethodDelete, "/dev/api/files/"+strconv.FormatInt(image.ID, 10), "", token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/files/"+strconv.FormatInt(image.ID, 10)+"/download", "", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted download status = %d, want 404", rec.Code)
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/files/stats?tenant_id="+strconv.FormatInt(tenantID, 10), "", token, nil)
	decodeBody(t, rec, &stats)
	if stats.Data.TotalFiles != 1 {
		t.Fatalf("stats after delete total = %d, want 1", stats.Data.TotalFiles)
	}

	var auditAction string
	if err := pool.QueryRow(ctx,
		`SELECT action FROM dev_actions WHERE action = 'files.delete' ORDER BY id DESC LIMIT 1`,
	).Scan(&auditAction); err != nil {
		t.Fatalf("read file delete audit: %v", err)
	}
	if auditAction != "files.delete" {
		t.Fatalf("audit action = %q, want files.delete", auditAction)
	}
}

func multipartHeader(t *testing.T, field, filename string, content []byte) *multipart.FileHeader {
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
