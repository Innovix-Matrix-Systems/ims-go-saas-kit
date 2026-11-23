package app_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

func TestDeveloperBackupsEndToEnd(t *testing.T) {
	pgDump := os.Getenv("BACKUP_PG_DUMP_PATH")
	if pgDump == "" {
		pgDump = "pg_dump"
	}
	if _, err := exec.LookPath(pgDump); err != nil {
		t.Skipf("%s is not installed; skipping backup integration test", pgDump)
	}

	t.Setenv("APP_STORAGE_DIR", t.TempDir())
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	t.Setenv("QUEUE_DRIVER", "sync")
	t.Setenv("BACKUP_ENABLED", "true")
	t.Setenv("BACKUP_DIR", t.TempDir())
	t.Setenv("BACKUP_DESTINATIONS", "local")
	t.Setenv("BACKUP_PG_DUMP_PATH", pgDump)

	kit := testkit.New(t)
	cfg := kit.Config
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config.Validate() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, pool := kit.Deps, kit.Pool

	ctx := context.Background()
	if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"backup-suite"}`, "", nil)
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

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/backups", "", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create backup status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			ID       int64  `json:"id"`
			Status   string `json:"status"`
			Filename string `json:"filename"`
			Size     int64  `json:"size_bytes"`
			Checksum string `json:"checksum_sha256"`
		} `json:"data"`
	}
	decodeBody(t, rec, &created)
	if created.Data.ID < 1 || created.Data.Status != "completed" || created.Data.Size == 0 || created.Data.Checksum == "" {
		t.Fatalf("created backup = %+v", created.Data)
	}
	runID := strconv.FormatInt(created.Data.ID, 10)

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/backups", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list backups status = %d body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Data []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	decodeBody(t, rec, &list)
	if len(list.Data) != 1 || list.Data[0].ID != created.Data.ID {
		t.Fatalf("backup list = %+v", list.Data)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/backups/stats", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("backup stats status = %d body = %s", rec.Code, rec.Body.String())
	}
	var stats struct {
		Data struct {
			TotalRuns      int64  `json:"total_runs"`
			TotalSize      int64  `json:"total_size"`
			SuccessfulRuns int64  `json:"successful_runs"`
			Status         string `json:"status"`
		} `json:"data"`
	}
	decodeBody(t, rec, &stats)
	if stats.Data.TotalRuns != 1 || stats.Data.TotalSize == 0 || stats.Data.SuccessfulRuns != 1 || stats.Data.Status != "ok" {
		t.Fatalf("backup stats = %+v", stats.Data)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/backups/config", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("backup config status = %d body = %s", rec.Code, rec.Body.String())
	}
	var configResponse struct {
		Data struct {
			PgDumpAvailable bool     `json:"pg_dump_available"`
			Destinations    []string `json:"destinations"`
		} `json:"data"`
	}
	decodeBody(t, rec, &configResponse)
	if !configResponse.Data.PgDumpAvailable || len(configResponse.Data.Destinations) != 1 || configResponse.Data.Destinations[0] != "local" {
		t.Fatalf("backup config = %+v", configResponse.Data)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/backups/"+runID+"/download", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("download backup status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/gzip" {
		t.Fatalf("download content type = %q", rec.Header().Get("Content-Type"))
	}
	assertBackupArchive(t, rec.Body.Bytes())

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/backups/clean", `{"dry_run":true}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("clean backup status = %d body = %s", rec.Code, rec.Body.String())
	}
	var clean struct {
		Data struct {
			DryRun     bool  `json:"dry_run"`
			Candidates int64 `json:"candidates"`
		} `json:"data"`
	}
	decodeBody(t, rec, &clean)
	if !clean.Data.DryRun || clean.Data.Candidates != 0 {
		t.Fatalf("clean preview = %+v", clean.Data)
	}

	rec = apiRequest(t, router, http.MethodDelete, "/dev/api/backups/"+runID, "", token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete backup status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/backups/"+runID, "", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted backup status = %d, want 404", rec.Code)
	}

	var actions int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM dev_actions WHERE action IN ('backups.create', 'backups.clean', 'backups.delete')`,
	).Scan(&actions); err != nil {
		t.Fatalf("count backup actions: %v", err)
	}
	if actions != 3 {
		t.Fatalf("backup audit actions = %d, want 3", actions)
	}
}

func assertBackupArchive(t *testing.T, data []byte) {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip archive: %v", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	entries := map[string]bool{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		entries[header.Name] = true
	}
	if !entries["manifest.json"] || !entries["database.dump"] {
		t.Fatalf("archive entries = %v, want manifest.json + database.dump", entries)
	}
}
