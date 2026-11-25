package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/health"
)

// ErrUnknownTask reports an unregistered scheduled task.
func ErrUnknownTask(id string) error {
	return fmt.Errorf("scheduler: unknown task %q", id)
}

// HealthCheck reports whether the scheduler heartbeat is fresh.
type HealthCheck struct {
	Store cache.Store
}

// Name implements health.Check.
func (c HealthCheck) Name() string { return "Scheduler" }

// Run implements health.Check.
func (c HealthCheck) Run(ctx context.Context) health.CheckResult {
	var timestamp int64
	found, err := c.Store.Get(ctx, HeartbeatKey, &timestamp)
	if err != nil {
		return health.CheckResult{
			Status:       health.StatusFailed,
			ShortSummary: "Unable to read scheduler heartbeat: " + err.Error(),
		}
	}
	if !found {
		return health.CheckResult{
			Status:       health.StatusWarning,
			ShortSummary: "Scheduler heartbeat not found; is the schedule process running?",
		}
	}

	age := time.Since(time.Unix(timestamp, 0))
	if age > 2*time.Minute {
		return health.CheckResult{
			Status:       health.StatusWarning,
			ShortSummary: fmt.Sprintf("Scheduler heartbeat is stale (last run %s ago).", age.Round(time.Second)),
			Meta:         map[string]any{"age_seconds": int64(age.Seconds())},
		}
	}
	return health.CheckResult{
		Status:       health.StatusOK,
		ShortSummary: fmt.Sprintf("Scheduler last ran %s ago.", age.Round(time.Second)),
		Meta:         map[string]any{"age_seconds": int64(age.Seconds())},
	}
}
