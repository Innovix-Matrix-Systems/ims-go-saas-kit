package dataprocessing

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// Rerun queues a job again after Retry has put it back to pending. Each
// module that starts jobs registers one per job kind (see RegisterRerun).
type Rerun func(ctx context.Context, job *Job) error

var (
	rerunMu sync.RWMutex
	reruns  = map[string]Rerun{}
)

// RerunKey names a job kind: its type and entity ("export:user").
func RerunKey(jobType, entityType string) string { return jobType + ":" + entityType }

// RegisterRerun lets failed jobs of one kind be retried.
func RegisterRerun(key string, rerun Rerun) {
	rerunMu.Lock()
	defer rerunMu.Unlock()
	reruns[key] = rerun
}

func rerunFor(job *Job) (Rerun, bool) {
	entity := ""
	if job.EntityType != nil {
		entity = *job.EntityType
	}
	rerunMu.RLock()
	defer rerunMu.RUnlock()
	rerun, ok := reruns[RerunKey(job.Type, entity)]
	return rerun, ok
}

// Retry runs a failed job again with the same input (filters, uploaded
// file): it is reset to pending and queued anew.
func (s *Service) Retry(ctx context.Context, job *Job) (_ *Job, err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.retry", attribute.Int64("job.id", job.ID))
	defer end(&err)
	if job.Status != StatusFailed {
		return nil, apperr.New(apperr.InvalidState, "Only a failed job can be retried.")
	}
	rerun, ok := rerunFor(job)
	if !ok {
		return nil, apperr.New(apperr.InvalidState, "This kind of job cannot be retried; start it again instead.")
	}
	fresh, err := s.repo.Reset(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.RemoveOutputFile(ctx, job.ID); err != nil {
		return nil, err
	}
	if err := rerun(ctx, fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

// Remove deletes a finished job and its file. A job that is still waiting or
// running is refused: its worker would write into nothing.
func (s *Service) Remove(ctx context.Context, job *Job) (err error) {
	ctx, end := tracing.Begin(ctx, "dataprocessing.remove", attribute.Int64("job.id", job.ID))
	defer end(&err)
	if job.Status == StatusPending || job.Status == StatusProcessing {
		return apperr.New(apperr.InvalidState, "A job that is still running cannot be removed.")
	}
	s.removeJobFile(ctx, ExpiredJob{ID: job.ID, FileDisk: job.FileDisk, FilePath: job.FilePath})
	_, err = s.repo.DeleteByIDs(ctx, []int64{job.ID})
	return err
}

// ExpiresAt is when a finished job and its file are deleted: personal-data
// exports after privacy.export_ttl_hours, everything else after the job's
// organization's data_processing_job.cleanup_days. Nil while it runs.
func ExpiresAt(ctx context.Context, settingsService *settings.Service, job Job) *time.Time {
	if job.CompletedAt == nil || settingsService == nil {
		return nil
	}
	scope := tenancy.Scope{Internal: true}
	if job.TenantID != nil {
		scope = tenancy.Scope{TenantID: *job.TenantID}
	}
	var at time.Time
	if job.Type == TypePrivacyExport {
		hours, err := settingsService.GetInt(ctx, scope, "privacy.export_ttl_hours")
		if err != nil || hours <= 0 {
			hours = 72
		}
		at = job.CompletedAt.Add(time.Duration(hours) * time.Hour)
	} else {
		at = job.CompletedAt.AddDate(0, 0, CleanupSetting(ctx, settingsService, scope))
	}
	return &at
}
