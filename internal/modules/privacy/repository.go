package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	contracts "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/privacy"
)

// Repository is the privacy_requests ledger plus the reads erasure needs.
type Repository interface {
	Subject(ctx context.Context, userID int64) (contracts.Subject, *time.Time, error)
	Create(ctx context.Context, input CreateInput) (*Request, error)
	OpenErasure(ctx context.Context, userID int64) (*Request, error)
	// MarkProcessing claims an open request; false means it was cancelled
	// or finished meanwhile. A non-empty source / non-zero requestedBy
	// records who made it run now.
	MarkProcessing(ctx context.Context, id int64, source string, requestedBy int64) (bool, error)
	Complete(ctx context.Context, id int64, report Report) error
	Fail(ctx context.Context, id int64, message string) error
	UserReferenceColumns(ctx context.Context) ([]string, error)
	Get(ctx context.Context, id int64) (*Request, error)
	Cancel(ctx context.Context, id int64) (bool, error)
	SetJob(ctx context.Context, id, jobID int64) error
	DueErasures(ctx context.Context, staleAfter time.Duration, limit int32) ([]DueErasure, error)
	RecentExports(ctx context.Context, userID int64, since time.Time) (int64, error)
	CompletedErasuresSince(ctx context.Context, since time.Time) ([]int64, error)
	SoftDeletedToErase(ctx context.Context, days int, limit int32) ([]int64, error)
	// WithQueries binds the repository to a transaction's queries.
	WithQueries(q *dbgen.Queries) Repository
}

// CreateInput is a new ledger row.
type CreateInput struct {
	TenantID     *int64
	UserID       int64
	Type         string
	Status       string
	Source       string
	RequestedBy  int64
	ScheduledFor *time.Time
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

// WithQueries implements Repository.
func (r *repository) WithQueries(q *dbgen.Queries) Repository {
	return &repository{queries: q}
}

// Subject reads the person behind userID, soft-deleted or erased included,
// and when they were erased (nil if never).
func (r *repository) Subject(ctx context.Context, userID int64) (contracts.Subject, *time.Time, error) {
	user, err := r.queries.GetUserForPrivacy(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Subject{}, nil, apperr.Newf(apperr.ResourceNotFound, "User %d not found.", userID)
	}
	if err != nil {
		return contracts.Subject{}, nil, database.MapError("privacy.subject", err)
	}
	subject := contracts.Subject{UserID: user.ID, TenantID: user.TenantID, Name: user.Name, Email: user.Email}
	if user.Phone != nil {
		subject.Phone = *user.Phone
	}
	return subject, contracts.Time(user.ErasedAt), nil
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*Request, error) {
	params := dbgen.CreatePrivacyRequestParams{
		TenantID: input.TenantID, UserID: input.UserID, Type: input.Type, Status: input.Status, Source: input.Source,
	}
	if input.RequestedBy != 0 {
		params.RequestedBy = &input.RequestedBy
	}
	if input.ScheduledFor != nil {
		params.ScheduledFor = pgtype.Timestamptz{Time: *input.ScheduledFor, Valid: true}
	}
	row, err := r.queries.CreatePrivacyRequest(ctx, params)
	if err != nil {
		return nil, database.MapError("privacy.create_request", err)
	}
	request := mapRequest(row)
	return &request, nil
}

func (r *repository) OpenErasure(ctx context.Context, userID int64) (*Request, error) {
	row, err := r.queries.GetOpenErasureForUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.MapError("privacy.open_erasure", err)
	}
	request := mapRequest(row)
	return &request, nil
}

func (r *repository) MarkProcessing(ctx context.Context, id int64, source string, requestedBy int64) (bool, error) {
	params := dbgen.MarkPrivacyRequestProcessingParams{ID: id}
	if source != "" {
		params.Source = &source
	}
	if requestedBy != 0 {
		params.RequestedBy = &requestedBy
	}
	affected, err := r.queries.MarkPrivacyRequestProcessing(ctx, params)
	if err != nil {
		return false, database.MapError("privacy.mark_processing", err)
	}
	return affected == 1, nil
}

func (r *repository) Complete(ctx context.Context, id int64, report Report) error {
	encoded, err := json.Marshal(report)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "The erasure report could not be encoded.", err)
	}
	if _, err := r.queries.CompletePrivacyRequest(ctx, dbgen.CompletePrivacyRequestParams{ID: id, Report: encoded}); err != nil {
		return database.MapError("privacy.complete_request", err)
	}
	return nil
}

func (r *repository) Fail(ctx context.Context, id int64, message string) error {
	if _, err := r.queries.FailPrivacyRequest(ctx, dbgen.FailPrivacyRequestParams{ID: id, Error: &message}); err != nil {
		return database.MapError("privacy.fail_request", err)
	}
	return nil
}

// DueErasure is a scheduled erasure whose grace period is over.
type DueErasure struct {
	RequestID int64
	UserID    int64
}

func (r *repository) Get(ctx context.Context, id int64) (*Request, error) {
	row, err := r.queries.GetPrivacyRequest(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.New(apperr.ResourceNotFound, "Privacy request not found.")
	}
	if err != nil {
		return nil, database.MapError("privacy.get_request", err)
	}
	request := mapRequest(row)
	return &request, nil
}

func (r *repository) Cancel(ctx context.Context, id int64) (bool, error) {
	affected, err := r.queries.CancelPrivacyRequest(ctx, id)
	if err != nil {
		return false, database.MapError("privacy.cancel_request", err)
	}
	return affected == 1, nil
}

func (r *repository) SetJob(ctx context.Context, id, jobID int64) error {
	if _, err := r.queries.SetPrivacyRequestJob(ctx, dbgen.SetPrivacyRequestJobParams{ID: id, ProcessingJobID: &jobID}); err != nil {
		return database.MapError("privacy.set_job", err)
	}
	return nil
}

func (r *repository) DueErasures(ctx context.Context, staleAfter time.Duration, limit int32) ([]DueErasure, error) {
	rows, err := r.queries.ListDueErasures(ctx, dbgen.ListDueErasuresParams{StaleMinutes: int32(staleAfter.Minutes()), PageLimit: limit})
	if err != nil {
		return nil, database.MapError("privacy.due_erasures", err)
	}
	due := make([]DueErasure, 0, len(rows))
	for _, row := range rows {
		due = append(due, DueErasure{RequestID: row.ID, UserID: row.UserID})
	}
	return due, nil
}

func (r *repository) RecentExports(ctx context.Context, userID int64, since time.Time) (int64, error) {
	count, err := r.queries.CountRecentExports(ctx, dbgen.CountRecentExportsParams{UserID: userID, Since: pgtype.Timestamptz{Time: since, Valid: true}})
	if err != nil {
		return 0, database.MapError("privacy.recent_exports", err)
	}
	return count, nil
}

func (r *repository) CompletedErasuresSince(ctx context.Context, since time.Time) ([]int64, error) {
	ids, err := r.queries.ListCompletedErasuresSince(ctx, pgtype.Timestamptz{Time: since, Valid: true})
	if err != nil {
		return nil, database.MapError("privacy.completed_erasures", err)
	}
	return ids, nil
}

func (r *repository) SoftDeletedToErase(ctx context.Context, days int, limit int32) ([]int64, error) {
	ids, err := r.queries.ListSoftDeletedUsersToErase(ctx, dbgen.ListSoftDeletedUsersToEraseParams{Days: int32(days), PageLimit: limit})
	if err != nil {
		return nil, database.MapError("privacy.soft_deleted_to_erase", err)
	}
	return ids, nil
}

func (r *repository) UserReferenceColumns(ctx context.Context) ([]string, error) {
	columns, err := r.queries.ListUserReferenceColumns(ctx)
	if err != nil {
		return nil, database.MapError("privacy.reference_columns", err)
	}
	return columns, nil
}
