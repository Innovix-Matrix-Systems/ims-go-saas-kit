package announcements

import (
	"context"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/privacy"
)

// PrivacyContributor covers what a person did with announcements (viewed,
// clicked, dismissed). The announcements themselves are the platform's.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "announcements" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"announcement_events.user_id"} }

type privacyEvent struct {
	Announcement string     `json:"announcement"`
	Type         string     `json:"type"`
	At           *time.Time `json:"at"`
}

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	rows, err := q.ListAnnouncementEventsForUser(ctx, s.UserID)
	if err != nil {
		return database.MapError("announcements.privacy_export", err)
	}
	events := make([]privacyEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, privacyEvent{Announcement: row.Title, Type: row.Type, At: privacy.Time(row.CreatedAt)})
	}
	return w.JSON("events", events)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	deleted, err := q.DeleteAnnouncementEventsForUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("announcements.privacy_erase", err)
	}
	return privacy.Report{Deleted: deleted}, nil
}
