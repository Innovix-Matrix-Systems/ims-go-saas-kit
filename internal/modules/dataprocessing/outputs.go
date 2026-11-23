package dataprocessing

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// An export's file is also listed in the organization's files (or, for a
// platform export, the global files), attached to its job. It stays as long
// as the job does: removing or cleaning up the job removes it, and the files
// routes refuse to delete it on their own.
const (
	OutputOwnerType  = "data_processing_job"
	OutputCollection = "output"
)

// registerOutput lists a finished export's file. Imports (the file is the
// uploaded input) and personal-data exports (they belong to one person) are
// not listed.
func (s *Service) registerOutput(ctx context.Context, id int64, file FileInput) error {
	if s.queries == nil {
		return nil
	}
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Type != TypeExport {
		return nil
	}
	if err := s.removeOutput(ctx, id); err != nil {
		return err
	}
	ownerType, collection := OutputOwnerType, OutputCollection
	mime := file.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	_, err = s.queries.CreateFile(ctx, dbgen.CreateFileParams{
		Uuid:       pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name:       file.Name,
		FileName:   file.Name,
		Disk:       file.Disk,
		Path:       file.Path,
		MimeType:   mime,
		Size:       file.Size,
		Type:       "document",
		OwnerType:  &ownerType,
		OwnerID:    &id,
		Collection: &collection,
		TenantID:   job.TenantID,
		CreatedBy:  job.UserID,
		UpdatedBy:  job.UserID,
	})
	if err != nil {
		return database.MapError("dataprocessing.register_output", err)
	}
	return nil
}

// removeOutput drops the job's files entry (the stored object is the job's
// and is removed with it).
func (s *Service) removeOutput(ctx context.Context, id int64) error {
	if s.queries == nil {
		return nil
	}
	if _, err := s.queries.DeleteFilesByOwner(ctx, dbgen.DeleteFilesByOwnerParams{
		OwnerType:  OutputOwnerType,
		OwnerID:    id,
		Collection: OutputCollection,
	}); err != nil {
		return database.MapError("dataprocessing.remove_output", err)
	}
	return nil
}
