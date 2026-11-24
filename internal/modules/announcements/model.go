// Package announcements lets platform staff tell organizations something:
// a notice, a warning, or a promotion with a button. Each announcement is one
// row whose audience is every organization or a chosen few; members see it
// as a banner while it runs, and their views, clicks and dismissals are
// counted once per person. Channels optionally deliver it once through the
// notification system (in-app, mail, sms, push) when it starts.
package announcements

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
)

// Kinds, audiences, statuses and event types.
const (
	KindInfo      = "info"
	KindPromotion = "promotion"
	KindWarning   = "warning"
	KindCritical  = "critical"

	AudienceAll     = "all"
	AudienceTenants = "tenants"

	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"

	EventView    = "view"
	EventClick   = "click"
	EventDismiss = "dismiss"
)

// Announcement is one announcement.
type Announcement struct {
	ID          int64
	Title       string
	Body        *string
	Kind        string
	CTALabel    *string
	CTAURL      *string
	Audience    string
	StartsAt    time.Time
	EndsAt      *time.Time
	Dismissible bool
	Status      string
	Channels    []string
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// State is where an announcement is in its life: a draft, scheduled (not
// started), live, ended (its window is over) or archived.
func (a Announcement) State(now time.Time) string {
	switch {
	case a.Status == StatusDraft:
		return "draft"
	case a.Status == StatusArchived:
		return "archived"
	case a.StartsAt.After(now):
		return "scheduled"
	case a.EndsAt != nil && !a.EndsAt.After(now):
		return "ended"
	default:
		return "live"
	}
}

// Totals are unique people who viewed, clicked and dismissed.
type Totals struct {
	Views      int64 `json:"views"`
	Clicks     int64 `json:"clicks"`
	Dismissals int64 `json:"dismissals"`
}

func optionalTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	value := t.Time
	return &value
}

func mapAnnouncement(row dbgen.Announcement) Announcement {
	channels := row.Channels
	if channels == nil {
		channels = []string{}
	}
	return Announcement{
		ID: row.ID, Title: row.Title, Body: row.Body, Kind: row.Kind, CTALabel: row.CtaLabel, CTAURL: row.CtaUrl,
		Audience: row.Audience, StartsAt: row.StartsAt.Time, EndsAt: optionalTime(row.EndsAt), Dismissible: row.Dismissible,
		Status: row.Status, Channels: channels, DeliveredAt: optionalTime(row.DeliveredAt),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}
