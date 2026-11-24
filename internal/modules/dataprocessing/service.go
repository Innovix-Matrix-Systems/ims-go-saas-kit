package dataprocessing

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// CleanupSettingKey drives the retention window of the cleanup task.
const CleanupSettingKey = "data_processing_job.cleanup_days"

// cleanupPageSize bounds the cleanup batch per round trip.
const cleanupPageSize = 500

// Service implements processing-job tracking rules.
type Service struct {
	repo    Repository
	disks   *storage.Registry
	cfg     config.Processing
	logger  *slog.Logger
	hasDisk bool
}

// NewService creates the processing-job service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// WithFiles attaches the storage registry so cleanup also removes job files.
func (s *Service) WithFiles(disks *storage.Registry, cfg config.Processing) *Service {
	s.disks = disks
	s.cfg = cfg
	s.hasDisk = true
	return s
}

// WithLogger attaches the service logger.
func (s *Service) WithLogger(logger *slog.Logger) *Service {
	s.logger = logger
	return s
}

// Create registers a new tracked job in the effective scope. The returned job
// carries the generated public job_id (UUID) used by the API routes.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, jobType string, entityType *string, totalItems int32, filters []byte, userID int64) (_ *Job, err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.create",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.String("job.type", jobType),
	)
	defer end(&err)
	if jobType != TypeImport && jobType != TypeExport && jobType != TypePrivacyExport {
		return nil, apperr.Newf(apperr.InvalidInput, "Processing job type %q is not supported.", jobType)
	}
	var tenantID *int64
	if effective := scope.EffectiveTenantID(); effective > 0 {
		tenantID = &effective
	}
	var branchID *int64
	if effective := scope.EffectiveBranchID(); effective > 0 {
		branchID = &effective
	}
	var actor *int64
	if userID > 0 {
		actor = &userID
	}
	return s.repo.Create(ctx, CreateInput{
		TenantID:   tenantID,
		BranchID:   branchID,
		JobID:      uuid.NewString(),
		Type:       jobType,
		Status:     StatusPending,
		EntityType: entityType,
		Filters:    filters,
		TotalItems: totalItems,
		UserID:     actor,
	})
}

// Get loads a job respecting the tenant scope (internal scope sees all).
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*Job, error) {
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !inScope(scope, job) {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Processing job with ID '%d' was not found.", id)
	}
	return job, nil
}

// GetByJobID loads a job by its public UUID.
func (s *Service) GetByJobID(ctx context.Context, scope tenancy.Scope, jobID string) (*Job, error) {
	job, err := s.repo.GetByJobID(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if !inScope(scope, job) {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Processing job '%s' was not found.", jobID)
	}
	return job, nil
}

func inScope(scope tenancy.Scope, job *Job) bool {
	if scope.AllTenants() {
		return true
	}
	if job.TenantID == nil {
		return false
	}
	return *job.TenantID == scope.EffectiveTenantID()
}

// List returns jobs with pagination and filters.
func (s *Service) List(ctx context.Context, scope tenancy.Scope, filter ListFilter) ([]Job, int64, error) {
	if scope.AllTenants() {
		filter.AllTenants = true
		filter.TenantID = 0
	} else {
		filter.TenantID = scope.EffectiveTenantID()
	}

	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ActorNames returns the names of the people who started the jobs.
func (s *Service) ActorNames(ctx context.Context, jobs []Job) map[int64]string {
	ids := make([]int64, 0, len(jobs))
	seen := map[int64]bool{}
	for _, job := range jobs {
		if job.UserID != nil && !seen[*job.UserID] {
			seen[*job.UserID] = true
			ids = append(ids, *job.UserID)
		}
	}
	names, err := s.repo.ActorNames(ctx, ids)
	if err != nil {
		return map[int64]string{}
	}
	return names
}

// Download is a resolved job artifact stream.
type Download struct {
	Reader   io.ReadCloser
	FileName string
	Mime     string
	Size     int64
}

// OpenDownload resolves the artifact of a completed job; jobs without an
// existing file are reported as not found.
func (s *Service) OpenDownload(ctx context.Context, scope tenancy.Scope, jobID string) (_ *Download, err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.open_download",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
	)
	defer end(&err)
	job, err := s.GetByJobID(ctx, scope, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status != StatusCompleted || job.FilePath == nil || *job.FilePath == "" {
		return nil, apperr.New(apperr.ResourceNotFound, "The job file is not available for download.")
	}
	if !s.hasDisk {
		return nil, apperr.New(apperr.ServiceUnavailable, "The file storage is not available.")
	}
	reader, diskFile, err := s.openFirstAvailable(ctx, job.FileDisk, *job.FilePath)
	if err != nil {
		return nil, err
	}
	fileName := "download"
	if job.FileName != nil && *job.FileName != "" {
		fileName = *job.FileName
	}
	download := &Download{
		Reader:   reader,
		FileName: fileName,
		Size:     diskFile.size,
	}
	if job.FileMime != nil {
		download.Mime = *job.FileMime
	}
	return download, nil
}

type diskFileInfo struct {
	path string
	size int64
}

func (s *Service) openFirstAvailable(ctx context.Context, jobDisk *string, path string) (io.ReadCloser, diskFileInfo, error) {
	for _, name := range s.diskCandidates(jobDisk) {
		disk, err := s.disks.Disk(name)
		if err != nil {
			continue
		}
		exists, err := disk.Exists(ctx, path)
		if err != nil || !exists {
			continue
		}
		size, _ := disk.Size(ctx, path)
		reader, err := disk.Get(ctx, path)
		if err != nil {
			continue
		}
		return reader, diskFileInfo{path: path, size: size}, nil
	}
	return nil, diskFileInfo{}, apperr.New(apperr.ResourceNotFound, "The job file is not available for download.")
}

func (s *Service) diskCandidates(jobDisk *string) []string {
	candidates := make([]string, 0, 2)
	if jobDisk != nil && *jobDisk != "" {
		candidates = append(candidates, *jobDisk)
	}
	if s.cfg.Disk != "" {
		duplicate := false
		for _, candidate := range candidates {
			if candidate == s.cfg.Disk {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, s.cfg.Disk)
		}
	}
	return candidates
}

// MarkProcessing marks a job as running.
func (s *Service) MarkProcessing(ctx context.Context, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.mark_processing", attribute.Int64("job.id", id))
	defer end(&err)
	_, err = s.repo.UpdateStatus(ctx, id, StatusProcessing, nil)
	return err
}

// MarkCompleted finishes a job successfully.
func (s *Service) MarkCompleted(ctx context.Context, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.mark_completed", attribute.Int64("job.id", id))
	defer end(&err)
	_, err = s.repo.UpdateStatus(ctx, id, StatusCompleted, nil)
	return err
}

// MarkFailed finishes a job with an error message.
func (s *Service) MarkFailed(ctx context.Context, id int64, message string) (err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.mark_failed", attribute.Int64("job.id", id))
	defer end(&err)
	_, err = s.repo.UpdateStatus(ctx, id, StatusFailed, &message)
	return err
}

// UpdateProgress records the row totals and processed/success/error counters.
func (s *Service) UpdateProgress(ctx context.Context, id int64, total, processed, success int32, errors []map[string]any) error {
	encoded, err := json.Marshal(errors)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "encode job errors", err)
	}
	return s.repo.UpdateProgress(ctx, id, total, processed, success, int32(len(errors)), encoded)
}

// AttachFile records the generated export/import file.
func (s *Service) AttachFile(ctx context.Context, id int64, file FileInput) (err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.attach_file",
		attribute.Int64("job.id", id),
		attribute.Int64("file.size", file.Size),
	)
	defer end(&err)
	if err := s.repo.UpdateFile(ctx, id, file); err != nil {
		return err
	}
	return s.registerOutput(ctx, id, file)
}

// CleanupCompleted deletes finished jobs older than the retention window
// (files included) and reports how many rows were removed.
func (s *Service) CleanupCompleted(ctx context.Context, retentionDays int) (_ int64, err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.cleanup_completed", attribute.Int("retention.days", retentionDays))
	defer end(&err)
	var deleted int64
	for {
		jobs, err := s.repo.ListExpired(ctx, retentionDays, cleanupPageSize)
		if err != nil {
			return deleted, err
		}
		if len(jobs) == 0 {
			return deleted, nil
		}
		ids := make([]int64, 0, len(jobs))
		for _, job := range jobs {
			ids = append(ids, job.ID)
			s.removeJobFile(ctx, job)
		}
		removed, err := s.repo.DeleteByIDs(ctx, ids)
		if err != nil {
			return deleted, err
		}
		deleted += removed
		if len(jobs) < cleanupPageSize {
			return deleted, nil
		}
	}
}

// CleanupType deletes finished jobs of one type older than maxAgeHours, with
// their files (privacy exports expire after privacy.export_ttl_hours).
func (s *Service) CleanupType(ctx context.Context, jobType string, maxAgeHours int) (_ int64, err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.cleanup_type",
		attribute.String("job.type", jobType),
		attribute.Int("max_age.hours", maxAgeHours),
	)
	defer end(&err)
	var deleted int64
	for {
		jobs, err := s.repo.ListExpiredOfType(ctx, jobType, maxAgeHours, cleanupPageSize)
		if err != nil {
			return deleted, err
		}
		if len(jobs) == 0 {
			return deleted, nil
		}
		ids := make([]int64, 0, len(jobs))
		for _, job := range jobs {
			ids = append(ids, job.ID)
			s.removeJobFile(ctx, job)
		}
		removed, err := s.repo.DeleteByIDs(ctx, ids)
		if err != nil {
			return deleted, err
		}
		deleted += removed
		if len(jobs) < cleanupPageSize {
			return deleted, nil
		}
	}
}

// removeJobFile deletes the stored artifact from the first disk that has it.
func (s *Service) removeJobFile(ctx context.Context, job ExpiredJob) {
	if err := s.repo.RemoveOutputFile(ctx, job.ID); err != nil && s.logger != nil {
		s.logger.WarnContext(ctx, "processing job files entry cleanup failed", "error", err)
	}
	if !s.hasDisk || job.FilePath == nil || *job.FilePath == "" {
		return
	}
	for _, name := range s.diskCandidates(job.FileDisk) {
		disk, err := s.disks.Disk(name)
		if err != nil {
			continue
		}
		exists, err := disk.Exists(ctx, *job.FilePath)
		if err != nil || !exists {
			continue
		}
		if err := disk.Delete(ctx, *job.FilePath); err != nil && s.logger != nil {
			s.logger.WarnContext(ctx, "processing job file cleanup failed", "error", err)
		}
		return
	}
}

// CountCompleted reports how many jobs the cleanup would delete.
func (s *Service) CountCompleted(ctx context.Context, retentionDays int) (int64, error) {
	return s.repo.CountCompletedBefore(ctx, retentionDays)
}

// CleanupSetting resolves the configured retention window.
func CleanupSetting(ctx context.Context, settingsService *settings.Service, scope tenancy.Scope) int {
	days, err := settingsService.GetInt(ctx, scope, CleanupSettingKey)
	if err != nil || days < 0 {
		for _, key := range settings.Catalog {
			if key.Name == CleanupSettingKey {
				if fallback, ok := key.Default.(int); ok {
					return fallback
				}
			}
		}
		return 30
	}
	return days
}

// CleanupTask builds the scheduled cleanup handler.
func CleanupTask(service *Service, settingsService *settings.Service, logger *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		scope := tenancy.Scope{Internal: true}
		days := CleanupSetting(ctx, settingsService, scope)
		deleted, err := service.CleanupCompleted(ctx, days)
		if err != nil {
			return err
		}
		logger.Info("data processing cleanup completed", "retention_days", days, "deleted", deleted)
		return nil
	}
}
