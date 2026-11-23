package app_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type processingJob struct {
	JobID     string  `json:"job_id"`
	Status    string  `json:"status"`
	FileName  *string `json:"file_name"`
	ExpiresAt *string `json:"expires_at"`
	ID        int64   `json:"id"`
}

func jobStatus(t *testing.T, app *testkit.App, user *testkit.User, jobID string) processingJob {
	t.Helper()
	var job processingJob
	app.As(user).Get("/api/v1/data-processing/job/"+jobID).Status(t, http.StatusOK).Data(t, &job)
	return job
}

// finish runs a queued activity export when the test queue did not run it
// already, and returns its state.
func finishActivityExport(t *testing.T, app *testkit.App, user *testkit.User, job processingJob) processingJob {
	t.Helper()
	if current := jobStatus(t, app, user, job.JobID); current.Status == "pending" {
		if err := app.Deps.Platform.ActivityExport.Process(context.Background(), job.ID); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}
	return jobStatus(t, app, user, job.JobID)
}

// An organization exports its activity log as a background job; staff export
// the platform log through the internal route; others cannot.
func TestActivityExportJobs(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	staff := app.SuperAdmin(t)
	app.As(admin).Post("/api/v1/role", map[string]any{"name": "Exported Role"}).Status(t, http.StatusCreated)

	var job processingJob
	app.As(admin).Post("/api/v1/activity/export", map[string]any{"action": "role.created"}).Status(t, http.StatusAccepted).Data(t, &job)
	done := finishActivityExport(t, app, admin, job)
	if done.Status != "completed" || done.FileName == nil || done.ExpiresAt == nil {
		t.Fatalf("export = %+v, want completed with a file and an expiry", done)
	}
	res := app.As(admin).Get("/api/v1/data-processing/job/"+job.JobID+"/download").Status(t, http.StatusOK)
	if res.Body.Len() == 0 {
		t.Fatal("empty export file")
	}

	var platformJob processingJob
	app.As(staff).Post("/api/v1/internal/activity/export", map[string]any{"tenant_id": "system"}).Status(t, http.StatusAccepted).Data(t, &platformJob)
	if done := finishActivityExport(t, app, staff, platformJob); done.Status != "completed" {
		t.Fatalf("platform export = %+v", done)
	}
	app.As(admin).Post("/api/v1/internal/activity/export", map[string]any{}).Status(t, http.StatusForbidden)

	member := app.User(t, testkit.Role(app.Role(t, "No Activity")))
	app.As(member).Post("/api/v1/activity/export", map[string]any{}).Status(t, http.StatusForbidden)
}

// A failed job is retried with the same input; finished jobs can be removed,
// running ones cannot.
func TestProcessingJobRetryAndRemove(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)

	var job processingJob
	app.As(admin).Post("/api/v1/activity/export", map[string]any{}).Status(t, http.StatusAccepted).Data(t, &job)
	finishActivityExport(t, app, admin, job)

	// A completed job cannot be retried.
	app.As(admin).Post("/api/v1/data-processing/job/"+job.JobID+"/retry", nil).Status(t, http.StatusConflict)

	if _, err := app.Pool.Exec(context.Background(), `UPDATE data_processing_jobs SET status = 'failed', error_message = 'boom' WHERE id = $1`, job.ID); err != nil {
		t.Fatal(err)
	}
	var retried processingJob
	app.As(admin).Post("/api/v1/data-processing/job/"+job.JobID+"/retry", nil).Status(t, http.StatusOK).Data(t, &retried)
	if retried.Status != "pending" && retried.Status != "completed" {
		t.Fatalf("retried status = %q", retried.Status)
	}
	if done := finishActivityExport(t, app, admin, job); done.Status != "completed" {
		t.Fatalf("after retry = %+v", done)
	}

	other := app.User(t, testkit.InTenant(app.Tenant(t, "Someone Else")))
	app.As(other).Delete("/api/v1/data-processing/job/"+job.JobID).Status(t, http.StatusNotFound)
	app.As(admin).Delete("/api/v1/data-processing/job/"+job.JobID).Status(t, http.StatusNoContent)
	app.As(admin).Get("/api/v1/data-processing/job/"+job.JobID).Status(t, http.StatusNotFound)
}

// Staff not acting in an organization retry and remove their own jobs through
// the internal routes; the organization routes refuse their writes.
func TestProcessingJobStaffRetryAndRemove(t *testing.T) {
	app := testkit.New(t)
	staff := app.SuperAdmin(t)
	admin := app.User(t)

	var job processingJob
	app.As(staff).Post("/api/v1/internal/activity/export", map[string]any{"tenant_id": "system"}).Status(t, http.StatusAccepted).Data(t, &job)
	finishActivityExport(t, app, staff, job)
	if _, err := app.Pool.Exec(context.Background(), `UPDATE data_processing_jobs SET status = 'failed' WHERE id = $1`, job.ID); err != nil {
		t.Fatal(err)
	}

	app.As(staff).Post("/api/v1/data-processing/job/"+job.JobID+"/retry", nil).Status(t, http.StatusForbidden)
	app.As(admin).Post("/api/v1/internal/data-processing/job/"+job.JobID+"/retry", nil).Status(t, http.StatusForbidden)
	app.As(staff).Post("/api/v1/internal/data-processing/job/"+job.JobID+"/retry", nil).Status(t, http.StatusOK)
	if done := finishActivityExport(t, app, staff, job); done.Status != "completed" {
		t.Fatalf("after retry = %+v", done)
	}
	app.As(staff).Delete("/api/v1/internal/data-processing/job/"+job.JobID).Status(t, http.StatusNoContent)
	app.As(staff).Get("/api/v1/data-processing/job/"+job.JobID).Status(t, http.StatusNotFound)
}

// An organization's own retention days decide when its jobs are deleted.
func TestProcessingRetentionPerOrganization(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	short := app.Tenant(t, "Short Retention")
	long := app.Tenant(t, "Default Retention")
	for _, statement := range []string{
		fmt.Sprintf(`INSERT INTO settings (key, value, target_type, target_id) VALUES ('data_processing_job.cleanup_days', '1'::jsonb, 'tenant', %d)`, short),
		fmt.Sprintf(`INSERT INTO data_processing_jobs (tenant_id, job_id, type, status, completed_at) VALUES (%d, 'short-old', 'export', 'completed', now() - interval '2 days')`, short),
		fmt.Sprintf(`INSERT INTO data_processing_jobs (tenant_id, job_id, type, status, completed_at) VALUES (%d, 'long-old', 'export', 'completed', now() - interval '2 days')`, long),
	} {
		if _, err := app.Pool.Exec(ctx, statement); err != nil {
			t.Fatalf("%v\n%s", err, statement)
		}
	}
	if _, err := app.Deps.Platform.Processing.CleanupCompleted(ctx, 30); err != nil {
		t.Fatal(err)
	}
	var remaining []string
	rows, err := app.Pool.Query(ctx, `SELECT job_id FROM data_processing_jobs WHERE job_id IN ('short-old', 'long-old') ORDER BY job_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		remaining = append(remaining, id)
	}
	if len(remaining) != 1 || remaining[0] != "long-old" {
		t.Fatalf("remaining = %v, want only long-old (its organization keeps 30 days)", remaining)
	}
}
