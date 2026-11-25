package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/webhooks"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// eventWebhookDisabled is the notification sent when an endpoint is
// disabled after failing repeatedly (mandatory: it is how the tenant learns
// its integration stopped).
const eventWebhookDisabled = "webhook.disabled"

// webhookNotices tells the person who created an endpoint (or, when that is
// unknown, the tenant) that it was disabled: in the feed and by mail.
type webhookNotices struct {
	notifications *notifications.Service
	logger        *slog.Logger
}

func newWebhookNotices(service *notifications.Service, logger *slog.Logger) webhookNotices {
	notifications.RegisterEvent(eventWebhookDisabled, notifications.Event{
		Description: "A webhook endpoint was disabled after failing repeatedly.",
		Channels:    []delivery.Channel{delivery.Mail},
		Mandatory:   true,
	})
	return webhookNotices{notifications: service, logger: logger}
}

// EndpointDisabled implements webhooks.Notifier.
func (n webhookNotices) EndpointDisabled(ctx context.Context, endpoint webhooks.Endpoint) {
	tenantID := endpoint.TenantID
	target := notifications.TargetInput{Type: notifications.TargetTenant}
	if endpoint.CreatedBy != nil {
		target = notifications.TargetInput{Type: notifications.TargetUser, ID: endpoint.CreatedBy}
	}
	body := fmt.Sprintf("%s stopped receiving events after its deliveries kept failing. Fix the receiver, then turn the endpoint back on.", endpoint.URL)
	_, _, err := n.notifications.Create(ctx, tenancy.Scope{Internal: true}, 0, notifications.CreateInput{
		TenantID:  &tenantID,
		EventType: eventWebhookDisabled,
		Priority:  notifications.PriorityWarning,
		Title:     "A webhook endpoint was disabled",
		Body:      &body,
		Targets:   []notifications.TargetInput{target},
		Channels:  []string{string(delivery.Mail)},
	})
	if err != nil {
		n.logger.Warn("webhooks: disabled-endpoint notice not sent", "endpoint_id", endpoint.ID, "error", err)
	}
}
