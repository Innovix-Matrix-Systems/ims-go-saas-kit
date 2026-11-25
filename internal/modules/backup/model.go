package backup

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusPartial   = "partial"
	StatusFailed    = "failed"
	StatusDeleted   = "deleted"
)

const (
	TriggerCLI       = "cli"
	TriggerSchedule  = "schedule"
	TriggerDashboard = "dashboard"
)

const (
	ScopeDatabase      = "database"
	ScopeDatabaseFiles = "database_files"
)

const (
	DestinationLocal = "local"
	DestinationS3    = "s3"
)

type DestinationResult struct {
	Destination string `json:"destination"`
	Status      string `json:"status"`
	Path        string `json:"path,omitempty"`
	Key         string `json:"key,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	Error       string `json:"error,omitempty"`
}

type Run struct {
	ID             int64               `json:"id"`
	UUID           string              `json:"uuid"`
	Status         string              `json:"status"`
	Trigger        string              `json:"trigger"`
	ActorUserID    *int64              `json:"actor_user_id"`
	Filename       string              `json:"filename"`
	Scope          string              `json:"scope"`
	SizeBytes      int64               `json:"size_bytes"`
	ChecksumSHA256 string              `json:"checksum_sha256,omitempty"`
	PgDumpVersion  string              `json:"pg_dump_version,omitempty"`
	DatabaseName   string              `json:"database_name,omitempty"`
	Destinations   []DestinationResult `json:"destinations"`
	Error          string              `json:"error,omitempty"`
	StartedAt      *time.Time          `json:"started_at,omitempty"`
	FinishedAt     *time.Time          `json:"finished_at,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
}

type Stats struct {
	TotalRuns      int64      `json:"total_runs"`
	TotalSize      int64      `json:"total_size"`
	SuccessfulRuns int64      `json:"successful_runs"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
}

type CleanupRun struct {
	ID           int64
	Filename     string
	CreatedAt    time.Time
	SizeBytes    int64
	Destinations []DestinationResult
}

type ConfigInfo struct {
	Enabled           bool     `json:"enabled"`
	Name              string   `json:"name"`
	Directory         string   `json:"directory"`
	Destinations      []string `json:"destinations"`
	S3Prefix          string   `json:"s3_prefix"`
	PgDumpPath        string   `json:"pg_dump_path"`
	PgDumpAvailable   bool     `json:"pg_dump_available"`
	PgDumpVersion     string   `json:"pg_dump_version,omitempty"`
	TimeoutSeconds    int64    `json:"timeout_seconds"`
	RunCron           string   `json:"run_cron"`
	CleanCron         string   `json:"clean_cron"`
	KeepAllDays       int      `json:"keep_all_days"`
	KeepDailyDays     int      `json:"keep_daily_days"`
	KeepWeeklyWeeks   int      `json:"keep_weekly_weeks"`
	KeepMonthlyMonths int      `json:"keep_monthly_months"`
	KeepYearlyYears   int      `json:"keep_yearly_years"`
	MaxTotalMB        int64    `json:"max_total_mb"`
	MaxAgeHours       int      `json:"max_age_hours"`
	IncludeFiles      bool     `json:"include_files"`
	FilesPaths        []string `json:"files_paths"`
}

func mapRun(row dbgen.BackupRun) (Run, error) {
	var destinations []DestinationResult
	if len(row.Destinations) > 0 {
		if err := json.Unmarshal(row.Destinations, &destinations); err != nil {
			return Run{}, err
		}
	}
	if destinations == nil {
		// Rows written before encodeDestinations hold JSON null.
		destinations = []DestinationResult{}
	}
	run := Run{
		ID:           row.ID,
		Status:       row.Status,
		Trigger:      row.TriggerKind,
		ActorUserID:  row.ActorUserID,
		Filename:     row.Filename,
		Scope:        row.Scope,
		SizeBytes:    row.SizeBytes,
		Destinations: destinations,
		CreatedAt:    row.CreatedAt.Time,
	}
	if row.Uuid.Valid {
		run.UUID = uuid.UUID(row.Uuid.Bytes).String()
	}
	if row.ChecksumSha256 != nil {
		run.ChecksumSHA256 = *row.ChecksumSha256
	}
	if row.PgDumpVersion != nil {
		run.PgDumpVersion = *row.PgDumpVersion
	}
	if row.DatabaseName != nil {
		run.DatabaseName = *row.DatabaseName
	}
	if row.Error != nil {
		run.Error = *row.Error
	}
	run.StartedAt = timePtr(row.StartedAt)
	run.FinishedAt = timePtr(row.FinishedAt)
	return run, nil
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := value.Time
	return &parsed
}
