// Package notifications implements the DB-backed notification fan-out model:
// notifications with typed targets, per-user deliveries and reads, cached
// unread counters with a database recompute fallback, and a cursor feed.
package notifications

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
)

// Notification priorities.
const (
	PriorityInfo     = "INFO"
	PriorityWarning  = "WARNING"
	PriorityCritical = "CRITICAL"
)

// Notification target types.
const (
	TargetUser   = "USER"
	TargetRole   = "ROLE"
	TargetBranch = "BRANCH"
	TargetTenant = "TENANT"
	TargetGlobal = "GLOBAL"
)

// EventRenewalReminder is the shipped event type; the catalog (Events in
// channels.go) is the extension point for future notification kinds.
const EventRenewalReminder = "renewal.reminder"

// Priorities is the validated priority catalog.
var Priorities = map[string]struct{}{
	PriorityInfo:     {},
	PriorityWarning:  {},
	PriorityCritical: {},
}

// queueForPriority routes by priority: CRITICAL jobs run on
// the critical queue, everything else on default.
func queueForPriority(priority string) queue.Name {
	if priority == PriorityCritical {
		return queue.QueueCritical
	}
	return queue.QueueDefault
}

// Notification is the domain representation of a notification.
type Notification struct {
	ID        int64
	TenantID  *int64
	EventType string
	Priority  string
	Title     string
	Body      *string
	Data      []byte
	GroupKey  *string
	CreatedBy *int64
	UpdatedBy *int64
	ExpiresAt *time.Time
	// Channels: "database" (the feed, always first) plus any of mail, sms,
	// push.
	Channels  []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Target is one addressability rule of a notification.
type Target struct {
	ID             int64
	TenantID       *int64
	NotificationID int64
	TargetType     string
	TargetID       *int64
}

// FeedItem is one feed row annotated with the reader's read state.
type FeedItem struct {
	ID        int64
	EventType string
	Priority  string
	Title     string
	Body      *string
	Data      []byte
	GroupKey  *string
	CreatedAt time.Time
	IsRead    bool
}

func mapNotification(row dbgen.Notification) Notification {
	return Notification{
		ID:        row.ID,
		TenantID:  row.TenantID,
		EventType: row.EventType,
		Priority:  row.Priority,
		Title:     row.Title,
		Body:      row.Body,
		Data:      row.Data,
		GroupKey:  row.GroupKey,
		CreatedBy: row.CreatedBy,
		UpdatedBy: row.UpdatedBy,
		ExpiresAt: timePtr(row.ExpiresAt),
		Channels:  row.Channels,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func mapTarget(row dbgen.NotificationTarget) Target {
	return Target{
		ID:             row.ID,
		TenantID:       row.TenantID,
		NotificationID: row.NotificationID,
		TargetType:     row.TargetType,
		TargetID:       row.TargetID,
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	value := ts.Time
	return &value
}
