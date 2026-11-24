package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/announcements"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// notifyAnnouncement delivers an announcement to one organization's members
// through the notification system: one notification addressed to the whole
// organization, on the announcement's channels, grouped by announcement and
// gone when the announcement ends.
func notifyAnnouncement(ctx context.Context, service *notifications.Service, tenantID int64, a announcements.Announcement) error {
	data, err := json.Marshal(map[string]any{"announcement_id": a.ID, "kind": a.Kind, "cta_label": a.CTALabel, "cta_url": a.CTAURL})
	if err != nil {
		return err
	}
	priority := "INFO"
	switch a.Kind {
	case announcements.KindCritical:
		priority = "CRITICAL"
	case announcements.KindWarning:
		priority = "WARNING"
	}
	group := fmt.Sprintf("announcement.%d", a.ID)
	tenant := tenantID
	_, _, err = service.Create(ctx, tenancy.Scope{Internal: true}, 0, notifications.CreateInput{
		TenantID:  &tenant,
		EventType: notifications.EventAnnouncement,
		Priority:  priority,
		Title:     a.Title,
		Body:      a.Body,
		Data:      data,
		GroupKey:  &group,
		ExpiresAt: a.EndsAt,
		Targets:   []notifications.TargetInput{{Type: notifications.TargetTenant}},
		Channels:  a.Channels,
	})
	return err
}
