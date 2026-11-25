package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// TestDeveloperPlaygroundEndToEnd proves the local-only Playground can enqueue
// synthetic queue jobs and that they are visible through the queue admin.
func TestDeveloperPlaygroundEndToEnd(t *testing.T) {
	router, token := playgroundRouter(t, "local")

	rec := apiRequest(t, router, http.MethodGet, "/dev/api/me", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev me status = %d body = %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Data struct {
			Playground bool `json:"playground"`
		} `json:"data"`
	}
	decodeBody(t, rec, &me)
	if !me.Data.Playground {
		t.Fatalf("me.playground = false in a local environment")
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/playground", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("playground info status = %d body = %s", rec.Code, rec.Body.String())
	}
	var info struct {
		Data struct {
			Enabled     bool     `json:"enabled"`
			QueueDriver string   `json:"queue_driver"`
			JobKind     string   `json:"job_kind"`
			Queues      []string `json:"queues"`
		} `json:"data"`
	}
	decodeBody(t, rec, &info)
	if !info.Data.Enabled || info.Data.JobKind != "dev.test" || info.Data.QueueDriver != "river" || len(info.Data.Queues) != 3 {
		t.Fatalf("playground info = %+v", info.Data)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/playground/jobs",
		`{"kind":"dev.test","queue":"default","count":2,"sleep_ms":0,"tag":"e2e"}`, token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("enqueue playground jobs status = %d body = %s", rec.Code, rec.Body.String())
	}
	var enqueued struct {
		Data struct {
			Enqueued int    `json:"enqueued"`
			Kind     string `json:"kind"`
			Queue    string `json:"queue"`
			Driver   string `json:"driver"`
		} `json:"data"`
	}
	decodeBody(t, rec, &enqueued)
	if enqueued.Data.Enqueued != 2 || enqueued.Data.Kind != "dev.test" || enqueued.Data.Driver != "river" {
		t.Fatalf("enqueue result = %+v", enqueued.Data)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/queue/jobs?kind=dev.test&limit=10", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue jobs status = %d body = %s", rec.Code, rec.Body.String())
	}
	var jobs struct {
		Data []struct {
			Kind  string `json:"kind"`
			Queue string `json:"queue"`
			State string `json:"state"`
		} `json:"data"`
	}
	decodeBody(t, rec, &jobs)
	if len(jobs.Data) < 2 {
		t.Fatalf("dev.test jobs = %+v, want at least 2", jobs.Data)
	}
	for _, job := range jobs.Data {
		if job.Kind != "dev.test" || job.Queue != "default" {
			t.Fatalf("queued job = %+v", job)
		}
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/playground/jobs",
		`{"kind":"user.export"}`, token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("enqueue user export status = %d body = %s", rec.Code, rec.Body.String())
	}
	var exportResult struct {
		Data struct {
			Enqueued        int     `json:"enqueued"`
			Kind            string  `json:"kind"`
			Queue           string  `json:"queue"`
			ProcessingJobID *int64  `json:"processing_job_id"`
			JobID           *string `json:"job_id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &exportResult)
	if exportResult.Data.Enqueued != 1 || exportResult.Data.Kind != "user.export" ||
		exportResult.Data.Queue != "heavy" || exportResult.Data.ProcessingJobID == nil ||
		exportResult.Data.JobID == nil || *exportResult.Data.JobID == "" {
		t.Fatalf("user export result = %+v", exportResult.Data)
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/queue/jobs?kind=user.export&limit=5", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user export queue status = %d body = %s", rec.Code, rec.Body.String())
	}
	var exportJobs struct {
		Data []struct {
			Kind  string `json:"kind"`
			Queue string `json:"queue"`
		} `json:"data"`
	}
	decodeBody(t, rec, &exportJobs)
	if len(exportJobs.Data) == 0 || exportJobs.Data[0].Kind != "user.export" || exportJobs.Data[0].Queue != "heavy" {
		t.Fatalf("user export queue jobs = %+v", exportJobs.Data)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/playground/jobs",
		`{"kind":"mail.send","to":"playground@example.test","subject":"Playground test","text":"hello"}`, token, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("enqueue test mail status = %d body = %s", rec.Code, rec.Body.String())
	}
	var mailResult struct {
		Data struct {
			Enqueued int    `json:"enqueued"`
			Kind     string `json:"kind"`
			Queue    string `json:"queue"`
		} `json:"data"`
	}
	decodeBody(t, rec, &mailResult)
	if mailResult.Data.Enqueued != 1 || mailResult.Data.Kind != "mail.send" || mailResult.Data.Queue != "default" {
		t.Fatalf("mail result = %+v", mailResult.Data)
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/queue/jobs?kind=mail.send&limit=5", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("mail queue status = %d body = %s", rec.Code, rec.Body.String())
	}
	var mailJobs struct {
		Data []struct {
			Kind  string `json:"kind"`
			Queue string `json:"queue"`
		} `json:"data"`
	}
	decodeBody(t, rec, &mailJobs)
	if len(mailJobs.Data) == 0 || mailJobs.Data[0].Kind != "mail.send" {
		t.Fatalf("mail queue jobs = %+v", mailJobs.Data)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/playground/jobs",
		`{"kind":"mail.send","subject":"missing recipient"}`, token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mail without recipient status = %d, want 422", rec.Code)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/playground/jobs",
		`{"kind":"dev.test","queue":"branch","count":1}`, token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported playground queue status = %d, want 422", rec.Code)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/playground/jobs",
		`{"kind":"nope"}`, token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported playground kind status = %d, want 422", rec.Code)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/actions", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("actions status = %d body = %s", rec.Code, rec.Body.String())
	}
	var actions struct {
		Data []struct {
			Action string `json:"action"`
		} `json:"data"`
	}
	decodeBody(t, rec, &actions)
	audited := false
	for _, action := range actions.Data {
		if action.Action == "playground.jobs" {
			audited = true
		}
	}
	if !audited {
		t.Fatalf("actions = %+v, want playground.jobs", actions.Data)
	}
}

// TestDeveloperPlaygroundLocalOnly proves the Playground API is closed outside
// local/development while the developer dashboard itself remains reachable.
func TestDeveloperPlaygroundLocalOnly(t *testing.T) {
	router, token := playgroundRouter(t, "staging")

	rec := apiRequest(t, router, http.MethodGet, "/dev/api/me", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev me status = %d body = %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Data struct {
			Playground bool `json:"playground"`
		} `json:"data"`
	}
	decodeBody(t, rec, &me)
	if me.Data.Playground {
		t.Fatalf("me.playground = true in staging")
	}

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/dev/api/playground", ""},
		{http.MethodPost, "/dev/api/playground/jobs", `{"kind":"dev.test","count":1}`},
	} {
		rec = apiRequest(t, router, request.method, request.path, request.body, token, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404 (body %s)", request.method, request.path, rec.Code, rec.Body.String())
		}
	}
}

func playgroundRouter(t *testing.T, environment string) (http.Handler, string) {
	t.Helper()
	t.Setenv("APP_ENV", environment)
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	kit := testkit.New(t)
	cfg := kit.Config
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := kit.Deps
	if err := seeder.Run(context.Background(), cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"playground-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	if login.Data.Token == "" {
		t.Fatal("developer token missing")
	}
	return router, login.Data.Token
}
