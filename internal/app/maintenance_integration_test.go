package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestMaintenanceModeEndToEnd covers phase 24: the dashboard takes the app
// down (dev.manage only, audited, in the activity log); /api/v1 answers 503
// with Retry-After while /up, /api/health and /dev/api keep working; the
// bypass header passes; up restores normal answers.
func TestMaintenanceModeEndToEnd(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	app := testkit.New(t)
	super := app.SuperAdmin(t)
	viewer := app.User(t, testkit.Role(app.Role(t, "Dashboard viewer", authz.PermDevView)))

	app.Guest().Get("/api/v1/system/info").Status(t, http.StatusOK)

	// dev.view may read the state but not toggle it.
	var state struct {
		Down       bool    `json:"down"`
		Message    string  `json:"message"`
		RetryAfter int     `json:"retry_after"`
		HasSecret  bool    `json:"has_secret"`
		Secret     *string `json:"secret"`
	}
	app.As(viewer).Get("/dev/api/maintenance").Status(t, http.StatusOK).Data(t, &state)
	if state.Down {
		t.Fatal("maintenance state before down: down = true")
	}
	app.As(viewer).Post("/dev/api/maintenance/down", map[string]any{}).Status(t, http.StatusForbidden)

	app.As(super).Post("/dev/api/maintenance/down", map[string]any{
		"message": "Upgrading", "retry_after": 120, "with_secret": true,
	}).Status(t, http.StatusOK).Data(t, &state)
	if !state.Down || state.Message != "Upgrading" || state.RetryAfter != 120 || !state.HasSecret || state.Secret == nil {
		t.Fatalf("down response = %+v", state)
	}
	secret := *state.Secret

	// Closed: the API and the MCP surface.
	rec := app.Guest().Get("/api/v1/system/info").Status(t, http.StatusServiceUnavailable)
	if got := rec.Header().Get("Retry-After"); got != "120" {
		t.Errorf("Retry-After = %q, want 120", got)
	}
	var problem struct {
		ErrorCode string `json:"error_code"`
		Detail    string `json:"detail"`
	}
	rec.JSON(t, &problem)
	if problem.ErrorCode != "MAINTENANCE_MODE" || problem.Detail != "Upgrading" {
		t.Errorf("problem = %+v", problem)
	}
	app.As(super).Get("/api/v1/permissions").Status(t, http.StatusServiceUnavailable)

	// The bypass secret passes; a wrong one does not.
	app.Guest().Header(maintenance.BypassHeader, secret).Get("/api/v1/system/info").Status(t, http.StatusOK)
	app.Guest().Header(maintenance.BypassHeader, "not-the-secret").Get("/api/v1/system/info").Status(t, http.StatusServiceUnavailable)

	// Open: probes and the dashboard.
	app.Guest().Get("/up").Status(t, http.StatusOK)
	var health struct {
		Maintenance struct {
			Down    bool   `json:"down"`
			Message string `json:"message"`
		} `json:"maintenance"`
	}
	healthRec := app.Guest().Get("/api/health")
	if healthRec.Code != http.StatusOK && healthRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/api/health status = %d", healthRec.Code)
	}
	if err := json.Unmarshal(healthRec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if !health.Maintenance.Down || health.Maintenance.Message != "Upgrading" {
		t.Errorf("health maintenance block = %+v", health.Maintenance)
	}
	state.Secret = nil
	app.As(viewer).Get("/dev/api/maintenance").Status(t, http.StatusOK).Data(t, &state)
	if !state.Down || state.Secret != nil {
		t.Fatalf("status while down = %+v (the secret is never shown again)", state)
	}

	// Back up.
	app.As(super).Post("/dev/api/maintenance/up", map[string]any{}).Status(t, http.StatusOK)
	app.Guest().Get("/api/v1/system/info").Status(t, http.StatusOK)

	// Audited in the dashboard log and in the system activity log.
	ctx := context.Background()
	var audits int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM dev_actions WHERE action IN ('maintenance.down', 'maintenance.up')`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Errorf("dashboard audit rows = %d, want 2", audits)
	}
	var activityRows int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM activity_log WHERE action IN ('maintenance.down', 'maintenance.up') AND tenant_id IS NULL`).Scan(&activityRows); err != nil {
		t.Fatal(err)
	}
	if activityRows != 2 {
		t.Errorf("system activity rows = %d, want 2", activityRows)
	}
}

// TestMaintenanceServiceAcrossProcesses drives the service `ims down`, `ims
// maintenance:status` and `ims up` call, and checks that another process (its
// own checker over the same database) sees each change.
func TestMaintenanceServiceAcrossProcesses(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()

	// Another process starting up: its own checker over the same database,
	// whose first read always goes to the store. (A 1ns interval would not
	// do: Windows clocks are coarse enough for two reads to look "fresh".)
	store := maintenance.NewStore(app.Deps.Tx.Queries())
	otherProcessDown := func() bool { return maintenance.NewChecker(store, time.Hour, nil).IsDown(ctx) }
	if otherProcessDown() {
		t.Fatal("other process: down before ims down")
	}
	if _, _, err := app.Deps.Platform.Maintenance.Down(ctx, maintenance.DownInput{Message: "CLI"}); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if !otherProcessDown() {
		t.Fatal("other process: up after ims down")
	}
	status, err := app.Deps.Platform.Maintenance.Status(ctx)
	if err != nil || !status.Down || status.Message != "CLI" || status.ByUserID != nil {
		t.Fatalf("Status() = %+v, %v", status, err)
	}
	if changed, err := app.Deps.Platform.Maintenance.Up(ctx, 0); err != nil || !changed {
		t.Fatalf("Up() = %v, %v", changed, err)
	}
	if otherProcessDown() {
		t.Fatal("other process: down after ims up")
	}
}
