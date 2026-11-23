package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

// TestDeveloperOverview proves GET /dev/api/overview: health from the cached
// report, exceptions in the last 24 hours grouped by class, the last hour of
// traffic per minute, settings/flags drift and the queue totals.
func TestDeveloperOverview(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	// Capture stays on (so the traffic row is shown) but samples nothing, so
	// only the rows this test writes are counted.
	t.Setenv("DEV_DASHBOARD_SAMPLE_RATE", "0")

	kit := testkit.New(t)
	deps := kit.Deps
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	if err := seeder.Run(ctx, kit.Config, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	queries := deps.Tx.Queries()
	now := time.Now()
	at := func(ago time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: now.Add(-ago), Valid: true}
	}
	for _, request := range []struct {
		status   int32
		duration float64
		ago      time.Duration
	}{
		{200, 10, 5 * time.Minute},
		{201, 30, 5 * time.Minute},
		{422, 20, 3 * time.Minute},
		{500, 90, 3 * time.Minute},
		{200, 5, 2 * time.Hour}, // outside the hour
	} {
		if _, err := queries.InsertDevRequest(ctx, dbgen.InsertDevRequestParams{
			RequestID: "overview", Method: http.MethodGet, Path: "/api/v1/branch", Route: "/api/v1/branch",
			Status: request.status, DurationMs: request.duration, CreatedAt: at(request.ago),
		}); err != nil {
			t.Fatalf("InsertDevRequest() error = %v", err)
		}
	}
	for _, exception := range []struct {
		class, message string
		ago            time.Duration
	}{
		{"*errors.errorString", "first", 3 * time.Hour},
		{"*errors.errorString", "latest", time.Hour},
		{"*pgconn.PgError", "duplicate key", 2 * time.Hour},
		{"*errors.errorString", "too old", 25 * time.Hour}, // outside the day
	} {
		if err := queries.InsertDevException(ctx, dbgen.InsertDevExceptionParams{
			Class: exception.class, Message: exception.message, CreatedAt: at(exception.ago),
		}); err != nil {
			t.Fatalf("InsertDevException() error = %v", err)
		}
	}

	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"overview-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/overview", "", login.Data.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Health struct {
				Status string `json:"status"`
				Checks []struct {
					Status string `json:"status"`
				} `json:"checks"`
				Migrations struct {
					Status  string `json:"status"`
					Pending int    `json:"pending"`
				} `json:"migrations"`
			} `json:"health"`
			Queue struct {
				Available bool `json:"available"`
			} `json:"queue"`
			Exceptions struct {
				Last24h int64 `json:"last_24h"`
				Groups  []struct {
					Class         string `json:"class"`
					Occurrences   int64  `json:"occurrences"`
					LatestMessage string `json:"latest_message"`
				} `json:"groups"`
			} `json:"exceptions"`
			Drift struct {
				Settings int `json:"settings"`
				Flags    int `json:"flags"`
			} `json:"drift"`
			Traffic struct {
				Capturing  bool    `json:"capturing"`
				SampleRate float64 `json:"sample_rate"`
				Minutes    []struct {
					Requests int64 `json:"requests"`
				} `json:"minutes"`
				Totals struct {
					Requests     int64   `json:"requests"`
					ClientErrors int64   `json:"client_errors"`
					ServerErrors int64   `json:"server_errors"`
					P95Ms        float64 `json:"p95_ms"`
				} `json:"totals"`
			} `json:"traffic"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	data := body.Data

	if len(data.Health.Checks) == 0 || data.Health.Status == "" {
		t.Fatalf("health = %+v, want the cached report", data.Health)
	}
	if data.Health.Migrations.Status != "ok" || data.Health.Migrations.Pending != 0 {
		t.Fatalf("migrations = %+v, want ok with none pending", data.Health.Migrations)
	}

	if data.Exceptions.Last24h != 3 {
		t.Fatalf("exceptions in 24h = %d, want 3", data.Exceptions.Last24h)
	}
	if len(data.Exceptions.Groups) != 2 {
		t.Fatalf("exception groups = %+v, want 2", data.Exceptions.Groups)
	}
	newest := data.Exceptions.Groups[0]
	if newest.Class != "*errors.errorString" || newest.Occurrences != 2 || newest.LatestMessage != "latest" {
		t.Fatalf("newest group = %+v, want errorString x2 with the latest message", newest)
	}

	traffic := data.Traffic
	if !traffic.Capturing || traffic.SampleRate != 0 {
		t.Fatalf("traffic capture = %v at %v, want on at 0", traffic.Capturing, traffic.SampleRate)
	}
	if traffic.Totals.Requests != 4 || traffic.Totals.ClientErrors != 1 || traffic.Totals.ServerErrors != 1 {
		t.Fatalf("traffic totals = %+v, want 4 requests, one 4xx, one 5xx", traffic.Totals)
	}
	if traffic.Totals.P95Ms < 30 || traffic.Totals.P95Ms > 90 {
		t.Fatalf("p95 = %v, want between 30 and 90 ms", traffic.Totals.P95Ms)
	}
	var perMinute int64
	for _, minute := range traffic.Minutes {
		perMinute += minute.Requests
	}
	if len(traffic.Minutes) != 2 || perMinute != 4 {
		t.Fatalf("minutes = %+v, want two buckets holding the 4 requests", traffic.Minutes)
	}

	// The catalogs have keys the database does not hold until a sync.
	if data.Drift.Settings == 0 || data.Drift.Flags == 0 {
		t.Fatalf("drift = %+v, want catalog keys missing before a sync", data.Drift)
	}
	for _, path := range []string{"/dev/api/settings/sync", "/dev/api/flags/sync"} {
		if rec := apiRequest(t, router, http.MethodPost, path, "", login.Data.Token, nil); rec.Code != http.StatusOK {
			t.Fatalf("POST %s status = %d body = %s", path, rec.Code, rec.Body.String())
		}
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/overview", "", login.Data.Token, nil)
	decodeBody(t, rec, &body)
	if body.Data.Drift.Settings != 0 || body.Data.Drift.Flags != 0 {
		t.Fatalf("drift after sync = %+v, want none", body.Data.Drift)
	}

	// dev.view is enough: the Overview is read-only.
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/overview", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous overview status = %d, want 401", rec.Code)
	}
}
