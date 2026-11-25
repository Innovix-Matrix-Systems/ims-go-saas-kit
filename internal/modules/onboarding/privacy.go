package onboarding

import (
	"context"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/privacy"
)

// PrivacyContributor covers the documents a user accepted and the
// organizations they own. Acceptances are evidence of consent and stay, but
// lose the IP address and browser; the ownership link points at the erased
// user's tombstone like created_by does.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "onboarding" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string {
	return []string{"user_agreements.user_id", "tenant_onboarding.owner_user_id"}
}

type privacyAgreement struct {
	Document   string     `json:"document"`
	Version    string     `json:"version"`
	AcceptedAt *time.Time `json:"accepted_at"`
	IPAddress  *string    `json:"ip_address"`
	UserAgent  *string    `json:"user_agent"`
}

type privacyOwnership struct {
	TenantID    int64      `json:"tenant_id"`
	Source      string     `json:"source"`
	CompletedAt *time.Time `json:"setup_completed_at"`
}

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	rows, err := q.ListUserAgreements(ctx, s.UserID)
	if err != nil {
		return database.MapError("onboarding.privacy_export", err)
	}
	agreements := make([]privacyAgreement, 0, len(rows))
	for _, row := range rows {
		agreements = append(agreements, privacyAgreement{
			Document: row.Document, Version: row.Version, AcceptedAt: privacy.Time(row.AcceptedAt),
			IPAddress: row.IpAddress, UserAgent: row.UserAgent,
		})
	}
	if err := w.JSON("agreements", agreements); err != nil {
		return err
	}
	owned, err := q.ListOnboardingOwnedByUser(ctx, s.UserIDPtr())
	if err != nil {
		return database.MapError("onboarding.privacy_export", err)
	}
	ownership := make([]privacyOwnership, 0, len(owned))
	for _, row := range owned {
		ownership = append(ownership, privacyOwnership{TenantID: row.TenantID, Source: row.Source, CompletedAt: privacy.Time(row.CompletedAt)})
	}
	return w.JSON("organizations_owned", ownership)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	scrubbed, err := q.ScrubUserAgreements(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("onboarding.privacy_erase", err)
	}
	return privacy.Report{Anonymized: scrubbed}, nil
}
