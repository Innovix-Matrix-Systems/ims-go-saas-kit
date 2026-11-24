package dev

import (
	"context"
	"net/http"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/health"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/telemetry"
)

// The Overview answers "is anything wrong right now?" in one call: health
// and migrations, the queue, exceptions in the last day, the last backup,
// settings and flags the catalog has but the database lacks, and the last
// hour of captured traffic. Every value comes from the code path its own
// screen uses (the cached /api/health report, queue stats, backup monitor,
// effective settings and flags); only the traffic and exception aggregates
// are new queries.

const (
	overviewTrafficWindow   = time.Hour
	overviewExceptionWindow = 24 * time.Hour
	overviewExceptionGroups = 5
)

// HealthSource is the cached health report, the same one /api/health serves.
type HealthSource interface {
	Results(ctx context.Context) health.StoredResults
}

// Overview is the GET /dev/api/overview payload.
type Overview struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Health      OverviewHealth     `json:"health"`
	Queue       OverviewQueue      `json:"queue"`
	Exceptions  OverviewExceptions `json:"exceptions"`
	Backup      OverviewBackup     `json:"backup"`
	Drift       OverviewDrift      `json:"drift"`
	Traffic     OverviewTraffic    `json:"traffic"`
}

// OverviewHealth summarises the health checks.
type OverviewHealth struct {
	// Status is ok, warning (a warning or an advisory failure) or failed.
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	// Checks is every check, the ones that are not OK first.
	Checks     []OverviewCheck    `json:"checks"`
	Problems   []OverviewCheck    `json:"problems"`
	Migrations OverviewMigrations `json:"migrations"`
}

// OverviewCheck is one health check.
type OverviewCheck struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Summary  string `json:"summary"`
	Advisory bool   `json:"advisory"`
}

// OverviewMigrations is the migrations check.
type OverviewMigrations struct {
	Status  string `json:"status"`
	Pending int    `json:"pending"`
	Summary string `json:"summary"`
}

// OverviewQueue totals the River queues.
type OverviewQueue struct {
	// Available is false when the queue driver cannot be inspected (sync).
	Available bool  `json:"available"`
	Queues    int   `json:"queues"`
	Paused    int   `json:"paused"`
	Backlog   int64 `json:"backlog"`
	Failed    int64 `json:"failed"`
}

// OverviewExceptions counts exceptions in the last 24 hours.
type OverviewExceptions struct {
	Last24h int64            `json:"last_24h"`
	Groups  []ExceptionGroup `json:"groups"`
}

// OverviewBackup is the backup monitor's verdict.
type OverviewBackup struct {
	Enabled       bool       `json:"enabled"`
	Status        string     `json:"status"`
	Summary       string     `json:"summary"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	AgeHours      float64    `json:"age_hours"`
	MaxAgeHours   int        `json:"max_age_hours"`
}

// OverviewDrift counts catalog keys with no stored global value (what a
// sync would create).
type OverviewDrift struct {
	Settings int `json:"settings"`
	Flags    int `json:"flags"`
}

// OverviewTraffic is the last hour of captured requests, per minute.
type OverviewTraffic struct {
	Capturing     bool            `json:"capturing"`
	SampleRate    float64         `json:"sample_rate"`
	WindowMinutes int             `json:"window_minutes"`
	Minutes       []TrafficMinute `json:"minutes"`
	Totals        TrafficTotals   `json:"totals"`
}

func newOverviewService(deps Deps) *OverviewService {
	return &OverviewService{
		health:     deps.Health,
		queueAdmin: deps.QueueAdmin,
		telemetry:  deps.Telemetry,
		collector:  deps.Collector,
		backups:    deps.Backups,
		settings:   deps.Settings,
		flags:      deps.FeatureFlags,
		now:        time.Now,
	}
}

// OverviewService builds the Overview.
type OverviewService struct {
	health     HealthSource
	queueAdmin queue.Admin
	telemetry  TelemetryRepository
	collector  *telemetry.Collector
	backups    *backup.Service
	settings   *settings.Service
	flags      *featureflags.Service
	now        func() time.Time
}

// Build collects the Overview. A part that cannot be read fails the call:
// the dashboard shows the error rather than a half-true "all OK".
func (s *OverviewService) Build(ctx context.Context) (Overview, error) {
	now := s.now()
	out := Overview{GeneratedAt: now}
	if s.health != nil {
		out.Health = summariseHealth(s.health.Results(ctx))
	}

	if s.queueAdmin != nil {
		queues, err := s.queueAdmin.ListQueues(ctx)
		if err != nil {
			return Overview{}, err
		}
		out.Queue = summariseQueues(queues)
	}

	count, err := s.telemetry.CountExceptionsSince(ctx, now.Add(-overviewExceptionWindow))
	if err != nil {
		return Overview{}, err
	}
	groups, err := s.telemetry.ExceptionGroups(ctx, now.Add(-overviewExceptionWindow), overviewExceptionGroups)
	if err != nil {
		return Overview{}, err
	}
	out.Exceptions = OverviewExceptions{Last24h: count, Groups: groups}

	if s.backups != nil {
		if out.Backup, err = s.backup(ctx); err != nil {
			return Overview{}, err
		}
	}
	if out.Drift, err = s.drift(ctx); err != nil {
		return Overview{}, err
	}

	out.Traffic = OverviewTraffic{
		Capturing:     s.collector.Enabled() && s.collector.Captures().HTTP,
		SampleRate:    s.collector.SampleRate(),
		WindowMinutes: int(overviewTrafficWindow / time.Minute),
		Minutes:       []TrafficMinute{},
	}
	if out.Traffic.Capturing {
		minutes, totals, err := s.telemetry.Traffic(ctx, now.Add(-overviewTrafficWindow))
		if err != nil {
			return Overview{}, err
		}
		out.Traffic.Minutes = minutes
		out.Traffic.Totals = totals
	}
	return out, nil
}

func (s *OverviewService) backup(ctx context.Context) (OverviewBackup, error) {
	stats, err := s.backups.Stats(ctx)
	if err != nil {
		return OverviewBackup{}, err
	}
	monitor, err := s.backups.Monitor(ctx)
	if err != nil {
		return OverviewBackup{}, err
	}
	return OverviewBackup{
		Enabled:       s.backups.Enabled(),
		Status:        monitor.Status,
		Summary:       monitor.Summary,
		LastSuccessAt: stats.LastSuccessAt,
		AgeHours:      monitor.AgeHours,
		MaxAgeHours:   monitor.MaxAgeHours,
	}, nil
}

func (s *OverviewService) drift(ctx context.Context) (OverviewDrift, error) {
	var drift OverviewDrift
	if s.settings != nil {
		items, err := s.settings.EffectiveForTarget(ctx, settings.Target{Type: settings.TargetGlobal})
		if err != nil {
			return drift, err
		}
		for _, item := range items {
			if item.Source == "default" {
				drift.Settings++
			}
		}
	}
	if s.flags != nil {
		items, err := s.flags.EffectiveForTarget(ctx, featureflags.Target{Type: featureflags.TargetGlobal})
		if err != nil {
			return drift, err
		}
		for _, item := range items {
			if item.Source == "default" {
				drift.Flags++
			}
		}
	}
	return drift, nil
}

func summariseHealth(results health.StoredResults) OverviewHealth {
	out := OverviewHealth{
		Status:    string(health.StatusOK),
		CheckedAt: time.Unix(results.FinishedAt, 0).UTC(),
		Checks:    []OverviewCheck{},
		Problems:  []OverviewCheck{},
	}
	var passing []OverviewCheck
	for _, check := range results.CheckResults {
		if check.Name == "Migrations" {
			out.Migrations = OverviewMigrations{
				Status:  string(check.Status),
				Pending: pendingCount(check.Meta),
				Summary: check.ShortSummary,
			}
		}
		item := OverviewCheck{
			Name:     check.Name,
			Label:    check.Label,
			Status:   string(check.Status),
			Summary:  check.ShortSummary,
			Advisory: check.Advisory,
		}
		if check.Status == health.StatusOK {
			passing = append(passing, item)
			continue
		}
		out.Problems = append(out.Problems, item)
		failed := check.Status == health.StatusFailed || check.Status == health.StatusCrashed
		if failed && !check.Advisory {
			out.Status = string(health.StatusFailed)
		} else if out.Status == string(health.StatusOK) {
			out.Status = string(health.StatusWarning)
		}
	}
	out.Checks = append(append(out.Checks, out.Problems...), passing...)
	return out
}

func pendingCount(meta map[string]any) int {
	switch value := meta["pending_count"].(type) {
	case int:
		return value
	case float64:
		return int(value)
	}
	return 0
}

func summariseQueues(queues []queue.AdminQueue) OverviewQueue {
	out := OverviewQueue{Available: true, Queues: len(queues)}
	for _, item := range queues {
		out.Backlog += item.Active
		out.Failed += item.Failed
		if item.PausedAt != nil {
			out.Paused++
		}
	}
	return out
}

func (h *Handler) getOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.overview.Build(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, overview)
}
