package access

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/privacy"
)

// PrivacyContributor covers the user's role assignments.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "access" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"model_has_roles.model_id"} }

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	roles, err := q.UserRoleNames(ctx, dbgen.UserRoleNamesParams{ModelType: authz.ModelTypeUser, ModelID: s.UserID})
	if err != nil {
		return database.MapError("access.privacy_export", err)
	}
	return w.JSON("roles", roles)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	roles, err := q.UserRoleNames(ctx, dbgen.UserRoleNamesParams{ModelType: authz.ModelTypeUser, ModelID: s.UserID})
	if err != nil {
		return privacy.Report{}, database.MapError("access.privacy_erase", err)
	}
	if err := q.ClearUserRoles(ctx, dbgen.ClearUserRolesParams{ModelType: authz.ModelTypeUser, ModelID: s.UserID}); err != nil {
		return privacy.Report{}, database.MapError("access.privacy_erase", err)
	}
	return privacy.Report{Deleted: int64(len(roles))}, nil
}
