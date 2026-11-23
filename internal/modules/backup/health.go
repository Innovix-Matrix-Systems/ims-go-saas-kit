package backup

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/health"
)

type HealthCheck struct {
	Service *Service
}

func (c HealthCheck) Name() string { return "Backups" }

func (c HealthCheck) Run(ctx context.Context) health.CheckResult {
	if c.Service == nil {
		return health.CheckResult{Status: health.StatusWarning, ShortSummary: "Backup service is not configured."}
	}
	result, err := c.Service.Monitor(ctx)
	if err != nil {
		return health.CheckResult{
			Status:       health.StatusFailed,
			ShortSummary: "Unable to read backup state: " + err.Error(),
		}
	}
	status := health.StatusOK
	switch result.Status {
	case "warning":
		status = health.StatusWarning
	case "failed":
		status = health.StatusFailed
	}
	meta := map[string]any{
		"total_size":      result.TotalSize,
		"successful_runs": result.SuccessfulRuns,
		"max_age_hours":   result.MaxAgeHours,
		"max_total_mb":    result.MaxTotalMB,
	}
	if result.LastRun != nil {
		meta["last_run_id"] = result.LastRun.ID
		meta["last_run_filename"] = result.LastRun.Filename
		meta["last_run_at"] = result.LastRun.CreatedAt
	}
	return health.CheckResult{
		Status:       status,
		ShortSummary: result.Summary,
		Meta:         meta,
	}
}
