package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	cachememory "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

func setupExchange(t *testing.T) (context.Context, *ExchangeService, *dataprocessing.Service, *pgxpool.Pool, *config.Config) {
	t.Helper()
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"notification_deliveries", "notification_reads", "notification_targets", "notifications",
		"files", "data_processing_jobs",
		"personal_access_tokens", "role_has_permissions", "model_has_roles", "users", "roles", "permissions",
		"branches", "tenants",
	)
	ctx := context.Background()

	for _, statement := range []string{
		`INSERT INTO tenants (id, name, code) VALUES (1, 'Tenant One', 'TEN1')`,
		`INSERT INTO branches (id, tenant_id, name) VALUES (1, 1, 'Main Branch')`,
		`INSERT INTO users (id, tenant_id, branch_id, name, email, password) VALUES (1, 1, 1, 'Actor', 'actor@example.com', 'x')`,
		`INSERT INTO roles (id, tenant_id, name) VALUES (1, 1, 'Tenant Role')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
	dbtest.SyncSequence(t, pool, "users")

	cfg := &config.Config{
		App:        config.App{Name: "test", URL: "https://api.test"},
		Storage:    config.Storage{PublicDir: t.TempDir(), PrivateDir: t.TempDir()},
		Processing: config.Processing{Disk: "private", BasePath: "jobs"},
		Auth:       config.Auth{DefaultPassword: "password123"},
	}
	registry, err := storage.New(cfg, nil)
	if err != nil {
		t.Fatalf("storage.New() error = %v", err)
	}
	processing := dataprocessing.NewService(dataprocessing.NewRepository(queries)).
		WithFiles(registry, cfg.Processing)

	identityRepo := NewRepository(queries)
	roleRepo := access.NewRoleRepository(queries)
	store := cachememory.New("identity-exchange-")
	t.Cleanup(store.Close)
	checker := authz.NewChecker(queries, store)
	users := NewService(identityRepo, roleRepo, branches.NewRepository(queries), checker, nil, cfg.Auth, nil)

	queueRegistry := queue.NewRegistry()
	engine := queue.NewSyncEngine(queueRegistry)
	exchange := NewExchangeService(users, identityRepo, roleRepo, processing, registry, engine, cfg, slog.Default())
	if err := exchange.RegisterJobs(queueRegistry); err != nil {
		t.Fatalf("RegisterJobs() error = %v", err)
	}
	return ctx, exchange, processing, pool, cfg
}

func buildWorkbook(t *testing.T, rows [][]any) []byte {
	t.Helper()
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	for index, row := range rows {
		cell := fmt.Sprintf("A%d", index+1)
		if err := workbook.SetSheetRow(sheet, cell, &row); err != nil {
			t.Fatalf("SetSheetRow: %v", err)
		}
	}
	buffer := new(bytes.Buffer)
	if err := workbook.Write(buffer); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buffer.Bytes()
}

func uploadFile(t *testing.T, field, filename string, content []byte) *multipart.FileHeader {
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

func TestUserImportReportsPerRowErrors(t *testing.T) {
	ctx, exchange, processing, pool, _ := setupExchange(t)

	content := buildWorkbook(t, [][]any{
		{"name", "email", "phone", "password", "status", "roles"},
		{"Valid User", "valid@example.com", "+8801700000000", "Imported-Pass-42", "Active", "Tenant Role"},
		{"", "not-an-email", "", "", "", "Unknown Role"},
		{"Default Password", "default@example.com", "", "", "Active", "Tenant Role"},
	})
	header := uploadFile(t, "file", "users.xlsx", content)

	job, err := exchange.StartImport(ctx, tenancy.Scope{TenantID: 1}, 1, header)
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}

	// The sync engine executed the import inline.
	finished, err := processing.Get(ctx, tenancy.Scope{Internal: true}, job.ID)
	if err != nil {
		t.Fatalf("processing.Get() error = %v", err)
	}
	if finished.Status != dataprocessing.StatusCompleted {
		t.Fatalf("job status = %q, want completed", finished.Status)
	}
	if finished.TotalItems != 3 || finished.SuccessCount != 2 || finished.ErrorCount != 1 {
		t.Fatalf("counters: total=%d success=%d errors=%d payload=%s", finished.TotalItems, finished.SuccessCount, finished.ErrorCount, finished.Errors)
	}

	var rowErrors []map[string]any
	if err := json.Unmarshal(finished.Errors, &rowErrors); err != nil {
		t.Fatalf("decode row errors: %v", err)
	}
	if len(rowErrors) != 1 {
		t.Fatalf("row errors = %+v", rowErrors)
	}
	if row := rowErrors[0]["row"]; row != 3.0 {
		t.Fatalf("error row = %v, want 3 (header is row 1)", row)
	}

	var imported int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE tenant_id = 1 AND email LIKE '%@example.com' AND id <> 1`).Scan(&imported); err != nil {
		t.Fatal(err)
	}
	if imported != 2 {
		t.Fatalf("imported users = %d, want 2", imported)
	}

	// Roles are matched by name and assigned.
	var roleAssignments int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM model_has_roles mr
		JOIN users u ON u.id = mr.model_id
		WHERE mr.model_type = 'user' AND u.email = 'valid@example.com'`).Scan(&roleAssignments); err != nil {
		t.Fatal(err)
	}
	if roleAssignments != 1 {
		t.Fatalf("role assignments = %d, want 1", roleAssignments)
	}

	// The source file is removed after the run.
	if finished.FilePath != nil {
		disk, err := exchange.disks.Disk("private")
		if err != nil {
			t.Fatalf("disk: %v", err)
		}
		if exists, _ := disk.Exists(ctx, *finished.FilePath); exists {
			t.Fatalf("import source file still exists at %s", *finished.FilePath)
		}
	}
}

func TestUserExportProducesDownloadableWorkbook(t *testing.T) {
	ctx, exchange, processing, _, _ := setupExchange(t)

	job, err := exchange.StartExport(ctx, tenancy.Scope{TenantID: 1}, 1, ExportRequest{})
	if err != nil {
		t.Fatalf("StartExport() error = %v", err)
	}
	finished, err := processing.Get(ctx, tenancy.Scope{Internal: true}, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != dataprocessing.StatusCompleted {
		t.Fatalf("job status = %q, want completed", finished.Status)
	}
	if finished.FileName == nil || !strings.Contains(*finished.FileName, "user_export_") {
		t.Fatalf("job file name = %v", finished.FileName)
	}

	download, err := processing.OpenDownload(ctx, tenancy.Scope{TenantID: 1}, *finished.QueueJobID)
	if err != nil {
		t.Fatalf("OpenDownload() error = %v", err)
	}
	defer download.Reader.Close()

	workbook, err := excelize.OpenReader(download.Reader)
	if err != nil {
		t.Fatalf("open exported workbook: %v", err)
	}
	defer workbook.Close()
	rows, err := workbook.GetRows(workbook.GetSheetName(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("exported rows = %d, want heading + data", len(rows))
	}
	headings := strings.Join(rows[0], ",")
	if headings != "ID,Name,Email,Phone,Status,Roles,Branch,Created At" {
		t.Fatalf("export headings = %q", headings)
	}
	if rows[1][1] != "Actor" {
		t.Fatalf("exported first row = %+v", rows[1])
	}

	// A pending job has no downloadable artifact.
	pending, err := processing.Create(ctx, tenancy.Scope{Internal: true}, dataprocessing.TypeExport, nil, 0, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processing.OpenDownload(ctx, tenancy.Scope{Internal: true}, *pending.QueueJobID); err == nil {
		t.Fatal("OpenDownload(pending job) = nil error")
	}
}
