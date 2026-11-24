package notifications

import (
	"context"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// PrivacyContributor covers notifications addressed to the user, their read
// state and their channel preferences. Notifications themselves are the
// sender's (or the tenant's) and stay; only the user's link to them goes.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "notifications" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string {
	return []string{
		"notification_deliveries.user_id",
		"notification_reads.user_id",
		"notification_targets.target_id",
		"notification_preferences.user_id",
	}
}

type privacyNotification struct {
	EventType string     `json:"event_type"`
	Title     string     `json:"title"`
	Body      *string    `json:"body"`
	CreatedAt *time.Time `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

type privacyPreference struct {
	EventType string `json:"event_type"`
	Channel   string `json:"channel"`
	Enabled   bool   `json:"enabled"`
}

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	rows, err := q.ListNotificationsForPrivacy(ctx, s.UserID)
	if err != nil {
		return database.MapError("notifications.privacy_export", err)
	}
	received := make([]privacyNotification, 0, len(rows))
	for _, n := range rows {
		received = append(received, privacyNotification{
			EventType: n.EventType, Title: n.Title, Body: n.Body,
			CreatedAt: privacy.Time(n.CreatedAt), ReadAt: privacy.Time(n.ReadAt),
		})
	}
	if err := w.JSON("received", received); err != nil {
		return err
	}
	prefs, err := q.ListNotificationPreferences(ctx, s.UserID)
	if err != nil {
		return database.MapError("notifications.privacy_export", err)
	}
	preferences := make([]privacyPreference, 0, len(prefs))
	for _, p := range prefs {
		preferences = append(preferences, privacyPreference{EventType: p.EventType, Channel: p.Channel, Enabled: p.Enabled})
	}
	return w.JSON("preferences", preferences)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	reads, err := q.DeleteNotificationReadsForUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("notifications.privacy_erase", err)
	}
	targets, err := q.DeleteNotificationTargetsForUser(ctx, s.UserIDPtr())
	if err != nil {
		return privacy.Report{}, database.MapError("notifications.privacy_erase", err)
	}
	prefs, err := q.DeleteNotificationPreferencesForUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("notifications.privacy_erase", err)
	}
	deliveries, err := q.DeleteNotificationDeliveriesForUser(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("notifications.privacy_erase", err)
	}
	return privacy.Report{Deleted: reads + targets + prefs + deliveries}, nil
}
