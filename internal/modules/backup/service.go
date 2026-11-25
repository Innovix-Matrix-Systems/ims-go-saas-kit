package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

const backupLockID int64 = 4804692

var (
	ErrDisabled       = apperr.New(apperr.Conflict, "Backups are disabled. Set BACKUP_ENABLED=true.")
	ErrAlreadyRunning = apperr.New(apperr.Conflict, "Another backup is already running; try again after it finishes.")
)

type DiskRegistry interface {
	Disk(name string) (storage.Disk, error)
}

type Options struct {
	Config     config.Backup
	Repository Repository
	Disks      DiskRegistry
	Pool       *pgxpool.Pool
	Queue      queue.Engine
	DSN        string
	AppVersion string
	Logger     *slog.Logger
}

type Service struct {
	cfg        config.Backup
	repo       Repository
	disks      DiskRegistry
	pool       *pgxpool.Pool
	queue      queue.Engine
	dsn        string
	appVersion string
	logger     *slog.Logger
	dumper     Dumper
	now        func() time.Time
}

type RunOptions struct {
	IncludeFiles *bool
	Destinations []string
	Filename     string
}

type Download struct {
	Reader   io.ReadCloser
	FileName string
	Size     int64
}

type CleanResult struct {
	DryRun     bool    `json:"dry_run"`
	Candidates int64   `json:"candidates"`
	Deleted    int64   `json:"deleted"`
	Failed     int64   `json:"failed"`
	IDs        []int64 `json:"ids"`
}

type MonitorResult struct {
	Status         string  `json:"status"`
	Summary        string  `json:"summary"`
	LastRun        *Run    `json:"last_run,omitempty"`
	AgeHours       float64 `json:"age_hours,omitempty"`
	TotalSize      int64   `json:"total_size"`
	SuccessfulRuns int64   `json:"successful_runs"`
	MaxAgeHours    int     `json:"max_age_hours"`
	MaxTotalMB     int64   `json:"max_total_mb"`
}

func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{
		cfg:        opts.Config,
		repo:       opts.Repository,
		disks:      opts.Disks,
		pool:       opts.Pool,
		queue:      opts.Queue,
		dsn:        opts.DSN,
		appVersion: opts.AppVersion,
		logger:     opts.Logger,
		dumper:     newPgDumper(opts.Config.PgDumpPath),
		now:        time.Now,
	}
}

func (s *Service) Enabled() bool { return s.cfg.Enabled }

func (s *Service) Get(ctx context.Context, id int64) (*Run, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]Run, error) {
	return s.repo.List(ctx, filter)
}

func (s *Service) Stats(ctx context.Context) (Stats, error) {
	return s.repo.Stats(ctx)
}

func (s *Service) Enqueue(ctx context.Context, trigger string, actorUserID int64) (_ *Run, err error) {
	ctx, end := tracing.Begin(ctx, "backup.enqueue", attribute.String("backup.trigger", trigger))
	defer end(&err)
	if !s.cfg.Enabled {
		return nil, ErrDisabled
	}
	if s.queue == nil {
		return nil, apperr.New(apperr.ServiceUnavailable, "The queue engine is unavailable.")
	}
	run, err := s.createRun(ctx, trigger, actorUserID, RunOptions{})
	if err != nil {
		return nil, err
	}
	if err := s.queue.Enqueue(ctx, KindRun, RunPayload{RunID: run.ID}, queue.EnqueueOptions{}); err != nil {
		updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_, _ = s.repo.Fail(updateCtx, run.ID, err.Error(), nil)
		cancel()
		return nil, err
	}
	return s.repo.Get(ctx, run.ID)
}

func (s *Service) RunNow(ctx context.Context, trigger string, actorUserID int64, opts RunOptions) (_ *Run, err error) {
	ctx, end := tracing.Begin(ctx, "backup.run_now", attribute.String("backup.trigger", trigger))
	defer end(&err)
	run, err := s.createRun(ctx, trigger, actorUserID, opts)
	if err != nil {
		return nil, err
	}
	if err := s.execute(ctx, run.ID, opts); err != nil {
		persisted, getErr := s.repo.Get(ctx, run.ID)
		if getErr == nil {
			return persisted, err
		}
		return nil, err
	}
	return s.repo.Get(ctx, run.ID)
}

func (s *Service) Execute(ctx context.Context, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "backup.execute", attribute.Int64("backup.id", id))
	defer end(&err)
	return s.execute(ctx, id, RunOptions{})
}

func (s *Service) Open(ctx context.Context, id int64) (_ *Download, err error) {
	ctx, end := tracing.Begin(ctx, "backup.open", attribute.Int64("backup.id", id))
	defer end(&err)
	run, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	localPath := filepath.Join(s.cfg.Dir, run.Filename)
	if file, err := os.Open(localPath); err == nil {
		return &Download{Reader: file, FileName: run.Filename, Size: run.SizeBytes}, nil
	}
	for _, destination := range run.Destinations {
		if destination.Key == "" {
			continue
		}
		disk, err := s.disks.Disk(destination.Destination)
		if err != nil {
			continue
		}
		reader, err := disk.Get(ctx, destination.Key)
		if err != nil {
			continue
		}
		return &Download{Reader: reader, FileName: run.Filename, Size: run.SizeBytes}, nil
	}
	return nil, apperr.New(apperr.ResourceNotFound, "The backup archive is not available.")
}

func (s *Service) Delete(ctx context.Context, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "backup.delete", attribute.Int64("backup.id", id))
	defer end(&err)
	run, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if run.Status == StatusRunning || run.Status == StatusPending {
		return apperr.New(apperr.Conflict, "A running backup cannot be deleted.")
	}
	if err := s.deleteRunFiles(ctx, run.Filename, run.Destinations); err != nil {
		return apperr.Wrap(apperr.ServiceUnavailable, "The backup files could not be removed.", err)
	}
	deleted, err := s.repo.SoftDelete(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperr.Newf(apperr.ResourceNotFound, "Backup run with ID '%d' was not found.", id)
	}
	return nil
}

func (s *Service) Clean(ctx context.Context, dryRun bool) (_ CleanResult, err error) {
	ctx, end := tracing.Begin(ctx, "backup.clean", attribute.Bool("backup.dry_run", dryRun))
	defer end(&err)
	runs, err := s.repo.ForCleanup(ctx)
	if err != nil {
		return CleanResult{}, err
	}
	retentionRuns := make([]RetentionRun, 0, len(runs))
	for _, run := range runs {
		retentionRuns = append(retentionRuns, RetentionRun{
			ID:        run.ID,
			CreatedAt: run.CreatedAt,
			SizeBytes: run.SizeBytes,
		})
	}
	ids := SelectForDeletion(retentionRuns, s.now(), s.policy())
	result := CleanResult{
		DryRun:     dryRun,
		Candidates: int64(len(ids)),
		IDs:        ids,
	}
	if dryRun {
		return result, nil
	}
	byID := make(map[int64]CleanupRun, len(runs))
	for _, run := range runs {
		byID[run.ID] = run
	}
	for _, id := range ids {
		run, ok := byID[id]
		if !ok {
			continue
		}
		if err := s.deleteRunFiles(ctx, run.Filename, run.Destinations); err != nil {
			s.logger.WarnContext(ctx, "backup cleanup could not remove files", "run_id", id, "error", err)
			result.Failed++
			continue
		}
		deleted, err := s.repo.SoftDelete(ctx, id)
		if err != nil {
			s.logger.WarnContext(ctx, "backup cleanup could not mark run deleted", "run_id", id, "error", err)
			result.Failed++
			continue
		}
		if deleted {
			result.Deleted++
		}
	}
	return result, nil
}

func (s *Service) Monitor(ctx context.Context) (_ MonitorResult, err error) {
	ctx, end := tracing.Begin(ctx, "backup.monitor")
	defer end(&err)
	stats, err := s.repo.Stats(ctx)
	if err != nil {
		return MonitorResult{}, err
	}
	result := MonitorResult{
		TotalSize:      stats.TotalSize,
		SuccessfulRuns: stats.SuccessfulRuns,
		MaxAgeHours:    s.cfg.MaxAgeHours,
		MaxTotalMB:     s.cfg.MaxTotalMB,
	}
	run, err := s.repo.LatestSuccessful(ctx)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			result.Status = "failed"
			result.Summary = "No successful backup has been recorded yet."
			return result, nil
		}
		return MonitorResult{}, err
	}
	result.LastRun = run
	finished := run.CreatedAt
	if run.FinishedAt != nil {
		finished = *run.FinishedAt
	}
	age := s.now().Sub(finished)
	if age < 0 {
		age = 0
	}
	result.AgeHours = age.Hours()
	result.Status = "ok"
	result.Summary = fmt.Sprintf("Last successful backup is %.1f hours old.", age.Hours())
	if s.cfg.MaxAgeHours > 0 && age > time.Duration(s.cfg.MaxAgeHours)*time.Hour {
		result.Status = "warning"
		result.Summary = fmt.Sprintf("Last successful backup is %.1f hours old (threshold %d hours).", age.Hours(), s.cfg.MaxAgeHours)
	}
	if s.cfg.MaxTotalMB > 0 && stats.TotalSize > s.cfg.MaxTotalMB*1024*1024 {
		result.Status = "warning"
		result.Summary += fmt.Sprintf(" Backup storage uses %d MB (threshold %d MB).", stats.TotalSize/(1024*1024), s.cfg.MaxTotalMB)
	}
	return result, nil
}

func (s *Service) Config(ctx context.Context) ConfigInfo {
	version, err := s.dumper.Version(ctx)
	available := err == nil
	info := ConfigInfo{
		Enabled:           s.cfg.Enabled,
		Name:              s.cfg.Name,
		Directory:         s.cfg.Dir,
		Destinations:      append([]string(nil), s.cfg.Destinations...),
		S3Prefix:          s.cfg.S3Prefix,
		PgDumpPath:        s.cfg.PgDumpPath,
		PgDumpAvailable:   available,
		PgDumpVersion:     version,
		TimeoutSeconds:    int64(s.cfg.Timeout.Seconds()),
		RunCron:           s.cfg.RunCron,
		CleanCron:         s.cfg.CleanCron,
		KeepAllDays:       s.cfg.KeepAllDays,
		KeepDailyDays:     s.cfg.KeepDailyDays,
		KeepWeeklyWeeks:   s.cfg.KeepWeeklyWeeks,
		KeepMonthlyMonths: s.cfg.KeepMonthlyMonths,
		KeepYearlyYears:   s.cfg.KeepYearlyYears,
		MaxTotalMB:        s.cfg.MaxTotalMB,
		MaxAgeHours:       s.cfg.MaxAgeHours,
		IncludeFiles:      s.cfg.IncludeFiles,
		FilesPaths:        append([]string(nil), s.cfg.FilesPaths...),
	}
	return info
}

func (s *Service) createRun(ctx context.Context, trigger string, actorUserID int64, opts RunOptions) (*Run, error) {
	switch trigger {
	case TriggerCLI, TriggerSchedule, TriggerDashboard:
	default:
		return nil, apperr.Validation(map[string][]string{"trigger": {"The backup trigger is invalid."}})
	}
	includeFiles := s.cfg.IncludeFiles
	if opts.IncludeFiles != nil {
		includeFiles = *opts.IncludeFiles
	}
	scope := ScopeDatabase
	if includeFiles {
		scope = ScopeDatabaseFiles
	}
	filename := opts.Filename
	if filename == "" {
		filename = s.defaultFilename(s.now())
	}
	filename = s.sanitizeFilename(filename)
	return s.repo.Create(ctx, CreateRunInput{
		Trigger:      trigger,
		ActorUserID:  actorUserID,
		Filename:     filename,
		Scope:        scope,
		DatabaseName: databaseNameFromDSN(s.dsn),
	})
}

func (s *Service) execute(ctx context.Context, id int64, opts RunOptions) error {
	run, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if run.Status != StatusPending {
		return nil
	}
	release, acquired, err := s.acquireLock(ctx)
	if err != nil {
		return s.failRun(ctx, id, err, nil)
	}
	if !acquired {
		return s.failRun(ctx, id, ErrAlreadyRunning, nil)
	}
	defer release()

	if _, err := s.repo.SetRunning(ctx, id); err != nil {
		return err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	includeFiles := s.cfg.IncludeFiles
	if opts.IncludeFiles != nil {
		includeFiles = *opts.IncludeFiles
	}
	destinations, err := normalizeDestinations(opts.Destinations)
	if err != nil {
		return s.failRun(ctx, id, err, nil)
	}
	if len(opts.Destinations) == 0 {
		destinations = append([]string(nil), s.cfg.Destinations...)
	}
	scope := ScopeDatabase
	if includeFiles {
		scope = ScopeDatabaseFiles
	}

	tmpDir := filepath.Join(s.cfg.Dir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return s.failRun(ctx, id, fmt.Errorf("create backup temporary directory: %w", err), nil)
	}
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		return s.failRun(ctx, id, fmt.Errorf("secure backup temporary directory: %w", err), nil)
	}
	dumpPath := filepath.Join(tmpDir, fmt.Sprintf("dump-%d.dump", id))
	defer os.Remove(dumpPath)

	info, err := s.dumper.Dump(timeoutCtx, s.dsn, dumpPath)
	if err != nil {
		return s.failRun(ctx, id, apperr.Wrap(apperr.ServiceUnavailable, "The database dump failed.", err), nil)
	}

	filename := s.uniqueFilename(run.Filename, id)
	archive, err := createArchive(timeoutCtx, s.cfg.Dir, ArchiveInput{
		Name:          s.cfg.Name,
		Environment:   s.cfg.Environment,
		AppVersion:    s.appVersion,
		Database:      info.Database,
		PgDumpVersion: info.Version,
		Scope:         scope,
		Filename:      filename,
		DumpPath:      dumpPath,
		CreatedAt:     s.now(),
		IncludeFiles:  includeFiles,
		FilesPaths:    s.cfg.FilesPaths,
	})
	if err != nil {
		return s.failRun(ctx, id, apperr.Wrap(apperr.InternalServerError, "The backup archive could not be created.", err), nil)
	}

	results, status := s.copyDestinations(timeoutCtx, archive, destinations)
	message := destinationError(results)
	updateCtx, cancelUpdate := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelUpdate()
	if _, err = s.repo.Complete(updateCtx, id, CompleteInput{
		Status:         status,
		Filename:       archive.Filename,
		SizeBytes:      archive.Size,
		ChecksumSHA256: archive.SHA256,
		PgDumpVersion:  info.Version,
		DatabaseName:   info.Database,
		Destinations:   results,
		ErrorMessage:   message,
	}); err != nil {
		return err
	}
	if status == StatusFailed {
		return apperr.New(apperr.ServiceUnavailable, "The backup could not be stored on any destination.")
	}
	return nil
}

func (s *Service) copyDestinations(ctx context.Context, archive *ArchiveResult, destinations []string) ([]DestinationResult, string) {
	results := make([]DestinationResult, 0, len(destinations))
	for _, destination := range destinations {
		switch destination {
		case DestinationLocal:
			results = append(results, DestinationResult{
				Destination: DestinationLocal,
				Status:      StatusCompleted,
				Path:        archive.Path,
				SizeBytes:   archive.Size,
			})
		case DestinationS3:
			result := DestinationResult{Destination: DestinationS3, SizeBytes: archive.Size}
			disk, err := s.disks.Disk(storage.DiskS3)
			if err != nil {
				result.Status = StatusFailed
				result.Error = err.Error()
				results = append(results, result)
				continue
			}
			file, err := os.Open(archive.Path)
			if err != nil {
				result.Status = StatusFailed
				result.Error = err.Error()
				results = append(results, result)
				continue
			}
			key := s.s3Key(archive.Filename)
			err = disk.Put(ctx, key, file, archive.Size)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				result.Status = StatusFailed
				result.Error = err.Error()
			} else {
				result.Status = StatusCompleted
				result.Key = key
			}
			results = append(results, result)
		}
	}
	completed := 0
	for _, result := range results {
		if result.Status == StatusCompleted {
			completed++
		}
	}
	switch {
	case completed == len(results) && completed > 0:
		return results, StatusCompleted
	case completed > 0:
		return results, StatusPartial
	default:
		return results, StatusFailed
	}
}

func (s *Service) failRun(ctx context.Context, id int64, err error, destinations []DestinationResult) error {
	s.logger.ErrorContext(ctx, "backup failed", "run_id", id, "error", err)
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, failErr := s.repo.Fail(updateCtx, id, err.Error(), destinations); failErr != nil {
		s.logger.ErrorContext(ctx, "backup failure could not be recorded", "run_id", id, "error", failErr)
	}
	return err
}

func (s *Service) deleteRunFiles(ctx context.Context, filename string, destinations []DestinationResult) error {
	var failures []error
	localPath := filepath.Join(s.cfg.Dir, filename)
	if err := os.Remove(localPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		failures = append(failures, fmt.Errorf("remove local backup %q: %w", localPath, err))
	}
	for _, destination := range destinations {
		if destination.Key == "" {
			continue
		}
		disk, err := s.disks.Disk(destination.Destination)
		if err != nil {
			failures = append(failures, fmt.Errorf("open %s disk: %w", destination.Destination, err))
			continue
		}
		if err := disk.Delete(ctx, destination.Key); err != nil {
			failures = append(failures, fmt.Errorf("remove %s object %q: %w", destination.Destination, destination.Key, err))
		}
	}
	return errors.Join(failures...)
}

func (s *Service) acquireLock(ctx context.Context) (func(), bool, error) {
	if s.pool == nil {
		return func() {}, true, nil
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", backupLockID).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", backupLockID)
		conn.Release()
	}
	return release, true, nil
}

func (s *Service) policy() RetentionPolicy {
	return RetentionPolicy{
		KeepAllDays:       s.cfg.KeepAllDays,
		KeepDailyDays:     s.cfg.KeepDailyDays,
		KeepWeeklyWeeks:   s.cfg.KeepWeeklyWeeks,
		KeepMonthlyMonths: s.cfg.KeepMonthlyMonths,
		KeepYearlyYears:   s.cfg.KeepYearlyYears,
		MaxTotalMB:        s.cfg.MaxTotalMB,
	}
}

func (s *Service) defaultFilename(now time.Time) string {
	return fmt.Sprintf("%s-%s-%s.tar.gz",
		sanitizeSegment(s.cfg.Name, "backup"),
		sanitizeSegment(s.cfg.Environment, "env"),
		now.UTC().Format("2006-01-02-150405"),
	)
}

func (s *Service) sanitizeFilename(value string) string {
	value = filepath.Base(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	name := strings.Trim(builder.String(), "._-")
	if name == "" {
		name = s.defaultFilename(s.now())
	}
	if !strings.HasSuffix(strings.ToLower(name), ".tar.gz") {
		name += ".tar.gz"
	}
	return name
}

func (s *Service) uniqueFilename(filename string, id int64) string {
	stem := strings.TrimSuffix(filename, ".tar.gz")
	candidate := filename
	if _, err := os.Stat(filepath.Join(s.cfg.Dir, candidate)); err == nil {
		candidate = fmt.Sprintf("%s-%d.tar.gz", stem, id)
	}
	for index := 1; ; index++ {
		if _, err := os.Stat(filepath.Join(s.cfg.Dir, candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d-%d.tar.gz", stem, id, index)
	}
}

func (s *Service) s3Key(filename string) string {
	prefix := strings.Trim(s.cfg.S3Prefix, "/")
	if prefix == "" {
		prefix = "backups"
	}
	return path.Join(prefix, sanitizeSegment(s.cfg.Environment, "env"), filename)
}

func sanitizeSegment(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	clean := strings.Trim(builder.String(), "-_")
	if clean == "" {
		return fallback
	}
	return clean
}

func normalizeDestinations(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := map[string]bool{DestinationLocal: true}
	destinations := []string{DestinationLocal}
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" || seen[value] {
			continue
		}
		switch value {
		case DestinationLocal, DestinationS3:
		default:
			return nil, apperr.Validation(map[string][]string{
				"destinations": {"Destinations must be local or s3."},
			})
		}
		seen[value] = true
		destinations = append(destinations, value)
	}
	return destinations, nil
}

func destinationError(results []DestinationResult) string {
	messages := make([]string, 0)
	for _, result := range results {
		if result.Status == StatusFailed && result.Error != "" {
			messages = append(messages, result.Destination+": "+result.Error)
		}
	}
	return strings.Join(messages, "; ")
}

func databaseNameFromDSN(dsn string) string {
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return ""
	}
	return parsed.Database
}
