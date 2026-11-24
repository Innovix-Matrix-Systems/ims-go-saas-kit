package privacy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
	contracts "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Setting keys (modules/settings/registry.go, group "privacy").
const (
	SettingGraceDays           = "privacy.deletion_grace_days"
	SettingExportTTLHours      = "privacy.export_ttl_hours"
	SettingEraseSoftDeletedAge = "privacy.erase_soft_deleted_after_days"
)

// Defaults used when a setting cannot be read.
const (
	defaultGraceDays      = 14
	defaultExportTTLHours = 72
)

// More activity actions, id-only like the others.
const (
	ActionDeletionRequested = "user.deletion_requested"
	ActionDeletionCancelled = "user.deletion_cancelled"
)

// KindPrivacyExport is the queue job that builds an export archive.
const KindPrivacyExport = "privacy.export"

// Delivery templates (internal/platform/delivery/templates).
const (
	TemplateDeletionScheduled = "privacy-deletion-scheduled"
	TemplateAccountErased     = "privacy-account-erased"
	TemplateExportReady       = "privacy-export-ready"
)

// batchSize bounds the work one scheduled run does.
const batchSize = 100

// staleProcessingAfter is when an erasure still "processing" is taken to
// belong to a process that died (longer than the erase-due task's timeout).
const staleProcessingAfter = time.Hour

// WithSettings reads the grace period, export lifetime and retention.
func (s *Service) WithSettings(service *settings.Service) *Service {
	s.settings = service
	return s
}

// WithDelivery mails the scheduled-deletion notice, the erasure
// confirmation and the export link.
func (s *Service) WithDelivery(service *delivery.Service) *Service {
	s.delivery = service
	return s
}

// WithExports stores export archives as processing jobs (downloaded through
// /api/v1/data-processing/job/{jobId}/download) built by a queue job.
// apiURL is the API's public base URL, for the link in the mail.
func (s *Service) WithExports(processing *dataprocessing.Service, cfg config.Processing, engine queue.Engine, apiURL string) *Service {
	s.processing, s.processingCfg, s.queue = processing, cfg, engine
	s.apiURL = strings.TrimRight(apiURL, "/")
	return s
}

// RegisterJobs adds the export job definition to the queue registry.
func (s *Service) RegisterJobs(registry *queue.Registry) error {
	return registry.Register(queue.Definition{
		Kind:        KindPrivacyExport,
		Description: "Builds a user's personal-data export archive and mails the download link.",
		Queue:       queue.QueueHeavy,
		MaxRetries:  1,
		Timeout:     30 * time.Minute,
		// The payload names its own user; tenantless accounts export too.
		SystemJob: true,
		Validate: func(payload []byte) error {
			var input exportPayload
			if err := json.Unmarshal(payload, &input); err != nil || input.RequestID < 1 {
				return apperr.New(apperr.InvalidInput, "privacy.export payload requires request_id")
			}
			return nil
		},
		Handle: func(ctx context.Context, payload []byte) error {
			var input exportPayload
			if err := json.Unmarshal(payload, &input); err != nil {
				return err
			}
			return s.ProcessExport(ctx, input.RequestID)
		},
	})
}

type exportPayload struct {
	RequestID int64 `json:"request_id"`
}

// --- Deletion with a grace period -----------------------------------------

// RequestDeletion schedules the caller's own erasure after the grace period
// (privacy.deletion_grace_days). The guards run now, so a refusal is
// immediate. Every session and MCP connection is signed out; the account can
// still sign in to cancel. Asking twice returns the scheduled request.
func (s *Service) RequestDeletion(ctx context.Context, userID int64) (_ *Request, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.request_deletion", attribute.Int64("user.id", userID))
	defer end(&err)
	subject, err := s.erasable(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.schedule(ctx, subject, SourceSelf, userID)
}

// ScheduleErasure is the admin twin of RequestDeletion: the same grace period
// and mail, asked by someone else. immediate skips the grace period. scope
// must reach the user's tenant (internal scopes reach every tenant).
func (s *Service) ScheduleErasure(ctx context.Context, scope tenancy.Scope, actorID, userID int64, immediate bool) (_ *Request, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.schedule_erasure",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("user.id", userID),
		attribute.Bool("privacy.immediate", immediate),
	)
	defer end(&err)
	if actorID == userID {
		return nil, apperr.New(apperr.Forbidden, "Use your own account's deletion request to delete yourself.")
	}
	// The tenant boundary first: a user outside the caller's reach must look
	// like a missing one, not answer "already erased" or "last Admin".
	if err := s.mustReach(ctx, scope, userID); err != nil {
		return nil, err
	}
	subject, err := s.erasable(ctx, userID)
	if err != nil {
		return nil, err
	}
	if immediate {
		return s.Erase(ctx, EraseInput{UserID: userID, Source: SourceAdmin, RequestedBy: actorID})
	}
	return s.schedule(ctx, subject, SourceAdmin, actorID)
}

func (s *Service) schedule(ctx context.Context, subject contracts.Subject, source string, requestedBy int64) (*Request, error) {
	if open, err := s.repo.OpenErasure(ctx, subject.UserID); err != nil || open != nil {
		return open, err
	}
	when := s.now().Add(time.Duration(s.graceDays(ctx, subject)) * 24 * time.Hour).UTC().Truncate(time.Second)
	request, err := s.repo.Create(ctx, CreateInput{
		TenantID: subject.TenantID, UserID: subject.UserID, Type: TypeErasure, Status: StatusScheduled,
		Source: source, RequestedBy: requestedBy, ScheduledFor: &when,
	})
	if err != nil {
		return nil, err
	}
	if err := s.auth.RevokeAll(ctx, subject.UserID); err != nil {
		s.logger.Warn("privacy: sessions not revoked after a deletion request", "user_id", subject.UserID, "error", err)
	}
	s.mail(ctx, subject, TemplateDeletionScheduled, map[string]any{"date": when.Format("2 Jan 2006, 15:04 UTC")})
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantValue(subject.TenantID), ActorID: requestedBy,
		Action: ActionDeletionRequested, Subject: "user", SubjectID: subject.UserID,
		Changes: map[string]any{"scheduled_for": when.Format(time.RFC3339), "source": source},
	})
	return request, nil
}

// CancelDeletion cancels the caller's own scheduled erasure. One an admin
// scheduled is the admin's to cancel (CancelErasure).
func (s *Service) CancelDeletion(ctx context.Context, userID int64) (_ *Request, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.cancel_deletion", attribute.Int64("user.id", userID))
	defer end(&err)
	open, err := s.repo.OpenErasure(ctx, userID)
	if err != nil {
		return nil, err
	}
	if open == nil {
		return nil, apperr.New(apperr.ResourceNotFound, "There is no scheduled deletion for this account.")
	}
	if open.Source != SourceSelf {
		return nil, apperr.New(apperr.Forbidden, "An administrator scheduled this deletion; ask them to cancel it.")
	}
	return s.cancel(ctx, open, userID)
}

// CancelErasure is the admin twin of CancelDeletion: it cancels any
// scheduled erasure of a user the scope reaches.
func (s *Service) CancelErasure(ctx context.Context, scope tenancy.Scope, actorID, userID int64) (_ *Request, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.cancel_erasure",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("user.id", userID),
	)
	defer end(&err)
	if err := s.mustReach(ctx, scope, userID); err != nil {
		return nil, err
	}
	open, err := s.repo.OpenErasure(ctx, userID)
	if err != nil {
		return nil, err
	}
	if open == nil {
		return nil, apperr.New(apperr.ResourceNotFound, "There is no scheduled deletion for this user.")
	}
	return s.cancel(ctx, open, actorID)
}

func (s *Service) cancel(ctx context.Context, open *Request, actorID int64) (*Request, error) {
	userID := open.UserID
	cancelled, err := s.repo.Cancel(ctx, open.ID)
	if err != nil {
		return nil, err
	}
	if !cancelled {
		return nil, apperr.New(apperr.InvalidState, "The deletion is already running and can no longer be cancelled.")
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantValue(open.TenantID), ActorID: actorID,
		Action: ActionDeletionCancelled, Subject: "user", SubjectID: userID,
	})
	open.Status = StatusCancelled
	return open, nil
}

// Deletion returns the caller's scheduled (or running) erasure.
func (s *Service) Deletion(ctx context.Context, userID int64) (*Request, error) {
	open, err := s.repo.OpenErasure(ctx, userID)
	if err != nil {
		return nil, err
	}
	if open == nil {
		return nil, apperr.New(apperr.ResourceNotFound, "There is no scheduled deletion for this account.")
	}
	return open, nil
}

// EraseDue runs the erasures whose grace period is over (the
// privacy.erase-due task), and picks up again an erasure left "processing"
// by a process that died. A refused or failed erasure is marked failed and
// not retried: an operator reruns it with `ims privacy:erase`. One cancelled
// at the last moment is skipped.
func (s *Service) EraseDue(ctx context.Context) (erased int, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.erase_due")
	defer end(&err)
	due, err := s.repo.DueErasures(ctx, staleProcessingAfter, batchSize)
	if err != nil {
		return 0, err
	}
	for _, request := range due {
		_, eraseErr := s.Erase(ctx, EraseInput{UserID: request.UserID, RequestID: request.RequestID})
		if apperr.Is(eraseErr, apperr.InvalidState) {
			continue // cancelled or replaced meanwhile
		}
		if eraseErr != nil {
			s.logger.Warn("privacy: scheduled erasure failed", "request_id", request.RequestID, "user_id", request.UserID, "error", eraseErr)
			if failErr := s.repo.Fail(ctx, request.RequestID, failureMessage(eraseErr)); failErr != nil {
				return erased, failErr
			}
			continue
		}
		erased++
	}
	return erased, nil
}

// EraseSoftDeleted erases users deleted longer than
// privacy.erase_soft_deleted_after_days ago (0, the default, turns it off).
func (s *Service) EraseSoftDeleted(ctx context.Context) (erased int, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.erase_soft_deleted")
	defer end(&err)
	days := s.intSetting(ctx, tenancy.Scope{}, SettingEraseSoftDeletedAge, 0)
	if days < 1 {
		return 0, nil
	}
	ids, err := s.repo.SoftDeletedToErase(ctx, days, batchSize)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, eraseErr := s.Erase(ctx, EraseInput{UserID: id, Source: SourceRetention}); eraseErr != nil {
			s.logger.Warn("privacy: retention erasure refused", "user_id", id, "error", eraseErr)
			// A failed retention row keeps the user out of later runs, so
			// one that can never be erased does not hold up the rest.
			if err := s.recordRetentionRefusal(ctx, id, eraseErr); err != nil {
				return erased, err
			}
			continue
		}
		erased++
	}
	return erased, nil
}

func (s *Service) recordRetentionRefusal(ctx context.Context, userID int64, cause error) error {
	subject, _, err := s.repo.Subject(ctx, userID)
	if err != nil {
		return err
	}
	if open, err := s.repo.OpenErasure(ctx, userID); err != nil || open != nil {
		return err // Erase already failed (or still holds) its own request
	}
	request, err := s.repo.Create(ctx, CreateInput{
		TenantID: subject.TenantID, UserID: userID, Type: TypeErasure, Status: StatusProcessing, Source: SourceRetention,
	})
	if err != nil {
		return err
	}
	return s.repo.Fail(ctx, request.ID, failureMessage(cause))
}

// --- Exports through the queue --------------------------------------------

// StartExport queues an export archive for the user. The job belongs to
// whoever asked (the user, or the admin) and only they can download it.
// Self-service exports are limited to one a day.
func (s *Service) StartExport(ctx context.Context, scope tenancy.Scope, input ExportInput) (_ *dataprocessing.Job, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.start_export", attribute.Int64("tenant.id", scope.EffectiveTenantID()))
	defer end(&err)
	if s.processing == nil || s.queue == nil {
		return nil, apperr.New(apperr.ServiceUnavailable, "Personal-data exports are not available.")
	}
	if err := s.mustReach(ctx, scope, input.UserID); err != nil {
		return nil, err
	}
	subject, erasedAt, err := s.repo.Subject(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if erasedAt != nil {
		return nil, apperr.New(apperr.Conflict, "This account has been erased; there is nothing left to export.")
	}
	owner := input.RequestedBy
	if owner == 0 {
		owner = subject.UserID
	}
	if input.Source == SourceSelf {
		recent, err := s.repo.RecentExports(ctx, subject.UserID, s.now().Add(-24*time.Hour))
		if err != nil {
			return nil, err
		}
		if recent > 0 {
			limited := apperr.New(apperr.RateLimitExceeded, "You can export your data once a day.")
			limited.RetryAfter = 24 * 60 * 60
			return nil, limited
		}
	}
	request, err := s.repo.Create(ctx, CreateInput{
		TenantID: subject.TenantID, UserID: subject.UserID, Type: TypeExport, Status: StatusScheduled,
		Source: input.Source, RequestedBy: owner,
	})
	if err != nil {
		return nil, err
	}
	// From here on a failure fails the ledger row, or it would count as
	// today's export and block the next attempt.
	failRequest := func(err error) error {
		_ = s.repo.Fail(context.WithoutCancel(ctx), request.ID, failureMessage(err))
		return err
	}
	filters, _ := json.Marshal(map[string]int64{"user_id": subject.UserID, "request_id": request.ID})
	entity := "user"
	job, err := s.processing.Create(ctx, subjectScope(subject), dataprocessing.TypePrivacyExport, &entity, 0, filters, owner)
	if err != nil {
		return nil, failRequest(err)
	}
	if err := s.repo.SetJob(ctx, request.ID, job.ID); err != nil {
		_ = s.processing.MarkFailed(context.WithoutCancel(ctx), job.ID, "The export could not be recorded.")
		return nil, failRequest(err)
	}
	if err := s.queue.Enqueue(ctx, KindPrivacyExport, exportPayload{RequestID: request.ID}, queue.EnqueueOptions{}); err != nil {
		_ = s.processing.MarkFailed(ctx, job.ID, "The export job could not be queued.")
		_ = s.repo.Fail(ctx, request.ID, "The export job could not be queued.")
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "The export job could not be queued.", err)
	}
	return job, nil
}

// ProcessExport builds the archive for a queued export, stores it on the
// processing disk, attaches it to the job and mails the link to whoever
// asked.
func (s *Service) ProcessExport(ctx context.Context, requestID int64) (err error) {
	ctx, end := tracing.Begin(ctx, "privacy.process_export", attribute.Int64("privacy.request_id", requestID))
	defer end(&err)
	request, err := s.repo.Get(ctx, requestID)
	if err != nil {
		return err
	}
	if request.Status == StatusCompleted {
		return nil
	}
	if request.ProcessingJobID == nil {
		return s.repo.Fail(ctx, request.ID, "The export has no processing job.")
	}
	job, err := s.processing.Get(ctx, tenancy.Scope{Internal: true}, *request.ProcessingJobID)
	if err != nil {
		return err
	}
	if err := s.processing.MarkProcessing(ctx, job.ID); err != nil {
		return err
	}
	if claimed, err := s.repo.MarkProcessing(ctx, request.ID, "", 0); err != nil || !claimed {
		return err // failed already: nothing to do
	}
	fail := func(message string, cause error) error {
		s.logger.Warn("privacy: export failed", "request_id", request.ID, "error", cause)
		_ = s.processing.MarkFailed(ctx, job.ID, message)
		_ = s.repo.Fail(ctx, request.ID, message)
		return nil // recorded; retrying would not help
	}

	subject, erasedAt, err := s.repo.Subject(ctx, request.UserID)
	if err != nil || erasedAt != nil {
		return fail("The account no longer exists.", err)
	}
	file, err := os.CreateTemp("", "privacy-export-*.zip")
	if err != nil {
		return fail("The export file could not be created.", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if _, err := s.writeExport(ctx, subject, file); err != nil {
		return fail("The export could not be built.", err)
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return fail("The export file could not be read.", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fail("The export file could not be read.", err)
	}
	disk, err := s.disks.Disk(s.processingCfg.Disk)
	if err != nil {
		return fail("The processing storage is not available.", err)
	}
	name := fmt.Sprintf("personal_data_%d_%s.zip", subject.UserID, *job.QueueJobID)
	path := processingPath(s.processingCfg, name)
	if err := disk.Put(ctx, path, file, size); err != nil {
		return fail("The export file could not be stored.", err)
	}
	if err := s.processing.AttachFile(ctx, job.ID, dataprocessing.FileInput{
		Name: name, Disk: s.processingCfg.Disk, Path: path, Size: size, Mime: "application/zip",
	}); err != nil {
		return err
	}
	if err := s.processing.MarkCompleted(ctx, job.ID); err != nil {
		return err
	}
	if err := s.repo.Complete(ctx, request.ID, Report{}); err != nil {
		return err
	}
	owner := subject
	if request.RequestedBy != nil && *request.RequestedBy != subject.UserID {
		if requester, _, err := s.repo.Subject(ctx, *request.RequestedBy); err == nil {
			owner = requester
		}
	}
	s.mail(ctx, owner, TemplateExportReady, map[string]any{
		"url":   s.apiURL + "/api/v1/data-processing/job/" + *job.QueueJobID + "/download",
		"hours": s.intSetting(ctx, tenancy.Scope{}, SettingExportTTLHours, defaultExportTTLHours),
	})
	actor := int64(0)
	if request.RequestedBy != nil {
		actor = *request.RequestedBy
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantValue(subject.TenantID), ActorID: actor,
		Action: ActionUserExported, Subject: "user", SubjectID: subject.UserID,
	})
	return nil
}

// ExpireExports deletes export archives older than privacy.export_ttl_hours
// (the privacy.expire-exports task).
func (s *Service) ExpireExports(ctx context.Context) (_ int64, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.expire_exports")
	defer end(&err)
	if s.processing == nil {
		return 0, nil
	}
	hours := s.intSetting(ctx, tenancy.Scope{}, SettingExportTTLHours, defaultExportTTLHours)
	return s.processing.CleanupType(ctx, dataprocessing.TypePrivacyExport, hours)
}

// --- Replay after a backup restore ----------------------------------------

// ErasedSince lists the users erased since the given time, from the ledger.
// Save it before restoring a backup: the restore brings back the old ledger.
func (s *Service) ErasedSince(ctx context.Context, since time.Time) ([]int64, error) {
	return s.repo.CompletedErasuresSince(ctx, since)
}

// Replay erases again users a backup restore brought back. Users that are
// still erased or no longer exist are skipped; guards do not apply (the
// erasure was already decided). It returns how many were erased again.
func (s *Service) Replay(ctx context.Context, userIDs []int64) (_ int, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.replay", attribute.Int("users", len(userIDs)))
	defer end(&err)
	replayed := 0
	for _, id := range userIDs {
		subject, erasedAt, err := s.repo.Subject(ctx, id)
		if apperr.Is(err, apperr.ResourceNotFound) || erasedAt != nil {
			continue
		}
		if err != nil {
			return replayed, err
		}
		_, objects, err := s.eraseWithoutLedger(ctx, subject)
		if err != nil {
			return replayed, fmt.Errorf("replay user %d: %w", id, err)
		}
		s.afterErasure(ctx, subject, objects, 0, false)
		replayed++
	}
	return replayed, nil
}

// --- helpers ----------------------------------------------------------------

// erasable loads a user that may be erased: exists, not yet erased, not the
// super admin, and no guard refuses.
func (s *Service) erasable(ctx context.Context, userID int64) (contracts.Subject, error) {
	subject, erasedAt, err := s.repo.Subject(ctx, userID)
	if err != nil {
		return contracts.Subject{}, err
	}
	if erasedAt != nil {
		return contracts.Subject{}, apperr.New(apperr.Conflict, "This account has already been erased.")
	}
	if err := s.refuseSuperAdmin(ctx, userID); err != nil {
		return contracts.Subject{}, err
	}
	if err := s.checkGuards(ctx, s.tx.Queries(), subject); err != nil {
		return contracts.Subject{}, err
	}
	return subject, nil
}

// mail sends one of the privacy templates to the subject's address. The
// recipient carries no user id, so the delivery log does not link the
// message back to an erased account.
func (s *Service) mail(ctx context.Context, subject contracts.Subject, template string, data map[string]any) {
	if s.delivery == nil || subject.Email == "" {
		return
	}
	err := s.delivery.Send(ctx, delivery.Message{
		To:       delivery.Recipient{TenantID: tenantValue(subject.TenantID), Name: subject.Name, Email: subject.Email},
		Template: template,
		Data:     data,
	})
	if err != nil {
		s.logger.Warn("privacy: mail not sent", "template", template, "user_id", subject.UserID, "error", err)
	}
}

func (s *Service) graceDays(ctx context.Context, subject contracts.Subject) int {
	scope := tenancy.Scope{}
	if subject.TenantID != nil {
		scope = tenancy.Scope{TenantID: *subject.TenantID}
	}
	days := s.intSetting(ctx, scope, SettingGraceDays, defaultGraceDays)
	if days < 0 {
		return defaultGraceDays
	}
	return days
}

func (s *Service) intSetting(ctx context.Context, scope tenancy.Scope, name string, fallback int) int {
	if s.settings == nil {
		return fallback
	}
	value, err := s.settings.GetInt(ctx, scope, name)
	if err != nil {
		return fallback
	}
	return value
}

// mustReach answers 404 for a user the scope does not reach, before anything
// else about them is checked.
func (s *Service) mustReach(ctx context.Context, scope tenancy.Scope, userID int64) error {
	subject, _, err := s.repo.Subject(ctx, userID)
	if err != nil {
		return err
	}
	if !reaches(scope, subject) {
		return apperr.Newf(apperr.ResourceNotFound, "User %d not found.", userID)
	}
	return nil
}

// reaches reports whether scope may act on the subject: internal scopes
// reach everyone, a tenant scope only its own users.
func reaches(scope tenancy.Scope, subject contracts.Subject) bool {
	if scope.Internal {
		return true
	}
	tenantID := scope.EffectiveTenantID()
	return tenantID > 0 && subject.TenantID != nil && *subject.TenantID == tenantID
}

func subjectScope(subject contracts.Subject) tenancy.Scope {
	if subject.TenantID == nil {
		return tenancy.Scope{Internal: true}
	}
	return tenancy.Scope{TenantID: *subject.TenantID}
}

func processingPath(cfg config.Processing, name string) string {
	base := strings.Trim(cfg.BasePath, "/")
	if base == "" {
		base = "jobs"
	}
	return base + "/privacy/" + name
}
