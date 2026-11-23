package webhooks

import (
	"context"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// PrivacyContributor scrubs stored delivery payloads about the user: they
// copy the activity entry's changes (a name, an e-mail). The events were the
// tenant's, so nothing is exported. Endpoints' created_by points at the
// tombstone like any audit stamp.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "webhooks" }

// Columns implements privacy.Contributor: payloads carry the user inside
// jsonb, not in a column.
func (PrivacyContributor) Columns() []string { return nil }

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(context.Context, *dbgen.Queries, privacy.Subject, privacy.Writer) error {
	return nil
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	scrubbed, err := q.ScrubWebhookPayloadsAboutUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("webhooks.privacy_erase", err)
	}
	return privacy.Report{Anonymized: scrubbed}, nil
}
