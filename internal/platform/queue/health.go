package queue

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/health"
)

// Stat summarizes one River queue. River owns its schema, so these queries are
// hand-written rather than sqlc-generated.
type Stat struct {
	Queue  string
	Total  int64
	Active int64
	Failed int64
}

// Stats reads queue depths from river_job.
func Stats(ctx context.Context, pool *pgxpool.Pool) ([]Stat, error) {
	rows, err := pool.Query(ctx, `
		SELECT queue,
		       count(*) AS total,
		       count(*) FILTER (WHERE state IN ('available', 'scheduled', 'retryable', 'running', 'pending')) AS active,
		       count(*) FILTER (WHERE state = 'discarded') AS failed
		FROM river_job
		GROUP BY queue
		ORDER BY queue`)
	if err != nil {
		return nil, fmt.Errorf("queue: read river stats: %w", err)
	}
	defer rows.Close()

	var stats []Stat
	for rows.Next() {
		var stat Stat
		if err := rows.Scan(&stat.Queue, &stat.Total, &stat.Active, &stat.Failed); err != nil {
			return nil, fmt.Errorf("queue: scan river stats: %w", err)
		}
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}

// HealthCheck reports River table reachability and failed-job counts.
type HealthCheck struct {
	Pool *pgxpool.Pool
}

// Name implements health.Check.
func (c HealthCheck) Name() string { return "Queue" }

// Run implements health.Check.
func (c HealthCheck) Run(ctx context.Context) health.CheckResult {
	stats, err := Stats(ctx, c.Pool)
	if err != nil {
		return health.CheckResult{
			Status:       health.StatusFailed,
			ShortSummary: "Unable to read queue stats (is River migrated?): " + err.Error(),
		}
	}

	meta := map[string]any{}
	var total, failed int64
	for _, stat := range stats {
		meta[stat.Queue] = map[string]any{
			"total":  stat.Total,
			"active": stat.Active,
			"failed": stat.Failed,
		}
		total += stat.Total
		failed += stat.Failed
	}

	if failed > 0 {
		return health.CheckResult{
			Status:       health.StatusWarning,
			ShortSummary: fmt.Sprintf("%d failed queue job(s) awaiting review.", failed),
			Meta:         meta,
		}
	}
	return health.CheckResult{
		Status:       health.StatusOK,
		ShortSummary: fmt.Sprintf("Queue tables reachable (%d job(s) tracked).", total),
		Meta:         meta,
	}
}
