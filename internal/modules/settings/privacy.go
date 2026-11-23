package settings

import (
	"context"
	"encoding/json"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// PrivacyContributor covers settings stored for the user (target "user").
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "settings" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"settings.target_id"} }

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	target := TargetUser
	rows, err := q.ListSettingsByTarget(ctx, dbgen.ListSettingsByTargetParams{TargetType: &target, TargetID: s.UserIDPtr()})
	if err != nil {
		return database.MapError("settings.privacy_export", err)
	}
	values := make(map[string]json.RawMessage, len(rows))
	for _, row := range rows {
		values[row.Key] = privacyValue(row.Value)
	}
	return w.JSON("user_settings", values)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	target := TargetUser
	deleted, err := q.DeleteSettingsByTarget(ctx, dbgen.DeleteSettingsByTargetParams{TargetType: &target, TargetID: s.UserIDPtr()})
	if err != nil {
		return privacy.Report{}, database.MapError("settings.privacy_erase", err)
	}
	return privacy.Report{Deleted: deleted}, nil
}

// privacyValue keeps a stored JSON value as is and quotes anything else.
func privacyValue(value []byte) json.RawMessage {
	if len(value) > 0 && json.Valid(value) {
		return json.RawMessage(value)
	}
	encoded, _ := json.Marshal(string(value))
	return encoded
}
