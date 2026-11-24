package privacy_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type privacyRequest struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	Source       string     `json:"source"`
	ScheduledFor *time.Time `json:"scheduled_for"`
}

func TestSelfServiceDeletion(t *testing.T) {
	app := testkit.New(t)
	app.User(t) // a second admin, so the user below is not the last one
	user := app.User(t)

	app.As(user).Post("/api/v1/user/privacy/deletion", map[string]any{"password": "wrong-password"}).Status(t, http.StatusUnprocessableEntity)
	app.As(user).Get("/api/v1/user/privacy/deletion").Status(t, http.StatusNotFound)

	var scheduled privacyRequest
	app.As(user).Post("/api/v1/user/privacy/deletion", map[string]any{"password": testkit.Password}).
		Status(t, http.StatusAccepted).Data(t, &scheduled)
	if scheduled.Type != "erasure" || scheduled.Status != "scheduled" || scheduled.Source != "self" || scheduled.ScheduledFor == nil {
		t.Fatalf("scheduled = %+v", scheduled)
	}
	if days := time.Until(*scheduled.ScheduledFor).Hours() / 24; days < 13.9 || days > 14.1 {
		t.Errorf("scheduled in %.1f days, want the 14-day grace period", days)
	}
	if date, _ := app.Queued(t, "privacy-deletion-scheduled")["date"].(string); date == "" {
		t.Error("the scheduled-deletion mail carries no date")
	}

	// Every session is signed out; signing in again still works, to cancel.
	app.As(user).Get("/api/v1/user/privacy/deletion").Status(t, http.StatusUnauthorized)
	user.Token = app.Login(t, user.Email, testkit.Password)

	// Asking twice returns the scheduled request (through the service: an
	// identical request right away is the duplicate-request guard's 409).
	again, err := app.Deps.Platform.Privacy.RequestDeletion(context.Background(), user.ID)
	if err != nil || again.ID != scheduled.ID {
		t.Errorf("asking twice: request %+v (%v), want the scheduled %d", again, err, scheduled.ID)
	}
	user.Token = app.Login(t, user.Email, testkit.Password)
	app.As(user).Get("/api/v1/user/privacy/deletion").Status(t, http.StatusOK)

	var cancelled privacyRequest
	app.As(user).Delete("/api/v1/user/privacy/deletion").Status(t, http.StatusOK).Data(t, &cancelled)
	if cancelled.Status != "cancelled" {
		t.Errorf("cancel status = %q", cancelled.Status)
	}
	app.As(user).Get("/api/v1/user/privacy/deletion").Status(t, http.StatusNotFound)
	if _, err := app.Deps.Platform.Privacy.CancelDeletion(context.Background(), user.ID); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Errorf("cancel with nothing scheduled: err = %v, want RESOURCE_NOT_FOUND", err)
	}
}

func TestScheduledErasureRunsWhenDue(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	app.User(t)
	user := app.User(t)
	var scheduled privacyRequest
	app.As(user).Post("/api/v1/user/privacy/deletion", map[string]any{"password": testkit.Password}).
		Status(t, http.StatusAccepted).Data(t, &scheduled)

	service := app.Deps.Platform.Privacy
	if erased, err := service.EraseDue(ctx); err != nil || erased != 0 {
		t.Fatalf("before the grace period: erased %d (%v), want 0", erased, err)
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE privacy_requests SET scheduled_for = now() - interval '1 minute' WHERE id = $1`, scheduled.ID); err != nil {
		t.Fatal(err)
	}
	if erased, err := service.EraseDue(ctx); err != nil || erased != 1 {
		t.Fatalf("after the grace period: erased %d (%v), want 1", erased, err)
	}
	var status, source string
	var erasedAt *time.Time
	if err := app.Pool.QueryRow(ctx, `SELECT r.status, r.source, u.erased_at FROM privacy_requests r JOIN users u ON u.id = r.user_id WHERE r.id = $1`, scheduled.ID).
		Scan(&status, &source, &erasedAt); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || source != "self" || erasedAt == nil {
		t.Errorf("after the run: status %q, source %q, erased_at %v", status, source, erasedAt)
	}
	app.Queued(t, "privacy-account-erased")
}

func TestDueErasureRespectsCancelAndRecoversStuckRuns(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	service := app.Deps.Platform.Privacy
	app.User(t)
	cancelled := app.User(t)
	stuck := app.User(t)

	// Cancelled after the task listed it: the run must not erase.
	request, err := service.RequestDeletion(ctx, cancelled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CancelDeletion(ctx, cancelled.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Erase(ctx, privacy.EraseInput{UserID: cancelled.ID, RequestID: request.ID}); !apperr.Is(err, apperr.InvalidState) {
		t.Errorf("erasing a cancelled request: err = %v, want INVALID_STATE", err)
	}
	var status string
	var erasedAt *time.Time
	if err := app.Pool.QueryRow(ctx, `SELECT r.status, u.erased_at FROM privacy_requests r JOIN users u ON u.id = r.user_id WHERE r.id = $1`, request.ID).
		Scan(&status, &erasedAt); err != nil || status != "cancelled" || erasedAt != nil {
		t.Errorf("after the refused run: status %q, erased_at %v (%v), want cancelled and not erased", status, erasedAt, err)
	}

	// Left "processing" by a process that died: the next run finishes it.
	request, err = service.RequestDeletion(ctx, stuck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE privacy_requests SET status = 'processing', updated_at = now() - interval '2 hours' WHERE id = $1`, request.ID); err != nil {
		t.Fatal(err)
	}
	if erased, err := service.EraseDue(ctx); err != nil || erased != 1 {
		t.Fatalf("EraseDue = %d (%v), want the stuck erasure finished", erased, err)
	}
	if err := app.Pool.QueryRow(ctx, `SELECT status FROM privacy_requests WHERE id = $1`, request.ID).Scan(&status); err != nil || status != "completed" {
		t.Errorf("stuck request status = %q (%v), want completed", status, err)
	}
}

func TestAdminErasure(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	target := app.User(t)

	// Not on yourself, not without the permission, not across tenants.
	app.As(admin).Post(fmt.Sprintf("/api/v1/user/%d/erase", admin.ID), nil).Status(t, http.StatusForbidden)
	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer", "user.view")))
	app.As(viewer).Post(fmt.Sprintf("/api/v1/user/%d/erase", target.ID), nil).Status(t, http.StatusForbidden)
	otherTenant := app.Tenant(t, "Other Tenant")
	outsider := app.User(t, testkit.InTenant(otherTenant))
	app.As(outsider).Post(fmt.Sprintf("/api/v1/user/%d/erase", target.ID), map[string]any{"immediate": true}).Status(t, http.StatusNotFound)

	// Outside the tenant everything is a 404, whatever the user's state:
	// no "super admin" 403 or "last Admin" 409 to probe with.
	app.As(outsider).Post(fmt.Sprintf("/api/v1/user/%d/erase", app.SuperAdmin(t).ID), nil).Status(t, http.StatusNotFound)
	app.As(outsider).Post(fmt.Sprintf("/api/v1/user/%d/privacy-export", target.ID), nil).Status(t, http.StatusNotFound)

	var scheduled privacyRequest
	app.As(admin).Post(fmt.Sprintf("/api/v1/user/%d/erase", target.ID), nil).Status(t, http.StatusAccepted).Data(t, &scheduled)
	if scheduled.Status != "scheduled" || scheduled.Source != "admin" {
		t.Errorf("scheduled = %+v", scheduled)
	}

	// The target cannot cancel what an admin scheduled; the admin can.
	target.Token = app.Login(t, target.Email, testkit.Password)
	app.As(target).Delete("/api/v1/user/privacy/deletion").Status(t, http.StatusForbidden)
	app.As(outsider).Delete(fmt.Sprintf("/api/v1/user/%d/erase", target.ID)).Status(t, http.StatusNotFound)
	app.As(admin).Delete(fmt.Sprintf("/api/v1/user/%d/erase", target.ID)).Status(t, http.StatusOK)
	app.As(admin).Post(fmt.Sprintf("/api/v1/user/%d/erase", target.ID), map[string]any{}).Status(t, http.StatusAccepted).Data(t, &scheduled)

	var erased privacyRequest
	app.As(admin).Post(fmt.Sprintf("/api/v1/user/%d/erase", target.ID), map[string]any{"immediate": true}).
		Status(t, http.StatusOK).Data(t, &erased)
	if erased.ID != scheduled.ID || erased.Status != "completed" {
		t.Errorf("immediate erasure = %+v, want the scheduled request completed", erased)
	}
	app.As(admin).Post(fmt.Sprintf("/api/v1/user/%d/erase", target.ID), map[string]any{"immediate": true}).Status(t, http.StatusConflict)
}

func TestExportOverHTTP(t *testing.T) {
	t.Setenv("AUTH_DATA_EXPORT_URL", "https://app.example.test/settings/privacy?export={job}")
	app := testkit.New(t)
	ctx := context.Background()
	// Exporting one's own data needs no permission at all.
	user := app.User(t, testkit.Role(app.Role(t, "No Permissions")))
	other := app.User(t) // an Admin with data-processing access

	var job struct {
		JobID string `json:"job_id"`
	}
	app.As(user).Post("/api/v1/user/privacy/export", map[string]any{"password": testkit.Password}).
		Status(t, http.StatusAccepted).Data(t, &job)
	if job.JobID == "" {
		t.Fatal("no job id")
	}
	// Once a day (asked through the service: an identical request right
	// away is the duplicate-request guard's 409, not the daily limit).
	if _, err := app.Deps.Platform.Privacy.StartExport(ctx, tenancy.Scope{Internal: true},
		privacy.ExportInput{UserID: user.ID, Source: privacy.SourceSelf, RequestedBy: user.ID}); !apperr.Is(err, apperr.RateLimitExceeded) {
		t.Errorf("second export today: err = %v, want RATE_LIMIT_EXCEEDED", err)
	}

	// The worker's part (no River worker runs in tests).
	var requestID int64
	if err := app.Pool.QueryRow(ctx, `SELECT id FROM privacy_requests WHERE user_id = $1 AND type = 'export'`, user.ID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if err := app.Deps.Platform.Privacy.ProcessExport(ctx, requestID); err != nil {
		t.Fatalf("process export: %v", err)
	}
	// The mail opens the client app, which downloads for the signed-in owner:
	// a browser following a link to the API itself sends no token.
	if link, _ := app.Queued(t, "privacy-export-ready")["url"].(string); link != "https://app.example.test/settings/privacy?export="+job.JobID {
		t.Errorf("export mail link = %q, want the privacy page with the job", link)
	}
	var own struct {
		Data []struct {
			JobID string `json:"job_id"`
			Type  string `json:"type"`
		} `json:"data"`
	}
	app.As(user).Get("/api/v1/data-processing/jobs/user?type=privacy_export").Status(t, http.StatusOK).JSON(t, &own)
	if len(own.Data) != 1 || own.Data[0].JobID != job.JobID {
		t.Errorf("own job list = %+v, want the export", own.Data)
	}

	download := fmt.Sprintf("/api/v1/data-processing/job/%s/download", job.JobID)
	app.As(other).Get(download).Status(t, http.StatusNotFound)
	body := app.As(user).Get(download).Status(t, http.StatusOK).Body.Bytes()
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("download is not a zip: %v", err)
	}
	found := false
	for _, file := range archive.File {
		found = found || file.Name == "identity/profile.json"
	}
	if !found {
		t.Error("the downloaded archive has no identity/profile.json")
	}

	// Other users' exports are not in the job lists either.
	var listed struct {
		Data []struct {
			Type string `json:"type"`
		} `json:"data"`
	}
	app.As(app.SuperAdmin(t)).InTenant(testkit.DefaultTenant, app.DefaultBranch(t, testkit.DefaultTenant)).
		Get("/api/v1/data-processing/jobs?per_page=100").Status(t, http.StatusOK).JSON(t, &listed)
	for _, item := range listed.Data {
		if item.Type == "privacy_export" {
			t.Error("another user's privacy export is listed to an admin")
		}
	}

	// An admin's export of the user belongs to the admin.
	var adminJob struct {
		JobID string `json:"job_id"`
	}
	app.As(other).Post(fmt.Sprintf("/api/v1/user/%d/privacy-export", user.ID), nil).Status(t, http.StatusAccepted).Data(t, &adminJob)
	app.As(user).Get(fmt.Sprintf("/api/v1/data-processing/job/%s", adminJob.JobID)).Status(t, http.StatusNotFound)

	// Erasing the user deletes their export archives too.
	var exports int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM data_processing_jobs WHERE type = 'privacy_export'`).Scan(&exports); err != nil || exports != 2 {
		t.Fatalf("export jobs = %d (%v), want 2", exports, err)
	}
	app.As(other).Post(fmt.Sprintf("/api/v1/user/%d/erase", user.ID), map[string]any{"immediate": true}).Status(t, http.StatusOK)
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM data_processing_jobs WHERE type = 'privacy_export'`).Scan(&exports); err != nil || exports != 0 {
		t.Errorf("export jobs after erasure = %d (%v), want 0", exports, err)
	}
}

func TestReplayErasesRestoredUsers(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	app.User(t)
	user := app.User(t)
	service := app.Deps.Platform.Privacy

	app.As(app.SuperAdmin(t)).Post(fmt.Sprintf("/api/v1/internal/user/%d/erase", user.ID), map[string]any{"immediate": true}).Status(t, http.StatusOK)
	ids, err := service.ErasedSince(ctx, time.Now().Add(-time.Hour))
	if err != nil || len(ids) != 1 || ids[0] != user.ID {
		t.Fatalf("ledger = %v (%v), want [%d]", ids, err, user.ID)
	}

	// A restore brings the person back.
	if _, err := app.Pool.Exec(ctx, `UPDATE users SET name = 'Restored Person', email = 'restored@privacy.test', erased_at = NULL, deleted_at = NULL WHERE id = $1`, user.ID); err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Replay(ctx, append(ids, 999999))
	if err != nil || replayed != 1 {
		t.Fatalf("replay = %d (%v), want 1", replayed, err)
	}
	assertNoPersonalData(t, app.Pool, "restored@privacy.test", "Restored Person")
	if replayed, err := service.Replay(ctx, ids); err != nil || replayed != 0 {
		t.Errorf("second replay = %d (%v), want 0", replayed, err)
	}
}
