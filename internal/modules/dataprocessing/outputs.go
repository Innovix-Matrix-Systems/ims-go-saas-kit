package dataprocessing

import "context"

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
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Type != TypeExport {
		return nil
	}
	if err := s.repo.RemoveOutputFile(ctx, id); err != nil {
		return err
	}
	if file.Mime == "" {
		file.Mime = "application/octet-stream"
	}
	return s.repo.AddOutputFile(ctx, *job, file)
}
