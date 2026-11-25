package dataprocessing

import (
	"context"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

func TestJobLifecycleAndCleanup(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "data_processing_jobs", "personal_access_tokens", "role_has_permissions", "model_has_roles", "users", "roles", "permissions", "branches", "tenants")

	service := NewService(NewRepository(queries))
	ctx := context.Background()
	scope := tenancy.Scope{Internal: true}

	job, err := service.Create(ctx, scope, TypeExport, strPtr("users"), 10, nil, 1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if job.Status != StatusPending || job.ProgressPercentage() != 0 {
		t.Fatalf("created job = %+v", job)
	}

	if err := service.MarkProcessing(ctx, job.ID); err != nil {
		t.Fatalf("MarkProcessing() error = %v", err)
	}
	if err := service.UpdateProgress(ctx, job.ID, 10, 5, 4, []map[string]any{{"row": 3, "message": "invalid"}}); err != nil {
		t.Fatalf("UpdateProgress() error = %v", err)
	}

	running, err := service.Get(ctx, scope, job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if running.Status != StatusProcessing || running.ProgressPercentage() != 50 {
		t.Fatalf("running job = %+v (progress %d)", running, running.ProgressPercentage())
	}
	if running.ErrorCount != 1 {
		t.Fatalf("error count = %d, want 1", running.ErrorCount)
	}

	if err := service.AttachFile(ctx, job.ID, FileInput{
		Name: "users.xlsx", Disk: "private", Path: "jobs/export/users.xlsx", Size: 2048, Mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}); err != nil {
		t.Fatalf("AttachFile() error = %v", err)
	}
	if err := service.MarkCompleted(ctx, job.ID); err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}

	finished, err := service.Get(ctx, scope, job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if finished.Status != StatusCompleted || finished.CompletedAt == nil || finished.FileName == nil {
		t.Fatalf("finished job = %+v", finished)
	}

	// Retention 0: completed jobs older than "now" are eligible.
	count, err := service.CountCompleted(ctx, 0)
	if err != nil || count != 1 {
		t.Fatalf("CountCompleted() = %d, %v; want 1", count, err)
	}
	deleted, err := service.CleanupCompleted(ctx, 0)
	if err != nil || deleted != 1 {
		t.Fatalf("CleanupCompleted() = %d, %v; want 1", deleted, err)
	}

	// Invalid type rejected.
	if _, err := service.Create(ctx, scope, "nonsense", nil, 0, nil, 1); err == nil {
		t.Fatal("Create(invalid type) = nil error")
	}
}

func strPtr(value string) *string { return &value }
