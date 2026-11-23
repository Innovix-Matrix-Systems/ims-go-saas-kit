package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/health"
)

// HealthCheck pings every configured connection.
type HealthCheck struct {
	Registry *Registry
}

// Name implements health.Check.
func (c HealthCheck) Name() string { return "Database" }

// Run implements health.Check.
func (c HealthCheck) Run(ctx context.Context) health.CheckResult {
	healthy := true
	meta := map[string]any{}
	summaries := make([]string, 0, len(c.Registry.Names()))

	for _, name := range c.Registry.Names() {
		conn, err := c.Registry.Conn(name)
		if err != nil {
			healthy = false
			summaries = append(summaries, name+": unknown")
			continue
		}
		start := time.Now()
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		pingErr := conn.Pool.Ping(pingCtx)
		cancel()
		durationMS := float64(time.Since(start).Microseconds()) / 1000.0

		if pingErr != nil {
			healthy = false
			meta[name] = map[string]any{"status": "failed", "error": pingErr.Error(), "duration_ms": durationMS}
			summaries = append(summaries, name+": failed")
			continue
		}
		meta[name] = map[string]any{"status": "ok", "duration_ms": durationMS}
		summaries = append(summaries, name+": ok")
	}

	if !healthy {
		return health.CheckResult{
			Status:       health.StatusFailed,
			ShortSummary: "Database connection failed (" + strings.Join(summaries, ", ") + ").",
			Meta:         meta,
		}
	}
	return health.CheckResult{
		Status:       health.StatusOK,
		ShortSummary: "Database connection is healthy (" + strings.Join(summaries, ", ") + ").",
		Meta:         meta,
	}
}

// MigrationCheck verifies that all embedded migrations are applied.
type MigrationCheck struct {
	Pool *pgxpool.Pool
}

// Name implements health.Check.
func (c MigrationCheck) Name() string { return "Migrations" }

// Run implements health.Check.
func (c MigrationCheck) Run(ctx context.Context) health.CheckResult {
	statuses, err := MigrationStatuses(ctx, c.Pool)
	if err != nil {
		return health.CheckResult{
			Status:       health.StatusFailed,
			ShortSummary: "Unable to read migration status: " + err.Error(),
		}
	}

	var pending []string
	for _, status := range statuses {
		if !status.Applied {
			pending = append(pending, fmt.Sprintf("%d (%s)", status.Version, status.Name))
		}
	}

	riverVersion, riverExists, err := RiverSchemaVersion(ctx, c.Pool)
	if err != nil {
		return health.CheckResult{
			Status:       health.StatusFailed,
			ShortSummary: "Unable to read River schema status: " + err.Error(),
		}
	}
	riverPending := 0
	if riverExists {
		if riverPending, err = RiverPendingCount(ctx, c.Pool); err != nil {
			return health.CheckResult{
				Status:       health.StatusFailed,
				ShortSummary: "Unable to check River migrations: " + err.Error(),
			}
		}
	}

	if !riverExists || riverPending > 0 {
		pending = append(pending, fmt.Sprintf("river (%d pending)", riverPending))
	}
	if len(pending) > 0 {
		return health.CheckResult{
			Status:       health.StatusWarning,
			ShortSummary: "The following migrations are pending: " + strings.Join(pending, ", "),
			Meta:         map[string]any{"pending_count": len(pending), "river_schema_version": riverVersion},
		}
	}
	return health.CheckResult{
		Status:       health.StatusOK,
		ShortSummary: fmt.Sprintf("All migrations are up to date (River schema v%d).", riverVersion),
		Meta:         map[string]any{"applied_count": len(statuses), "river_schema_version": riverVersion},
	}
}
