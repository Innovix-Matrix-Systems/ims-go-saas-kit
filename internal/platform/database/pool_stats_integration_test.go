package database_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/health"
)

// A nearly full pool turns the health check into a warning that names the
// pool, while the counts stay out of the (public) report.
func TestHealthCheckWarnsOnABusyPoolWithoutCounts(t *testing.T) {
	ctx := context.Background()
	registry, err := database.Open(ctx, config.Database{Connections: []config.Connection{{
		Name: "primary", Driver: "pgsql", DSN: dbtest.DSN(t), MaxConns: 5, ConnTimeout: 5 * time.Second,
	}}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(registry.Close)
	check := database.HealthCheck{Registry: registry}

	if result := check.Run(ctx); result.Status != health.StatusOK {
		t.Fatalf("idle pool: status = %s (%s), want ok", result.Status, result.ShortSummary)
	}

	// Hold 4 of 5 connections (80%): busy, and the ping still gets the fifth.
	held := make([]*pgxpool.Conn, 0, 4)
	for range 4 {
		conn, err := registry.Primary().Pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		held = append(held, conn)
	}
	stats := registry.PoolStats()
	if len(stats) != 1 || stats[0].Name != "primary" || stats[0].Active != 4 || stats[0].Max != 5 || !stats[0].Busy {
		t.Fatalf("PoolStats = %+v, want primary 4/5 active, busy", stats)
	}
	if stats[0].Acquires < 4 {
		t.Errorf("Acquires = %d, want at least 4", stats[0].Acquires)
	}
	result := check.Run(ctx)
	for _, conn := range held {
		conn.Release()
	}
	if result.Status != health.StatusWarning {
		t.Fatalf("busy pool: status = %s (%s), want warning", result.Status, result.ShortSummary)
	}
	if !strings.Contains(result.ShortSummary, "primary: busy") {
		t.Errorf("summary = %q, want it to name the busy pool", result.ShortSummary)
	}
	meta, _ := result.Meta["primary"].(map[string]any)
	for _, key := range []string{"active", "idle", "total", "max"} {
		if _, ok := meta[key]; ok {
			t.Errorf("public meta has %q: %v", key, meta)
		}
	}
}
