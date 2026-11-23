package dev

import (
	"context"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/privacy"
)

// PrivacyContributor covers the developer dashboard's records. Captured
// requests (headers, bodies, IP) and everything linked to them are local
// tooling and are deleted; the dashboard audit log keeps its rows (like the
// activity log) without the IP and browser.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "dev" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string {
	return []string{"dev_requests.user_id", "dev_requests.impersonator_id", "dev_actions.actor_user_id"}
}

// Export implements privacy.Contributor: diagnostics, not data the user gave
// the application.
func (PrivacyContributor) Export(context.Context, *dbgen.Queries, privacy.Subject, privacy.Writer) error {
	return nil
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	deleted, err := q.DeleteDevTelemetryForUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("dev.privacy_erase", err)
	}
	scrubbed, err := q.ScrubDevActionsClient(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("dev.privacy_erase", err)
	}
	return privacy.Report{Deleted: deleted, Anonymized: scrubbed}, nil
}
