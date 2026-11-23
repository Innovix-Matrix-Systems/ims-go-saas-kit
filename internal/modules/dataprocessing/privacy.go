package dataprocessing

import (
	"context"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/privacy"
)

// PrivacyContributor covers processing jobs. Personal-data exports of the
// user (their own, or made for them by an admin) are their data: the jobs
// and archives are deleted. Import/export jobs the user started hold tenant
// data, so they stay and only lose the link to the user.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "dataprocessing" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"data_processing_jobs.user_id"} }

// Export implements privacy.Contributor: nothing personal is stored here
// (earlier exports are copies of this one).
func (PrivacyContributor) Export(context.Context, *dbgen.Queries, privacy.Subject, privacy.Writer) error {
	return nil
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	exports, err := q.DeletePrivacyExportJobsAbout(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("dataprocessing.privacy_erase", err)
	}
	report := privacy.Report{Deleted: int64(len(exports))}
	for _, job := range exports {
		if job.FileDisk != nil && job.FilePath != nil && *job.FilePath != "" {
			report.Objects = append(report.Objects, privacy.Object{Disk: *job.FileDisk, Path: *job.FilePath})
		}
	}
	detached, err := q.DetachProcessingJobsFromUser(ctx, s.UserIDPtr())
	if err != nil {
		return privacy.Report{}, database.MapError("dataprocessing.privacy_erase", err)
	}
	report.Anonymized = detached
	return report, nil
}
