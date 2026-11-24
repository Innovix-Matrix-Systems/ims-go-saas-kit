// Package dataprocessing tracks background import/export jobs (progress,
// results and files) and provides their cleanup task.
package dataprocessing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Job lifecycle statuses.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// Job types.
const (
	TypeImport = "import"
	TypeExport = "export"
	// TypePrivacyExport is a user's personal-data archive
	// (internal/modules/privacy). Only the user who asked can read or
	// download it: data-processing.view.all does not reach it.
	TypePrivacyExport = "privacy_export"
)

// Job is the domain representation of a processing job.
type Job struct {
	ID             int64
	TenantID       *int64
	BranchID       *int64
	QueueJobID     *string
	Type           string
	Status         string
	EntityType     *string
	Filters        []byte
	TotalItems     int32
	ProcessedItems int32
	SuccessCount   int32
	ErrorCount     int32
	Errors         []byte
	ErrorMessage   *string
	FileName       *string
	FileDisk       *string
	FilePath       *string
	FileSize       *int64
	FileMime       *string
	UserID         *int64
	StartedAt      *time.Time
	CompletedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProgressPercentage reports completion in the 0..100 range.
func (j Job) ProgressPercentage() int {
	if j.TotalItems == 0 {
		return 0
	}
	percentage := int((int64(j.ProcessedItems) * 100) / int64(j.TotalItems))
	if percentage > 100 {
		return 100
	}
	return percentage
}

// CreateInput carries job creation input.
type CreateInput struct {
	TenantID   *int64
	BranchID   *int64
	JobID      string
	Type       string
	Status     string
	EntityType *string
	Filters    []byte
	TotalItems int32
	UserID     *int64
}

// ListFilter carries job list filters.
type ListFilter struct {
	AllTenants bool
	TenantID   int64
	Status     string
	Type       string
	Search     string
	// UserID keeps one person's jobs; zero keeps everyone's.
	UserID int64
	// ViewerID is who reads: personal-data exports are listed to their
	// owner only.
	ViewerID int64
	Limit    int32
	Offset   int32
}

// ExpiredJob carries the file metadata needed to clean up one expired job.
type ExpiredJob struct {
	ID       int64
	JobID    *string
	Status   string
	FileDisk *string
	FilePath *string
	FileName *string
}

// FileInput carries generated-file metadata.
type FileInput struct {
	Name string
	Disk string
	Path string
	Size int64
	Mime string
}

// Repository is the persistence contract for processing jobs.
type Repository interface {
	Create(ctx context.Context, input CreateInput) (*Job, error)
	Get(ctx context.Context, id int64) (*Job, error)
	GetByJobID(ctx context.Context, jobID string) (*Job, error)
	List(ctx context.Context, filter ListFilter) ([]Job, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	UpdateStatus(ctx context.Context, id int64, status string, errorMessage *string) (*Job, error)
	UpdateProgress(ctx context.Context, id int64, total, processed, success, errorCount int32, errorsJSON []byte) error
	UpdateFile(ctx context.Context, id int64, file FileInput) error
	DeleteCompletedBefore(ctx context.Context, retentionDays int) (int64, error)
	CountCompletedBefore(ctx context.Context, retentionDays int) (int64, error)
	ListExpired(ctx context.Context, retentionDays int, limit int32) ([]ExpiredJob, error)
	ListExpiredOfType(ctx context.Context, jobType string, maxAgeHours int, limit int32) ([]ExpiredJob, error)
	DeleteByIDs(ctx context.Context, ids []int64) (int64, error)
	ActorNames(ctx context.Context, userIDs []int64) (map[int64]string, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*Job, error) {
	row, err := r.queries.CreateDataProcessingJob(ctx, dbgen.CreateDataProcessingJobParams{
		TenantID:   input.TenantID,
		BranchID:   input.BranchID,
		JobID:      &input.JobID,
		Type:       input.Type,
		Status:     input.Status,
		EntityType: input.EntityType,
		Filters:    input.Filters,
		TotalItems: input.TotalItems,
		UserID:     input.UserID,
	})
	if err != nil {
		return nil, database.MapError("dataprocessing.create", err)
	}
	job := mapJob(row)
	return &job, nil
}

func (r *repository) Get(ctx context.Context, id int64) (*Job, error) {
	row, err := r.queries.GetDataProcessingJob(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Processing job with ID '%d' was not found.", id)
		}
		return nil, database.MapError("dataprocessing.get", err)
	}
	job := mapJob(row)
	return &job, nil
}

func (r *repository) GetByJobID(ctx context.Context, jobID string) (*Job, error) {
	row, err := r.queries.GetDataProcessingJobByJobID(ctx, &jobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Processing job '%s' was not found.", jobID)
		}
		return nil, database.MapError("dataprocessing.get_by_job_id", err)
	}
	job := mapJob(row)
	return &job, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]Job, error) {
	rows, err := r.queries.ListDataProcessingJobs(ctx, dbgen.ListDataProcessingJobsParams{
		AllTenants: filter.AllTenants,
		TenantID:   optionalInt64(filter.TenantID),
		Status:     optionalString(filter.Status),
		Type:       optionalString(filter.Type),
		UserID:     optionalInt64(filter.UserID),
		ViewerID:   filter.ViewerID,
		Search:     filter.Search,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("dataprocessing.list", err)
	}
	items := make([]Job, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapJob(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.CountDataProcessingJobs(ctx, dbgen.CountDataProcessingJobsParams{
		AllTenants: filter.AllTenants,
		TenantID:   optionalInt64(filter.TenantID),
		Status:     optionalString(filter.Status),
		Type:       optionalString(filter.Type),
		UserID:     optionalInt64(filter.UserID),
		ViewerID:   filter.ViewerID,
		Search:     filter.Search,
	})
	if err != nil {
		return 0, database.MapError("dataprocessing.count", err)
	}
	return total, nil
}

func (r *repository) UpdateStatus(ctx context.Context, id int64, status string, errorMessage *string) (*Job, error) {
	row, err := r.queries.UpdateDataProcessingJobStatus(ctx, dbgen.UpdateDataProcessingJobStatusParams{
		ID:           id,
		Status:       status,
		ErrorMessage: errorMessage,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Processing job with ID '%d' was not found.", id)
		}
		return nil, database.MapError("dataprocessing.update_status", err)
	}
	job := mapJob(row)
	return &job, nil
}

func (r *repository) UpdateProgress(ctx context.Context, id int64, total, processed, success, errorCount int32, errorsJSON []byte) error {
	if _, err := r.queries.UpdateDataProcessingJobProgress(ctx, dbgen.UpdateDataProcessingJobProgressParams{
		ID:             id,
		TotalItems:     total,
		ProcessedItems: processed,
		SuccessCount:   success,
		ErrorCount:     errorCount,
		Errors:         errorsJSON,
	}); err != nil {
		return database.MapError("dataprocessing.update_progress", err)
	}
	return nil
}

func (r *repository) UpdateFile(ctx context.Context, id int64, file FileInput) error {
	if _, err := r.queries.UpdateDataProcessingJobFile(ctx, dbgen.UpdateDataProcessingJobFileParams{
		ID:       id,
		FileName: &file.Name,
		FileDisk: &file.Disk,
		FilePath: &file.Path,
		FileSize: &file.Size,
		FileMime: &file.Mime,
	}); err != nil {
		return database.MapError("dataprocessing.update_file", err)
	}
	return nil
}

func (r *repository) DeleteCompletedBefore(ctx context.Context, retentionDays int) (int64, error) {
	deleted, err := r.queries.DeleteCompletedDataProcessingJobs(ctx, int32(retentionDays))
	if err != nil {
		return 0, database.MapError("dataprocessing.cleanup", err)
	}
	return deleted, nil
}

func (r *repository) CountCompletedBefore(ctx context.Context, retentionDays int) (int64, error) {
	count, err := r.queries.CountCompletedDataProcessingJobsBefore(ctx, int32(retentionDays))
	if err != nil {
		return 0, database.MapError("dataprocessing.count_completed", err)
	}
	return count, nil
}

func (r *repository) ListExpired(ctx context.Context, retentionDays int, limit int32) ([]ExpiredJob, error) {
	rows, err := r.queries.ListExpiredDataProcessingJobs(ctx, dbgen.ListExpiredDataProcessingJobsParams{
		RetentionDays: int32(retentionDays),
		PageLimit:     limit,
	})
	if err != nil {
		return nil, database.MapError("dataprocessing.list_expired", err)
	}
	jobs := make([]ExpiredJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, ExpiredJob{
			ID:       row.ID,
			JobID:    row.JobID,
			Status:   row.Status,
			FileDisk: row.FileDisk,
			FilePath: row.FilePath,
			FileName: row.FileName,
		})
	}
	return jobs, nil
}

func (r *repository) ListExpiredOfType(ctx context.Context, jobType string, maxAgeHours int, limit int32) ([]ExpiredJob, error) {
	rows, err := r.queries.ListExpiredDataProcessingJobsOfType(ctx, dbgen.ListExpiredDataProcessingJobsOfTypeParams{
		Type:        jobType,
		MaxAgeHours: int32(maxAgeHours),
		PageLimit:   limit,
	})
	if err != nil {
		return nil, database.MapError("dataprocessing.list_expired_of_type", err)
	}
	jobs := make([]ExpiredJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, ExpiredJob{
			ID: row.ID, JobID: row.JobID, Status: row.Status,
			FileDisk: row.FileDisk, FilePath: row.FilePath, FileName: row.FileName,
		})
	}
	return jobs, nil
}

func (r *repository) DeleteByIDs(ctx context.Context, ids []int64) (int64, error) {
	deleted, err := r.queries.DeleteDataProcessingJobsByIDs(ctx, ids)
	if err != nil {
		return 0, database.MapError("dataprocessing.delete_by_ids", err)
	}
	return deleted, nil
}

func mapJob(row dbgen.DataProcessingJob) Job {
	return Job{
		ID:             row.ID,
		TenantID:       row.TenantID,
		BranchID:       row.BranchID,
		QueueJobID:     row.JobID,
		Type:           row.Type,
		Status:         row.Status,
		EntityType:     row.EntityType,
		Filters:        row.Filters,
		TotalItems:     row.TotalItems,
		ProcessedItems: row.ProcessedItems,
		SuccessCount:   row.SuccessCount,
		ErrorCount:     row.ErrorCount,
		Errors:         row.Errors,
		ErrorMessage:   row.ErrorMessage,
		FileName:       row.FileName,
		FileDisk:       row.FileDisk,
		FilePath:       row.FilePath,
		FileSize:       row.FileSize,
		FileMime:       row.FileMime,
		UserID:         row.UserID,
		StartedAt:      timePtr(row.StartedAt),
		CompletedAt:    timePtr(row.CompletedAt),
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalInt64(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

// ActorNames returns the names of the people who started jobs.
func (r *repository) ActorNames(ctx context.Context, userIDs []int64) (map[int64]string, error) {
	names := map[int64]string{}
	if len(userIDs) == 0 {
		return names, nil
	}
	rows, err := r.queries.ListDataProcessingJobActors(ctx, userIDs)
	if err != nil {
		return nil, database.MapError("dataprocessing.actor_names", err)
	}
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}
