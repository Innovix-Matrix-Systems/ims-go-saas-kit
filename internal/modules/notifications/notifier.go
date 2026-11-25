package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// NotifyRenewalReminder creates the in-app renewal reminder for a tenant
// (billing depends on this through its small Notifier interface).
func (s *Service) NotifyRenewalReminder(ctx context.Context, tenantID, subscriptionID int64, expiresAt time.Time, daysLeft int) (err error) {
	ctx, end := tracing.Begin(ctx, "notifications.renewal_reminder",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("subscription.id", subscriptionID),
		attribute.Int("days_left", daysLeft),
	)
	defer end(&err)
	tenant := tenantID
	data, err := json.Marshal(map[string]any{
		"subscription_id": subscriptionID,
		"expires_at":      expiresAt.UTC().Format(time.RFC3339),
		"days_before":     daysLeft,
	})
	if err != nil {
		return err
	}
	groupKey := fmt.Sprintf("billing.renewal.%d", subscriptionID)

	_, _, err = s.Create(ctx, tenancy.Scope{Internal: true}, 0, CreateInput{
		TenantID:  &tenant,
		EventType: EventRenewalReminder,
		Priority:  PriorityWarning,
		Message: &Message{Key: "notifications.renewal_reminder", Params: map[string]any{
			"Count": daysLeft, "Date": expiresAt.Format("2006-01-02 15:04"),
		}},
		Data:     data,
		GroupKey: &groupKey,
		Targets:  []TargetInput{{Type: TargetTenant}},
		Channels: []string{string(delivery.Mail)},
	})
	return err
}
