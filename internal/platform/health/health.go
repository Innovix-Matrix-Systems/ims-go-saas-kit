package health

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Status is the outcome of a check.
type Status string

const (
	StatusOK      Status = "ok"
	StatusWarning Status = "warning"
	StatusFailed  Status = "failed"
	StatusCrashed Status = "crashed"
)

// CheckResult is the outcome of one check.
type CheckResult struct {
	Name                string         `json:"name"`
	Label               string         `json:"label"`
	NotificationMessage string         `json:"notificationMessage"`
	ShortSummary        string         `json:"shortSummary"`
	Status              Status         `json:"status"`
	Meta                map[string]any `json:"meta,omitempty"`
	// Advisory checks are reported but never make the endpoint answer 503
	// (e.g. backups, the separate scheduler process): an instance can serve
	// traffic while they are unhealthy.
	Advisory bool `json:"advisory,omitempty"`
}

// StoredResults is the payload returned by the health endpoint.
type StoredResults struct {
	FinishedAt   int64         `json:"finishedAt"`
	CheckResults []CheckResult `json:"checkResults"`
	// Maintenance is the maintenance-mode block, filled per
	// request by the handler, never cached with the checks.
	Maintenance any `json:"maintenance,omitempty"`
}

// HasFailures reports whether any non-advisory check failed or crashed.
func (s StoredResults) HasFailures() bool {
	for _, r := range s.CheckResults {
		if r.Advisory {
			continue
		}
		if r.Status == StatusFailed || r.Status == StatusCrashed {
			return true
		}
	}
	return false
}

// Check is a single health check.
type Check interface {
	Name() string
	Run(ctx context.Context) CheckResult
}

// Registry holds the checks and runs them.
type Registry struct {
	checks   []Check
	advisory map[int]bool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Add appends a check whose failure makes the instance unready (503).
func (r *Registry) Add(check Check) {
	r.checks = append(r.checks, check)
}

// AddAdvisory appends a check that is reported but never fails readiness.
func (r *Registry) AddAdvisory(check Check) {
	if r.advisory == nil {
		r.advisory = map[int]bool{}
	}
	r.advisory[len(r.checks)] = true
	r.checks = append(r.checks, check)
}

// Run executes every check with a per-check timeout and returns the results.
func (r *Registry) Run(ctx context.Context) StoredResults {
	results := make([]CheckResult, 0, len(r.checks))
	for i, check := range r.checks {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result := check.Run(checkCtx)
		cancel()
		result.Advisory = r.advisory[i]

		if result.Name == "" {
			result.Name = check.Name()
		}
		if result.Label == "" {
			result.Label = result.Name
		}
		if result.Status == "" {
			result.Status = StatusOK
		}
		if result.Meta == nil {
			result.Meta = map[string]any{}
		}
		results = append(results, result)
	}
	return StoredResults{
		FinishedAt:   time.Now().Unix(),
		CheckResults: results,
	}
}

// EnvironmentCheck reports the runtime environment.
type EnvironmentCheck struct {
	Env string
}

// Name implements Check.
func (c EnvironmentCheck) Name() string { return "Environment" }

// Run implements Check.
func (c EnvironmentCheck) Run(ctx context.Context) CheckResult {
	return CheckResult{
		Status:       StatusOK,
		ShortSummary: fmt.Sprintf("Running in %s on %s.", c.Env, runtime.Version()),
		Meta: map[string]any{
			"environment": c.Env,
			"go_version":  runtime.Version(),
			"platform":    runtime.GOOS + "/" + runtime.GOARCH,
		},
	}
}

// DebugModeCheck warns when debug mode is enabled in production.
type DebugModeCheck struct {
	Env   string
	Debug bool
}

// Name implements Check.
func (c DebugModeCheck) Name() string { return "Debug Mode" }

// Run implements Check.
func (c DebugModeCheck) Run(ctx context.Context) CheckResult {
	if c.Debug && strings.EqualFold(c.Env, "production") {
		return CheckResult{
			Status:       StatusWarning,
			ShortSummary: "Debug mode is enabled in production.",
			Meta:         map[string]any{"debug": true, "environment": c.Env},
		}
	}
	return CheckResult{
		Status:       StatusOK,
		ShortSummary: fmt.Sprintf("Debug mode is %s.", enabledWord(c.Debug)),
		Meta:         map[string]any{"debug": c.Debug},
	}
}

// ApplicationCheck reports the running application.
type ApplicationCheck struct {
	NameValue    string
	VersionValue string
}

// Name implements Check.
func (c ApplicationCheck) Name() string { return "Application" }

// Run implements Check.
func (c ApplicationCheck) Run(ctx context.Context) CheckResult {
	return CheckResult{
		Status:       StatusOK,
		ShortSummary: fmt.Sprintf("%s %s is running.", c.NameValue, c.VersionValue),
		Meta:         map[string]any{"name": c.NameValue, "version": c.VersionValue},
	}
}

func enabledWord(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}
