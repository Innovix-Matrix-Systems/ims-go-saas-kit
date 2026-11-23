package backup

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

type CreateRunInput struct {
	Trigger      string
	ActorUserID  int64
	Filename     string
	Scope        string
	DatabaseName string
}

type ListFilter struct {
	Status   string
	BeforeID int64
	Limit    int32
}

type CompleteInput struct {
	Status         string
	Filename       string
	SizeBytes      int64
	ChecksumSHA256 string
	PgDumpVersion  string
	DatabaseName   string
	Destinations   []DestinationResult
	ErrorMessage   string
}

type Repository interface {
	Create(ctx context.Context, input CreateRunInput) (*Run, error)
	Get(ctx context.Context, id int64) (*Run, error)
	List(ctx context.Context, filter ListFilter) ([]Run, error)
	Stats(ctx context.Context) (Stats, error)
	SetRunning(ctx context.Context, id int64) (*Run, error)
	Complete(ctx context.Context, id int64, input CompleteInput) (*Run, error)
	Fail(ctx context.Context, id int64, message string, destinations []DestinationResult) (*Run, error)
	SoftDelete(ctx context.Context, id int64) (bool, error)
	ForCleanup(ctx context.Context) ([]CleanupRun, error)
	LatestSuccessful(ctx context.Context) (*Run, error)
}

type repository struct {
	queries *dbgen.Queries
}

func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Create(ctx context.Context, input CreateRunInput) (*Run, error) {
	var actor *int64
	if input.ActorUserID > 0 {
		actor = &input.ActorUserID
	}
	var databaseName *string
	if input.DatabaseName != "" {
		databaseName = &input.DatabaseName
	}
	row, err := r.queries.CreateBackupRun(ctx, dbgen.CreateBackupRunParams{
		Uuid:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TriggerKind:  input.Trigger,
		ActorUserID:  actor,
		Filename:     input.Filename,
		Scope:        input.Scope,
		DatabaseName: databaseName,
	})
	if err != nil {
		return nil, database.MapError("backup.create_run", err)
	}
	run, err := mapRun(row)
	if err != nil {
		return nil, database.MapError("backup.create_run", err)
	}
	return &run, nil
}

func (r *repository) Get(ctx context.Context, id int64) (*Run, error) {
	row, err := r.queries.GetBackupRun(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Backup run with ID '%d' was not found.", id)
		}
		return nil, database.MapError("backup.get_run", err)
	}
	run, err := mapRun(row)
	if err != nil {
		return nil, database.MapError("backup.get_run", err)
	}
	return &run, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]Run, error) {
	rows, err := r.queries.ListBackupRuns(ctx, dbgen.ListBackupRunsParams{
		BeforeID: filter.BeforeID,
		Status:   optionalText(filter.Status),
		RowLimit: filter.Limit,
	})
	if err != nil {
		return nil, database.MapError("backup.list_runs", err)
	}
	runs := make([]Run, 0, len(rows))
	for _, row := range rows {
		run, err := mapRun(row)
		if err != nil {
			return nil, database.MapError("backup.list_runs", err)
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (r *repository) Stats(ctx context.Context) (Stats, error) {
	row, err := r.queries.BackupRunStats(ctx)
	if err != nil {
		return Stats{}, database.MapError("backup.stats", err)
	}
	return Stats{
		TotalRuns:      row.TotalRuns,
		TotalSize:      row.TotalSize,
		SuccessfulRuns: row.SuccessfulRuns,
		LastSuccessAt:  timePtr(row.LastSuccessAt),
	}, nil
}

func (r *repository) SetRunning(ctx context.Context, id int64) (*Run, error) {
	row, err := r.queries.SetBackupRunRunning(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Backup run with ID '%d' was not found.", id)
		}
		return nil, database.MapError("backup.set_running", err)
	}
	run, err := mapRun(row)
	if err != nil {
		return nil, database.MapError("backup.set_running", err)
	}
	return &run, nil
}

func (r *repository) Complete(ctx context.Context, id int64, input CompleteInput) (*Run, error) {
	payload, err := encodeDestinations(input.Destinations)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "encode backup destinations", err)
	}
	var checksum, version, databaseName *string
	if input.ChecksumSHA256 != "" {
		checksum = &input.ChecksumSHA256
	}
	if input.PgDumpVersion != "" {
		version = &input.PgDumpVersion
	}
	if input.DatabaseName != "" {
		databaseName = &input.DatabaseName
	}
	row, err := r.queries.CompleteBackupRun(ctx, dbgen.CompleteBackupRunParams{
		Status:         input.Status,
		Filename:       input.Filename,
		SizeBytes:      input.SizeBytes,
		ChecksumSha256: checksum,
		PgDumpVersion:  version,
		DatabaseName:   databaseName,
		Destinations:   payload,
		ErrorMessage:   input.ErrorMessage,
		ID:             id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Backup run with ID '%d' was not found.", id)
		}
		return nil, database.MapError("backup.complete_run", err)
	}
	run, err := mapRun(row)
	if err != nil {
		return nil, database.MapError("backup.complete_run", err)
	}
	return &run, nil
}

func (r *repository) Fail(ctx context.Context, id int64, message string, destinations []DestinationResult) (*Run, error) {
	payload, err := encodeDestinations(destinations)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "encode backup destinations", err)
	}
	row, err := r.queries.FailBackupRun(ctx, dbgen.FailBackupRunParams{
		ErrorMessage: message,
		Destinations: payload,
		ID:           id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "Backup run with ID '%d' was not found.", id)
		}
		return nil, database.MapError("backup.fail_run", err)
	}
	run, err := mapRun(row)
	if err != nil {
		return nil, database.MapError("backup.fail_run", err)
	}
	return &run, nil
}

func (r *repository) SoftDelete(ctx context.Context, id int64) (bool, error) {
	affected, err := r.queries.SoftDeleteBackupRun(ctx, id)
	if err != nil {
		return false, database.MapError("backup.soft_delete", err)
	}
	return affected > 0, nil
}

func (r *repository) ForCleanup(ctx context.Context) ([]CleanupRun, error) {
	rows, err := r.queries.ListBackupRunsForCleanup(ctx)
	if err != nil {
		return nil, database.MapError("backup.cleanup_list", err)
	}
	runs := make([]CleanupRun, 0, len(rows))
	for _, row := range rows {
		var destinations []DestinationResult
		if len(row.Destinations) > 0 {
			_ = json.Unmarshal(row.Destinations, &destinations)
		}
		runs = append(runs, CleanupRun{
			ID:           row.ID,
			Filename:     row.Filename,
			CreatedAt:    row.CreatedAt.Time,
			SizeBytes:    row.SizeBytes,
			Destinations: destinations,
		})
	}
	return runs, nil
}

func (r *repository) LatestSuccessful(ctx context.Context) (*Run, error) {
	row, err := r.queries.LatestSuccessfulBackup(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.New(apperr.ResourceNotFound, "No successful backup was found.")
		}
		return nil, database.MapError("backup.latest_successful", err)
	}
	run, err := mapRun(row)
	if err != nil {
		return nil, database.MapError("backup.latest_successful", err)
	}
	return &run, nil
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// encodeDestinations stores a run's destinations as a JSON array, never
// null: a run that fails before copying anywhere has no results.
func encodeDestinations(destinations []DestinationResult) ([]byte, error) {
	if destinations == nil {
		destinations = []DestinationResult{}
	}
	return json.Marshal(destinations)
}
