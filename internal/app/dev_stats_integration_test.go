package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// TestDeveloperChartStats proves the chart endpoints: requests by status
// class and route, exceptions per hour (every hour present) and deliveries
// per day and channel, plus the window validation.
func TestDeveloperChartStats(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	// Capture on but sampling nothing, so only the rows below are counted.
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
		method, route string
		status        int32
		duration      float64
		ago           time.Duration
	}{
		{http.MethodGet, "/api/v1/branch", 200, 10, 5 * time.Minute},
		{http.MethodGet, "/api/v1/branch", 200, 20, 6 * time.Minute},
		{http.MethodGet, "/api/v1/branch", 500, 30, 7 * time.Minute},
		{http.MethodPost, "/api/v1/login", 302, 400, 8 * time.Minute},
		{http.MethodPost, "/api/v1/login", 422, 300, 9 * time.Minute},
		{http.MethodGet, "/api/v1/branch", 200, 5, 3 * time.Hour}, // outside 1h, inside 24h
	} {
		if _, err := queries.InsertDevRequest(ctx, dbgen.InsertDevRequestParams{
			RequestID: "stats", Method: request.method, Path: request.route, Route: request.route,
			Status: request.status, DurationMs: request.duration, CreatedAt: at(request.ago),
		}); err != nil {
			t.Fatalf("InsertDevRequest() error = %v", err)
		}
	}
	for _, ago := range []time.Duration{10 * time.Minute, 20 * time.Minute, 5 * time.Hour, 30 * time.Hour} {
		if err := queries.InsertDevException(ctx, dbgen.InsertDevExceptionParams{Class: "*errors.errorString", Message: "boom", CreatedAt: at(ago)}); err != nil {
			t.Fatalf("InsertDevException() error = %v", err)
		}
	}
	for _, delivery := range []struct{ channel, status string }{
		{"mail", "sent"}, {"mail", "sent"}, {"mail", "failed"}, {"sms", "queued"}, {"push", "skipped"},
	} {
		if _, err := queries.CreateDelivery(ctx, dbgen.CreateDeliveryParams{
			Channel: delivery.channel, Address: "x", Template: "stats", Status: delivery.status,
		}); err != nil {
			t.Fatalf("CreateDelivery() error = %v", err)
		}
	}

	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"stats-suite"}`, "", nil)
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
	get := func(path string, want int, into any) {
		t.Helper()
		rec := apiRequest(t, router, http.MethodGet, path, "", token, nil)
		if rec.Code != want {
			t.Fatalf("GET %s status = %d, want %d; body = %s", path, rec.Code, want, rec.Body.String())
		}
		if into != nil {
			decodeBody(t, rec, into)
		}
	}

	type route struct {
		Method   string  `json:"method"`
		Route    string  `json:"route"`
		Requests int64   `json:"requests"`
		Errors   int64   `json:"errors"`
		P95Ms    float64 `json:"p95_ms"`
	}
	var requests struct {
		Data struct {
			WindowHours int `json:"window_hours"`
			Status      struct {
				Success      int64 `json:"success"`
				Redirect     int64 `json:"redirect"`
				ClientErrors int64 `json:"client_errors"`
				ServerErrors int64 `json:"server_errors"`
			} `json:"status"`
			Busiest []route `json:"busiest"`
			Slowest []route `json:"slowest"`
		} `json:"data"`
	}
	get("/dev/api/requests/stats", http.StatusOK, &requests)
	status := requests.Data.Status
	if requests.Data.WindowHours != 1 || status.Success != 2 || status.Redirect != 1 || status.ClientErrors != 1 || status.ServerErrors != 1 {
		t.Errorf("1h stats = %+v", requests.Data)
	}
	if len(requests.Data.Busiest) != 2 || requests.Data.Busiest[0].Route != "/api/v1/branch" ||
		requests.Data.Busiest[0].Requests != 3 || requests.Data.Busiest[0].Errors != 1 {
		t.Errorf("busiest = %+v, want /api/v1/branch first with 3 requests, 1 error", requests.Data.Busiest)
	}
	if len(requests.Data.Slowest) == 0 || requests.Data.Slowest[0].Route != "/api/v1/login" {
		t.Errorf("slowest = %+v, want /api/v1/login first", requests.Data.Slowest)
	}
	get("/dev/api/requests/stats?hours=24", http.StatusOK, &requests)
	if requests.Data.Status.Success != 3 {
		t.Errorf("24h success = %d, want 3", requests.Data.Status.Success)
	}
	get("/dev/api/requests/stats?hours=2", http.StatusUnprocessableEntity, nil)

	var exceptions struct {
		Data struct {
			Hours []struct {
				Occurrences int64 `json:"occurrences"`
			} `json:"hours"`
			Classes []struct {
				Class       string `json:"class"`
				Occurrences int64  `json:"occurrences"`
			} `json:"classes"`
		} `json:"data"`
	}
	get("/dev/api/exceptions/stats", http.StatusOK, &exceptions)
	var hourly int64
	for _, hour := range exceptions.Data.Hours {
		hourly += hour.Occurrences
	}
	if len(exceptions.Data.Hours) != 24 || hourly != 3 {
		t.Errorf("hours = %d rows totalling %d, want 24 rows totalling 3", len(exceptions.Data.Hours), hourly)
	}
	if len(exceptions.Data.Classes) != 1 || exceptions.Data.Classes[0].Occurrences != 3 {
		t.Errorf("classes = %+v, want one class with 3", exceptions.Data.Classes)
	}

	type counts struct {
		Channel string `json:"channel"`
		Sent    int64  `json:"sent"`
		Failed  int64  `json:"failed"`
		Queued  int64  `json:"queued"`
		Skipped int64  `json:"skipped"`
	}
	var deliveries struct {
		Data struct {
			WindowDays int      `json:"window_days"`
			Days       []counts `json:"days"`
			Channels   []counts `json:"channels"`
		} `json:"data"`
	}
	get("/dev/api/deliveries/stats", http.StatusOK, &deliveries)
	if deliveries.Data.WindowDays != 7 || len(deliveries.Data.Days) != 7 {
		t.Fatalf("days = %d rows (window %d), want 7", len(deliveries.Data.Days), deliveries.Data.WindowDays)
	}
	today := deliveries.Data.Days[6]
	if today.Sent != 2 || today.Failed != 1 || today.Queued != 1 || today.Skipped != 1 {
		t.Errorf("today = %+v, want 2 sent, 1 failed, 1 queued, 1 skipped", today)
	}
	if len(deliveries.Data.Channels) != 3 || deliveries.Data.Channels[0].Channel != "mail" || deliveries.Data.Channels[0].Sent != 2 {
		t.Errorf("channels = %+v, want mail, push, sms with mail sent 2", deliveries.Data.Channels)
	}
	get("/dev/api/deliveries/stats?days=3", http.StatusUnprocessableEntity, nil)
}
