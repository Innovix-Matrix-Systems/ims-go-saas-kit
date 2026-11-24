package privacy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/webhooks"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
	contracts "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// Service runs erasures and exports. It is callable from HTTP, the CLI, jobs
// and scheduled tasks; callers check permissions, the service checks the
// rules (super admin, guards, already erased).
type Service struct {
	repo     Repository
	tx       *database.TxManager
	registry *contracts.Registry
	checker  *authz.Checker
	auth     authn.Authenticator
	disks    *storage.Registry
	activity *activity.Recorder
	logger   *slog.Logger
	now      func() time.Time

	// Optional collaborators (flows.go).
	settings      *settings.Service
	delivery      *delivery.Service
	processing    *dataprocessing.Service
	processingCfg config.Processing
	queue         queue.Engine
	exportURL     string
}

// NewService creates the privacy service.
func NewService(repo Repository, tx *database.TxManager, registry *contracts.Registry, checker *authz.Checker, auth authn.Authenticator, logger *slog.Logger) *Service {
	webhooks.RegisterEvent(ActionUserErased, "A user's personal data was erased (the payload carries the id only).")
	return &Service{repo: repo, tx: tx, registry: registry, checker: checker, auth: auth, logger: logger, now: time.Now}
}

// WithStorage lets erasure delete the stored objects contributors report.
func (s *Service) WithStorage(disks *storage.Registry) *Service {
	s.disks = disks
	return s
}

// WithActivity records erasures and exports (id-only entries).
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// EraseInput asks for one user's erasure.
type EraseInput struct {
	UserID      int64
	Source      string // SourceSelf, SourceAdmin, SourceCLI, SourceRetention; "" keeps a scheduled request's
	RequestedBy int64  // 0 for the CLI and the scheduler
	// RequestID runs exactly this scheduled request (the due-erasure task):
	// if it is no longer open, nothing runs.
	RequestID int64
}

// Erase erases the user now: guards, every contributor in one transaction
// (the tombstone last), the ledger row, then the stored objects, sessions and
// permission cache. A failed run leaves the ledger row "failed" and the data
// untouched; running it again is safe.
func (s *Service) Erase(ctx context.Context, input EraseInput) (_ *Request, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.erase")
	defer end(&err)
	subject, erasedAt, err := s.repo.Subject(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if erasedAt != nil {
		return nil, apperr.New(apperr.Conflict, "This account has already been erased.")
	}
	if err := s.refuseSuperAdmin(ctx, subject.UserID); err != nil {
		return nil, err
	}
	if err := s.checkGuards(ctx, s.tx.Queries(), subject); err != nil {
		return nil, err
	}

	request, err := s.repo.OpenErasure(ctx, subject.UserID)
	if err != nil {
		return nil, err
	}
	switch {
	case request == nil && input.RequestID != 0:
		// The scheduled request was cancelled (or finished) meanwhile.
		return nil, apperr.New(apperr.InvalidState, "The deletion was cancelled.")
	case request == nil:
		request, err = s.repo.Create(ctx, CreateInput{
			TenantID: subject.TenantID, UserID: subject.UserID, Type: TypeErasure, Status: StatusProcessing,
			Source: input.Source, RequestedBy: input.RequestedBy,
		})
		if err != nil {
			return nil, err
		}
	default:
		if input.RequestID != 0 && request.ID != input.RequestID {
			return nil, apperr.New(apperr.InvalidState, "The deletion was replaced by another request.")
		}
		// Claim the request: a cancel that landed after OpenErasure wins.
		// An admin or the CLI running it now is recorded as the requester.
		claimed, err := s.repo.MarkProcessing(ctx, request.ID, input.Source, input.RequestedBy)
		if err != nil {
			return nil, err
		}
		if !claimed {
			return nil, apperr.New(apperr.InvalidState, "The deletion was cancelled.")
		}
		if input.Source != "" {
			request.Source = input.Source
		}
		if input.RequestedBy != 0 {
			request.RequestedBy = &input.RequestedBy
		}
	}

	report, objects, err := s.erase(ctx, subject, request.ID, true)
	if err != nil {
		failCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if failErr := s.repo.Fail(failCtx, request.ID, failureMessage(err)); failErr != nil {
			s.logger.Error("privacy: erasure failure not recorded", "request_id", request.ID, "error", failErr)
		}
		return nil, err
	}
	s.afterErasure(ctx, subject, objects, input.RequestedBy, true)

	now := s.now().UTC()
	request.Status, request.Report, request.CompletedAt = StatusCompleted, report, &now
	return request, nil
}

// eraseWithoutLedger runs the contributors with no guard and no ledger row
// (the replay of an erasure already decided and recorded).
func (s *Service) eraseWithoutLedger(ctx context.Context, subject contracts.Subject) (Report, []contracts.Object, error) {
	return s.erase(ctx, subject, 0, false)
}

// erase runs the guards and the contributors in one transaction and marks
// the ledger row completed inside it (requestID 0: no ledger row).
func (s *Service) erase(ctx context.Context, subject contracts.Subject, requestID int64, guarded bool) (Report, []contracts.Object, error) {
	report := Report{}
	var objects []contracts.Object
	run := func(q *dbgen.Queries) error {
		// Again inside the transaction: another admin may have been
		// removed since the first check.
		if guarded {
			if err := s.checkGuards(ctx, q, subject); err != nil {
				return err
			}
		}
		for _, contributor := range s.registry.Contributors() {
			part, err := contributor.Erase(ctx, q, subject)
			if err != nil {
				return fmt.Errorf("privacy: %s: %w", contributor.Name(), err)
			}
			objects = append(objects, part.Objects...)
			part.Objects = nil
			report[contributor.Name()] = part
		}
		if requestID == 0 {
			return nil
		}
		return s.repo.WithQueries(q).Complete(ctx, requestID, report)
	}
	var err error
	if subject.TenantID != nil {
		// One erasure per tenant at a time: two running together would each
		// count the other as the remaining Admin and both pass the guard.
		err = s.tx.WithTenantLock(ctx, "privacy.erase", *subject.TenantID, run)
	} else {
		err = s.tx.WithTx(ctx, run)
	}
	if err != nil {
		return nil, nil, err
	}
	return report, objects, nil
}

// afterErasure does what cannot join the transaction. Failures are logged,
// not returned: the personal data in the database is already gone. announce
// records the user.erased entry (also a webhook) and mails the confirmation
// to the address the account had; a replay does neither.
func (s *Service) afterErasure(ctx context.Context, subject contracts.Subject, objects []contracts.Object, requestedBy int64, announce bool) {
	for _, object := range objects {
		if s.disks == nil {
			break
		}
		disk, err := s.disks.Disk(object.Disk)
		if err == nil {
			err = disk.Delete(ctx, object.Path)
		}
		if err != nil {
			s.logger.Warn("privacy: stored file not deleted", "user_id", subject.UserID, "disk", object.Disk, "path", object.Path, "error", err)
		}
	}
	// Sessions are already deleted with the tokens; RevokeAll also drops
	// whatever the authenticator keeps outside them.
	if err := s.auth.RevokeAll(ctx, subject.UserID); err != nil {
		s.logger.Warn("privacy: sessions not revoked", "user_id", subject.UserID, "error", err)
	}
	s.checker.InvalidateUser(ctx, subject.UserID)
	if !announce {
		return
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantValue(subject.TenantID), ActorID: requestedBy,
		Action: ActionUserErased, Subject: "user", SubjectID: subject.UserID,
	})
	s.mail(ctx, subject, TemplateAccountErased, map[string]any{})
}

// ExportInput asks for one user's export.
type ExportInput struct {
	UserID      int64
	Source      string
	RequestedBy int64
}

// Export writes the user's personal data to out as a zip: one folder per
// contributor and a manifest.json. It never contains secrets.
func (s *Service) Export(ctx context.Context, input ExportInput, out io.Writer) (_ *Manifest, err error) {
	ctx, end := tracing.Begin(ctx, "privacy.export")
	defer end(&err)
	subject, erasedAt, err := s.repo.Subject(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if erasedAt != nil {
		return nil, apperr.New(apperr.Conflict, "This account has been erased; there is nothing left to export.")
	}
	request, err := s.repo.Create(ctx, CreateInput{
		TenantID: subject.TenantID, UserID: subject.UserID, Type: TypeExport, Status: StatusProcessing,
		Source: input.Source, RequestedBy: input.RequestedBy,
	})
	if err != nil {
		return nil, err
	}

	manifest, err := s.writeExport(ctx, subject, out)
	if err != nil {
		failCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if failErr := s.repo.Fail(failCtx, request.ID, failureMessage(err)); failErr != nil {
			s.logger.Error("privacy: export failure not recorded", "request_id", request.ID, "error", failErr)
		}
		return nil, err
	}
	if err := s.repo.Complete(ctx, request.ID, Report{}); err != nil {
		return nil, err
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: tenantValue(subject.TenantID), ActorID: input.RequestedBy,
		Action: ActionUserExported, Subject: "user", SubjectID: subject.UserID,
	})
	return manifest, nil
}

func (s *Service) writeExport(ctx context.Context, subject contracts.Subject, out io.Writer) (*Manifest, error) {
	archive := newZipExport(out)
	manifest := &Manifest{SchemaVersion: manifestSchemaVersion, GeneratedAt: s.now().UTC().Truncate(time.Second), UserID: subject.UserID}
	queries := s.tx.Queries()
	for _, contributor := range s.registry.Contributors() {
		manifest.Contributors = append(manifest.Contributors, contributor.Name())
		if err := contributor.Export(ctx, queries, subject, archive.folder(contributor.Name())); err != nil {
			return nil, fmt.Errorf("privacy: export %s: %w", contributor.Name(), err)
		}
	}
	manifest.Files = archive.files
	if err := archive.root().JSON("manifest", manifest); err != nil {
		return nil, err
	}
	if err := archive.close(); err != nil {
		return nil, err
	}
	return manifest, nil
}

// Coverage matches the schema's user-reference columns against the
// contributors and exemptions. An entry with Covered() false is a column no
// erasure handles.
func (s *Service) Coverage(ctx context.Context) ([]contracts.Coverage, error) {
	columns, err := s.repo.UserReferenceColumns(ctx)
	if err != nil {
		return nil, err
	}
	return s.registry.Cover(columns), nil
}

func (s *Service) refuseSuperAdmin(ctx context.Context, userID int64) error {
	super, err := s.checker.IsSuperAdmin(ctx, userID)
	if err != nil {
		return err
	}
	if super {
		return apperr.New(apperr.Forbidden, "The super admin user cannot be erased.")
	}
	return nil
}

func (s *Service) checkGuards(ctx context.Context, q *dbgen.Queries, subject contracts.Subject) error {
	var reasons []string
	for _, guard := range s.registry.Guards() {
		found, err := guard.CheckErasure(ctx, q, subject)
		if err != nil {
			return err
		}
		reasons = append(reasons, found...)
	}
	if len(reasons) == 0 {
		return nil
	}
	refused := apperr.New(apperr.Conflict, strings.Join(reasons, " "))
	refused.Meta = map[string]any{"reasons": reasons}
	return refused
}

func tenantValue(tenantID *int64) int64 {
	if tenantID == nil {
		return 0
	}
	return *tenantID
}

// failureMessage is what the ledger keeps about a failed run: the
// application error's message, or a generic line (a driver error could echo
// a value, and the ledger never holds personal data).
func failureMessage(err error) string {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return "The request failed; see the application logs."
}
