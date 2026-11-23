package oauth

import (
	"context"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/privacy"
)

// PrivacyContributor covers the user's MCP connections (OAuth grants).
// Deleting a grant removes its codes, refresh tokens and access tokens too.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "oauth" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"oauth_grants.user_id"} }

type privacyGrant struct {
	ClientID    string     `json:"client_id"`
	ClientName  string     `json:"client_name"`
	RedirectURI string     `json:"redirect_uri"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   *time.Time `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
}

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	rows, err := q.ListOAuthGrantsForPrivacy(ctx, s.UserID)
	if err != nil {
		return database.MapError("oauth.privacy_export", err)
	}
	grants := make([]privacyGrant, 0, len(rows))
	for _, g := range rows {
		grants = append(grants, privacyGrant{
			ClientID: g.ClientID, ClientName: g.ClientName, RedirectURI: g.RedirectUri, Scopes: g.Scopes,
			CreatedAt: privacy.Time(g.CreatedAt), LastUsedAt: privacy.Time(g.LastUsedAt), RevokedAt: privacy.Time(g.RevokedAt),
		})
	}
	return w.JSON("connections", grants)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	deleted, err := q.DeleteOAuthGrantsForUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("oauth.privacy_erase", err)
	}
	return privacy.Report{Deleted: deleted}, nil
}
