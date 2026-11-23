package config

import (
	"path/filepath"
	"strings"
	"time"
)

// Data-processing artifacts and database backups (DATA_PROCESSING_*, BACKUP_*).

// Processing holds the disk and path prefix for data-processing artifacts.
type Processing struct {
	Disk     string
	BasePath string
}

// Backup holds the database backup settings. Local storage is mandatory;
// S3/R2 object storage is optional.
type Backup struct {
	Enabled           bool
	Name              string
	Environment       string
	Dir               string
	Destinations      []string
	S3Prefix          string
	PgDumpPath        string
	Timeout           time.Duration
	RunCron           string
	CleanCron         string
	KeepAllDays       int
	KeepDailyDays     int
	KeepWeeklyWeeks   int
	KeepMonthlyMonths int
	KeepYearlyYears   int
	MaxTotalMB        int64
	MaxAgeHours       int
	IncludeFiles      bool
	FilesPaths        []string
}

func (c *Config) loadProcessing() error {
	c.Processing = Processing{
		Disk:     getString("DATA_PROCESSING_DISK", "private"),
		BasePath: strings.Trim(getString("DATA_PROCESSING_BASE_PATH", "jobs"), "/"),
	}
	return nil
}

func (c *Config) loadBackup() error {
	enabled, err := getBool("BACKUP_ENABLED", false)
	if err != nil {
		return err
	}
	timeout, err := getDuration("BACKUP_TIMEOUT", time.Hour)
	if err != nil {
		return err
	}
	maxTotalMB, err := getInt64("BACKUP_MAX_TOTAL_MB", 0)
	if err != nil {
		return err
	}
	maxAgeHours, err := getInt("BACKUP_MAX_AGE_HOURS", 26)
	if err != nil {
		return err
	}
	includeFiles, err := getBool("BACKUP_INCLUDE_FILES", false)
	if err != nil {
		return err
	}
	keepAllDays, err := getInt("BACKUP_KEEP_ALL_DAYS", 7)
	if err != nil {
		return err
	}
	keepDailyDays, err := getInt("BACKUP_KEEP_DAILY_DAYS", 16)
	if err != nil {
		return err
	}
	keepWeeklyWeeks, err := getInt("BACKUP_KEEP_WEEKLY_WEEKS", 8)
	if err != nil {
		return err
	}
	keepMonthlyMonths, err := getInt("BACKUP_KEEP_MONTHLY_MONTHS", 4)
	if err != nil {
		return err
	}
	keepYearlyYears, err := getInt("BACKUP_KEEP_YEARLY_YEARS", 2)
	if err != nil {
		return err
	}
	dir := getString("BACKUP_DIR", "backups")
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(c.App.StorageDir, dir)
	}
	c.Backup = Backup{
		Enabled:           enabled,
		Name:              getString("BACKUP_NAME", c.App.Name),
		Environment:       c.App.Env,
		Dir:               filepath.Clean(dir),
		Destinations:      backupDestinations(getString("BACKUP_DESTINATIONS", "local")),
		S3Prefix:          strings.Trim(getString("BACKUP_S3_PREFIX", "backups"), "/"),
		PgDumpPath:        getString("BACKUP_PG_DUMP_PATH", "pg_dump"),
		Timeout:           timeout,
		RunCron:           getString("BACKUP_RUN_CRON", "30 1 * * *"),
		CleanCron:         getString("BACKUP_CLEAN_CRON", "30 2 * * *"),
		KeepAllDays:       keepAllDays,
		KeepDailyDays:     keepDailyDays,
		KeepWeeklyWeeks:   keepWeeklyWeeks,
		KeepMonthlyMonths: keepMonthlyMonths,
		KeepYearlyYears:   keepYearlyYears,
		MaxTotalMB:        maxTotalMB,
		MaxAgeHours:       maxAgeHours,
		IncludeFiles:      includeFiles,
		FilesPaths:        splitCSV(getString("BACKUP_FILES_PATHS", "")),
	}
	return nil
}

func backupDestinations(raw string) []string {
	seen := map[string]bool{"local": true}
	destinations := []string{"local"}
	for _, name := range splitCSV(raw) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		destinations = append(destinations, name)
	}
	return destinations
}
