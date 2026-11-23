package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// NotifyRenewalReminder creates the in-app renewal reminder for a tenant
// (billing depends on this through its small Notifier interface).
func (s *Service) NotifyRenewalReminder(ctx context.Context, tenantID, subscriptionID int64, expiresAt time.Time, daysLeft int) error {
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
	title := fmt.Sprintf("Subscription renews in %d day(s)", daysLeft)
	body := fmt.Sprintf("The subscription expires on %s.", expiresAt.Format("2006-01-02 15:04:05"))

	_, _, err = s.Create(ctx, tenancy.Scope{Internal: true}, 0, CreateInput{
		TenantID:  &tenant,
		EventType: EventRenewalReminder,
		Priority:  PriorityWarning,
		Title:     title,
		Body:      &body,
		Data:      data,
		GroupKey:  &groupKey,
		Targets:   []TargetInput{{Type: TargetTenant}},
		Channels:  []string{string(delivery.Mail)},
	})
	return err
}
