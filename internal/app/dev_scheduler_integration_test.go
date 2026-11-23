package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

// TestDeveloperScheduler proves the Scheduler panel: every registered task
// with its next run, Run now in the background with the outcome recorded in
// schedule_runs (the task's last and recent runs, and the history), and the
// dev.manage gate on running.
func TestDeveloperScheduler(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	kit := testkit.New(t)
	deps := kit.Deps
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := seeder.Run(context.Background(), kit.Config, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"scheduler-suite"}`, "", nil)
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

	type task struct {
		ID      string  `json:"id"`
		Spec    string  `json:"spec"`
		NextRun *string `json:"next_run"`
		LastRun *struct {
			Status  string `json:"status"`
			Trigger string `json:"trigger"`
		} `json:"last_run"`
		Recorded bool `json:"recorded"`
		Recent   []struct {
			ID int64 `json:"id"`
		} `json:"recent"`
	}
	status := func() map[string]task {
		t.Helper()
		rec := apiRequest(t, router, http.MethodGet, "/dev/api/scheduler", "", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("scheduler status = %d body = %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data struct {
				Timezone string `json:"timezone"`
				Tasks    []task `json:"tasks"`
			} `json:"data"`
		}
		decodeBody(t, rec, &body)
		if body.Data.Timezone == "" {
			t.Fatal("scheduler timezone missing")
		}
		byID := map[string]task{}
		for _, item := range body.Data.Tasks {
			byID[item.ID] = item
		}
		return byID
	}

	tasks := status()
	heartbeat, ok := tasks["scheduler.heartbeat"]
	if !ok || heartbeat.Spec == "" || heartbeat.NextRun == nil {
		t.Fatalf("scheduler.heartbeat = %+v (found %v), want a spec and a next run", heartbeat, ok)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/scheduler/no.such.task/run", "", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("run unknown task status = %d, want 404", rec.Code)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/scheduler/notifications.purge-expired/run", "", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run task status = %d body = %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		last := status()["notifications.purge-expired"].LastRun
		if last != nil && last.Status == "ok" && last.Trigger == "manual" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("last run = %+v, want a finished manual run", last)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The run is in the history, filterable by task and status.
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/scheduler/runs?task=notifications.purge-expired&status=ok", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d body = %s", rec.Code, rec.Body.String())
	}
	var history struct {
		Data []struct {
			Task       string   `json:"task"`
			Trigger    string   `json:"trigger"`
			Status     string   `json:"status"`
			DurationMS *float64 `json:"duration_ms"`
		} `json:"data"`
	}
	decodeBody(t, rec, &history)
	if len(history.Data) != 1 || history.Data[0].Task != "notifications.purge-expired" || history.Data[0].Trigger != "manual" || history.Data[0].DurationMS == nil {
		t.Fatalf("history = %+v, want the one manual purge run with a duration", history.Data)
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/scheduler/runs?status=failed", "", token, nil)
	decodeBody(t, rec, &history)
	if rec.Code != http.StatusOK || len(history.Data) != 0 {
		t.Fatalf("failed runs = %d %+v, want none", rec.Code, history.Data)
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/scheduler/runs?status=sideways", "", token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status filter = %d, want 422", rec.Code)
	}
	tasks = status()
	if recent := tasks["notifications.purge-expired"].Recent; len(recent) != 1 {
		t.Fatalf("purge recent runs = %d, want 1", len(recent))
	}
	// The heartbeat beats every minute and keeps no history.
	if hb := tasks["scheduler.heartbeat"]; hb.Recorded || len(hb.Recent) != 0 {
		t.Fatalf("heartbeat = %+v, want no history", hb)
	}

	// A run another process has open (the `ims schedule` process) refuses a
	// second one; the same open run past the task's timeout was lost and no
	// longer blocks.
	ctx := context.Background()
	var openID int64
	if err := kit.Pool.QueryRow(ctx,
		"INSERT INTO schedule_runs (task, triggered_by, status) VALUES ('notifications.purge-expired', 'schedule', 'running') RETURNING id",
	).Scan(&openID); err != nil {
		t.Fatalf("open run: %v", err)
	}
	rec = apiRequest(t, router, http.MethodPost, "/dev/api/scheduler/notifications.purge-expired/run", "", token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("run while running = %d %s, want 409", rec.Code, rec.Body.String())
	}
	if _, err := kit.Pool.Exec(ctx, "UPDATE schedule_runs SET started_at = now() - interval '2 days' WHERE id = $1", openID); err != nil {
		t.Fatalf("age run: %v", err)
	}
	rec = apiRequest(t, router, http.MethodPost, "/dev/api/scheduler/notifications.purge-expired/run", "", token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run past a lost run = %d %s, want 202", rec.Code, rec.Body.String())
	}

	// Running a task changes things: viewers without dev.manage may not.
	rec = apiRequest(t, router, http.MethodPost, "/dev/api/scheduler/notifications.purge-expired/run", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous run status = %d, want 401", rec.Code)
	}
}
