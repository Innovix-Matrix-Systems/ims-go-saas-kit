package featureflags

import (
	"context"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/privacy"
)

// PrivacyContributor covers feature flags overridden for the user.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "featureflags" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"feature_flags.target_id"} }

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	target := string(TargetUser)
	rows, err := q.ListFeatureFlagsByTarget(ctx, dbgen.ListFeatureFlagsByTargetParams{TargetType: &target, TargetID: s.UserIDPtr()})
	if err != nil {
		return database.MapError("featureflags.privacy_export", err)
	}
	flags := make(map[string]bool, len(rows))
	for _, row := range rows {
		flags[row.Key] = row.Value
	}
	return w.JSON("user_flags", flags)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	target := string(TargetUser)
	deleted, err := q.DeleteFeatureFlagsByTarget(ctx, dbgen.DeleteFeatureFlagsByTargetParams{TargetType: &target, TargetID: s.UserIDPtr()})
	if err != nil {
		return privacy.Report{}, database.MapError("featureflags.privacy_erase", err)
	}
	return privacy.Report{Deleted: deleted}, nil
}
