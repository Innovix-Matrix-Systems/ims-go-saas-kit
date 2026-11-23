package activity

import (
	"context"
	"encoding/json"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// PrivacyContributor covers the activity log (docs/design/account-deletion-
// and-export.md D6). Rows are kept, so the tenant's audit trail has no holes,
// and scrubbed so they no longer identify the person:
//
//   - rows the user performed keep actor_id (now the tombstone), lose the IP;
//   - rows about the user (created, updated, their logins, act-as on them)
//     keep the action and lose the copied values;
//   - failed logins with the user's e-mail lose the e-mail and the IP;
//   - entries the user made while acting as someone show "Deleted user".
//
// activity.prune still removes every row once ACTIVITY_RETENTION_DAYS pass.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "activity" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string {
	return []string{"activity_log.actor_id", "activity_log.subject_id", "activity_log.impersonator_id"}
}

type privacyEntry struct {
	Action      string          `json:"action"`
	SubjectType string          `json:"subject_type,omitempty"`
	SubjectID   *int64          `json:"subject_id,omitempty"`
	ActorID     *int64          `json:"actor_id,omitempty"`
	Changes     json.RawMessage `json:"changes"`
	CreatedAt   *time.Time      `json:"created_at"`
}

// Export implements privacy.Contributor: what the user did, and what was
// recorded about them.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	performed, err := q.ListActivityByActorForPrivacy(ctx, s.UserIDPtr())
	if err != nil {
		return database.MapError("activity.privacy_export", err)
	}
	entries := make([]privacyEntry, 0, len(performed))
	for _, row := range performed {
		entries = append(entries, privacyEntry{
			Action: row.Action, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
			Changes: rawChanges(row.Changes), CreatedAt: privacy.Time(row.CreatedAt),
		})
	}
	if err := w.JSON("performed", entries); err != nil {
		return err
	}
	about, err := q.ListActivityAboutUserForPrivacy(ctx, s.UserIDPtr())
	if err != nil {
		return database.MapError("activity.privacy_export", err)
	}
	entries = make([]privacyEntry, 0, len(about))
	for _, row := range about {
		entries = append(entries, privacyEntry{
			Action: row.Action, ActorID: row.ActorID,
			Changes: rawChanges(row.Changes), CreatedAt: privacy.Time(row.CreatedAt),
		})
	}
	return w.JSON("about_me", entries)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	var report privacy.Report
	steps := []func() (int64, error){
		func() (int64, error) { return q.ScrubActivityActorIP(ctx, s.UserIDPtr()) },
		func() (int64, error) { return q.ScrubActivityAboutUser(ctx, s.UserIDPtr()) },
		func() (int64, error) { return q.ScrubActivityImpersonatorName(ctx, s.UserIDPtr()) },
	}
	if s.Email != "" {
		steps = append(steps, func() (int64, error) { return q.ScrubFailedLoginsByEmail(ctx, s.Email) })
	}
	for _, step := range steps {
		n, err := step()
		if err != nil {
			return privacy.Report{}, database.MapError("activity.privacy_erase", err)
		}
		report.Anonymized += n
	}
	return report, nil
}

func rawChanges(changes []byte) json.RawMessage {
	if len(changes) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(changes)
}
