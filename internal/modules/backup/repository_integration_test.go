package backup

import (
	"context"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
)

func TestBackupRepository(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	ctx := context.Background()
	dbtest.Clean(t, pool, "backup_runs")

	repo := NewRepository(queries)
	first, err := repo.Create(ctx, CreateRunInput{
		Trigger:      TriggerCLI,
		Filename:     "ims-test-2026-10-21-120000.tar.gz",
		Scope:        ScopeDatabase,
		DatabaseName: "ims_test",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if first.Status != StatusPending || first.Trigger != TriggerCLI || first.Filename == "" {
		t.Fatalf("Create() = %+v", first)
	}

	loaded, err := repo.Get(ctx, first.ID)
	if err != nil || loaded.ID != first.ID {
		t.Fatalf("Get() = %+v, %v", loaded, err)
	}

	running, err := repo.SetRunning(ctx, first.ID)
	if err != nil || running.Status != StatusRunning || running.StartedAt == nil {
		t.Fatalf("SetRunning() = %+v, %v", running, err)
	}

	destinations := []DestinationResult{{
		Destination: DestinationLocal,
		Status:      StatusCompleted,
		Path:        "/tmp/backup.tar.gz",
		SizeBytes:   1234,
	}}
	completed, err := repo.Complete(ctx, first.ID, CompleteInput{
		Status:         StatusCompleted,
		Filename:       "ims-test-2026-10-21-120000.tar.gz",
		SizeBytes:      1234,
		ChecksumSHA256: "abc123",
		PgDumpVersion:  "17.1",
		DatabaseName:   "ims_test",
		Destinations:   destinations,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completed.Status != StatusCompleted || len(completed.Destinations) != 1 ||
		completed.Destinations[0].Path != "/tmp/backup.tar.gz" || completed.ChecksumSHA256 != "abc123" {
		t.Fatalf("Complete() = %+v", completed)
	}

	latest, err := repo.LatestSuccessful(ctx)
	if err != nil || latest.ID != first.ID {
		t.Fatalf("LatestSuccessful() = %+v, %v", latest, err)
	}

	stats, err := repo.Stats(ctx)
	if err != nil || stats.TotalRuns != 1 || stats.TotalSize != 1234 || stats.SuccessfulRuns != 1 || stats.LastSuccessAt == nil {
		t.Fatalf("Stats() = %+v, %v", stats, err)
	}

	second, err := repo.Create(ctx, CreateRunInput{
		Trigger:  TriggerSchedule,
		Filename: "ims-test-2026-10-21-130000.tar.gz",
		Scope:    ScopeDatabaseFiles,
	})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if _, err := repo.Fail(ctx, second.ID, "s3 unavailable", nil); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	list, err := repo.List(ctx, ListFilter{Limit: 10})
	if err != nil || len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	filtered, err := repo.List(ctx, ListFilter{Status: StatusCompleted, Limit: 10})
	if err != nil || len(filtered) != 1 || filtered[0].ID != first.ID {
		t.Fatalf("List(completed) = %+v, %v, want the first run", filtered, err)
	}
	page, err := repo.List(ctx, ListFilter{BeforeID: second.ID, Limit: 10})
	if err != nil || len(page) != 1 || page[0].ID != first.ID {
		t.Fatalf("List(before) = %+v, %v", page, err)
	}

	cleanup, err := repo.ForCleanup(ctx)
	if err != nil || len(cleanup) != 2 || cleanup[0].Filename == "" {
		t.Fatalf("ForCleanup() = %+v, %v", cleanup, err)
	}

	deleted, err := repo.SoftDelete(ctx, second.ID)
	if err != nil || !deleted {
		t.Fatalf("SoftDelete() = %v, %v", deleted, err)
	}
	if _, err := repo.Get(ctx, second.ID); err == nil {
		t.Fatal("Get() after SoftDelete() = nil error")
	}
}
