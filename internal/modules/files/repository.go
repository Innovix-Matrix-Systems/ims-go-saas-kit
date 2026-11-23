package files

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// CreateInput carries file creation input.
type CreateInput struct {
	UUID       uuid.UUID
	Name       string
	FileName   string
	Disk       string
	Path       string
	MimeType   string
	Size       int64
	Type       string
	OwnerType  *string
	OwnerID    *int64
	Collection *string
	TenantID   *int64
	CreatedBy  *int64
}

// ListFilter carries file list filters. AllTenants is true only for
// tenantless (system) callers; tenant-bound callers are scoped to TenantID.
type ListFilter struct {
	AllTenants bool
	TenantID   int64
	Type       string
	Attached   *bool
	Limit      int32
	Offset     int32
}

// Repository is the persistence contract for files.
type Repository interface {
	Create(ctx context.Context, input CreateInput) (*File, error)
	Get(ctx context.Context, id int64) (*File, error)
	GetInTenant(ctx context.Context, id int64, allTenants bool, tenantID int64) (*File, error)
	GetByOwner(ctx context.Context, ownerType string, ownerID int64, collection string) (*File, error)
	List(ctx context.Context, filter ListFilter) ([]File, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Delete(ctx context.Context, id int64, allTenants bool, tenantID int64) error
	ListProfileFiles(ctx context.Context, userIDs []int64) ([]File, error)
	// UsedBytes is the total size of a tenant's files (storage quota).
	UsedBytes(ctx context.Context, tenantID int64) (int64, error)
}

func (r *repository) UsedBytes(ctx context.Context, tenantID int64) (int64, error) {
	used, err := r.queries.SumFileBytesForTenant(ctx, tenantID)
	if err != nil {
		return 0, database.MapError("files.used_bytes", err)
	}
	return used, nil
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed file repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Create(ctx context.Context, input CreateInput) (*File, error) {
	row, err := r.queries.CreateFile(ctx, dbgen.CreateFileParams{
		Uuid:       uuidParam(input.UUID),
		Name:       input.Name,
		FileName:   input.FileName,
		Disk:       input.Disk,
		Path:       input.Path,
		MimeType:   input.MimeType,
		Size:       input.Size,
		Type:       input.Type,
		OwnerType:  input.OwnerType,
		OwnerID:    input.OwnerID,
		Collection: input.Collection,
		TenantID:   input.TenantID,
		CreatedBy:  input.CreatedBy,
		UpdatedBy:  input.CreatedBy,
	})
	if err != nil {
		return nil, database.MapError("files.create", err)
	}
	file := mapFile(row)
	return &file, nil
}

func (r *repository) Get(ctx context.Context, id int64) (*File, error) {
	row, err := r.queries.GetFile(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "File with ID '%d' was not found.", id)
		}
		return nil, database.MapError("files.get", err)
	}
	file := mapFile(row)
	return &file, nil
}

func (r *repository) GetInTenant(ctx context.Context, id int64, allTenants bool, tenantID int64) (*File, error) {
	row, err := r.queries.GetFileInTenant(ctx, dbgen.GetFileInTenantParams{
		ID:         id,
		AllTenants: allTenants,
		TenantID:   &tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Newf(apperr.ResourceNotFound, "File with ID '%d' was not found.", id)
		}
		return nil, database.MapError("files.get_in_tenant", err)
	}
	file := mapFile(row)
	return &file, nil
}

func (r *repository) GetByOwner(ctx context.Context, ownerType string, ownerID int64, collection string) (*File, error) {
	row, err := r.queries.GetFileByOwner(ctx, dbgen.GetFileByOwnerParams{
		OwnerType:  ownerType,
		OwnerID:    ownerID,
		Collection: collection,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.New(apperr.ResourceNotFound, "The file was not found.")
		}
		return nil, database.MapError("files.get_by_owner", err)
	}
	file := mapFile(row)
	return &file, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]File, error) {
	rows, err := r.queries.ListFiles(ctx, dbgen.ListFilesParams{
		AllTenants: filter.AllTenants,
		TenantID:   &filter.TenantID,
		Type:       optionalString(filter.Type),
		Attached:   filter.Attached,
		PageLimit:  filter.Limit,
		PageOffset: filter.Offset,
	})
	if err != nil {
		return nil, database.MapError("files.list", err)
	}
	items := make([]File, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapFile(row))
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, filter ListFilter) (int64, error) {
	total, err := r.queries.CountFiles(ctx, dbgen.CountFilesParams{
		AllTenants: filter.AllTenants,
		TenantID:   &filter.TenantID,
		Type:       optionalString(filter.Type),
		Attached:   filter.Attached,
	})
	if err != nil {
		return 0, database.MapError("files.count", err)
	}
	return total, nil
}

func (r *repository) Delete(ctx context.Context, id int64, allTenants bool, tenantID int64) error {
	affected, err := r.queries.DeleteFileInTenant(ctx, dbgen.DeleteFileInTenantParams{
		ID:         id,
		AllTenants: allTenants,
		TenantID:   &tenantID,
	})
	if err != nil {
		return database.MapError("files.delete", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "File with ID '%d' was not found.", id)
	}
	return nil
}

func (r *repository) ListProfileFiles(ctx context.Context, userIDs []int64) ([]File, error) {
	rows, err := r.queries.ListProfileFilesByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, database.MapError("files.profile_files", err)
	}
	items := make([]File, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapFile(row))
	}
	return items, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
