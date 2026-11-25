package cache

import (
	"context"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/health"
)

// HealthCheck pings the configured cache store.
type HealthCheck struct {
	Store Store
}

// Name implements health.Check.
func (c HealthCheck) Name() string { return "Cache" }

// Run implements health.Check.
func (c HealthCheck) Run(ctx context.Context) health.CheckResult {
	start := time.Now()
	err := c.Store.Ping(ctx)
	durationMS := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return health.CheckResult{
			Status:       health.StatusFailed,
			ShortSummary: "Cache store is unreachable: " + err.Error(),
			Meta:         map[string]any{"duration_ms": durationMS},
		}
	}
	return health.CheckResult{
		Status:       health.StatusOK,
		ShortSummary: "Cache store is reachable.",
		Meta:         map[string]any{"duration_ms": durationMS},
	}
}
