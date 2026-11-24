package dataprocessing_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type listedJob struct {
	JobID    string  `json:"job_id"`
	Type     string  `json:"type"`
	UserID   *int64  `json:"user_id"`
	UserName *string `json:"user_name"`
}

func TestJobOwnership(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	tenantID := app.Tenant(t, "Jobs Co")
	branch := app.DefaultBranch(t, tenantID)
	admin := app.User(t, testkit.InTenant(tenantID), testkit.Name("Ada Admin"))
	member := app.User(t, testkit.InTenant(tenantID), testkit.Name("Mo Member"))
	// A member without the job permissions.
	if _, err := app.Pool.Exec(ctx, `DELETE FROM model_has_roles WHERE model_type = 'user' AND model_id = $1`, member.ID); err != nil {
		t.Fatal(err)
	}

	job := func(owner *testkit.User, kind, id string) string {
		if _, err := app.Pool.Exec(ctx,
			`INSERT INTO data_processing_jobs (tenant_id, branch_id, job_id, type, status, user_id) VALUES ($1, $2, $3, $4, 'failed', $5)`,
			tenantID, branch, id, kind, owner.ID); err != nil {
			t.Fatal(err)
		}
		return id
	}
	adminJob := job(admin, "user_export", "own-admin-export")
	memberJob := job(member, "user_export", "own-member-export")
	memberPrivacy := job(member, "privacy_export", "own-member-privacy")

	list := func(user *testkit.User, path string) map[string]listedJob {
		var rows []listedJob
		app.As(user).Get(path).Status(t, http.StatusOK).Data(t, &rows)
		out := map[string]listedJob{}
		for _, row := range rows {
			out[row.JobID] = row
		}
		return out
	}

	// The admin sees every member's jobs, with who started them, but not a
	// member's personal-data export.
	all := list(admin, "/api/v1/data-processing/jobs?per_page=100")
	if _, ok := all[memberJob]; !ok {
		t.Fatalf("admin's list misses the member's job: %v", all)
	}
	if name := all[memberJob].UserName; name == nil || *name != "Mo Member" {
		t.Errorf("user_name = %v, want Mo Member", name)
	}
	if _, ok := all[memberPrivacy]; ok {
		t.Error("the admin's list shows a member's personal-data export")
	}
	// Filtered to one member.
	mine := list(admin, fmt.Sprintf("/api/v1/data-processing/jobs?per_page=100&user_id=%d", member.ID))
	if _, ok := mine[adminJob]; ok || len(mine) != 1 {
		t.Errorf("user_id filter: got %v, want the member's export only", mine)
	}
	// The admin may act on the member's job, not on their personal-data export.
	app.As(admin).Get("/api/v1/data-processing/job/"+memberJob).Status(t, http.StatusOK)
	app.As(admin).Get("/api/v1/data-processing/job/"+memberPrivacy).Status(t, http.StatusNotFound)

	// The member sees their own jobs only.
	app.As(member).Get("/api/v1/data-processing/jobs").Status(t, http.StatusForbidden)
	own := list(member, "/api/v1/data-processing/jobs/user?per_page=100")
	if _, ok := own[adminJob]; ok {
		t.Error("the member's list shows the admin's job")
	}
	if _, ok := own[memberJob]; !ok {
		t.Error("the member's list misses their own job")
	}
	app.As(member).Get("/api/v1/data-processing/job/"+adminJob).Status(t, http.StatusForbidden)
	app.As(member).Delete("/api/v1/data-processing/job/"+adminJob).Status(t, http.StatusForbidden)
}
