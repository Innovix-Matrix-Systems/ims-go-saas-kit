package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// Notification channels and preferences (phase 26.6): a notification in the
// feed can also be mailed, texted or pushed to the users it addresses, who
// opt out per event type (docs/guides/notifications.md).

// sendNotification creates a notification as the super admin and runs its
// fan-out inline (testkit does not run queue workers).
func sendNotification(t *testing.T, app *testkit.App, body map[string]any) int64 {
	t.Helper()
	var created struct {
		ID       int64    `json:"id"`
		Channels []string `json:"channels"`
	}
	app.As(app.SuperAdmin(t)).Post("/api/v1/internal/notification", body).Status(t, http.StatusCreated).Data(t, &created)
	if err := app.Deps.Platform.Notifications.ProcessFanOut(context.Background(), created.ID); err != nil {
		t.Fatalf("fan-out: %v", err)
	}
	return created.ID
}

func notificationDeliveries(t *testing.T, app *testkit.App, channel string, userID int64) int {
	t.Helper()
	return countRows(t, app, `SELECT count(*) FROM deliveries WHERE template = 'notification' AND channel = $1 AND user_id = $2`, channel, userID)
}

func TestNotificationsAreMailedToAddressedUsers(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	reader := app.User(t)
	optedOut := app.User(t)
	unverified := app.User(t)
	if _, err := app.Pool.Exec(ctx, `UPDATE users SET email_verified_at = NULL WHERE id = $1`, unverified.ID); err != nil {
		t.Fatalf("unverify: %v", err)
	}
	app.As(optedOut).Put("/api/v1/notification/preferences", map[string]any{
		"preferences": []map[string]any{{"event_type": notifications.EventRenewalReminder, "channel": "mail", "enabled": false}},
	}).Status(t, http.StatusOK)

	id := sendNotification(t, app, map[string]any{
		"event_type": notifications.EventRenewalReminder, "priority": "INFO", "title": "Renew soon",
		"body": "Your plan renews in 3 days.", "data": map[string]any{"url": "https://app.test/billing"},
		"tenant_id": testkit.DefaultTenant, "targets": []map[string]any{{"type": "TENANT"}},
		"channels": []string{"mail"},
	})
	if got := notificationDeliveries(t, app, "mail", reader.ID); got != 1 {
		t.Fatalf("mails to the reader = %d, want 1", got)
	}
	if got := notificationDeliveries(t, app, "mail", optedOut.ID); got != 0 {
		t.Fatalf("mails to a user who opted out = %d, want 0", got)
	}
	if got := notificationDeliveries(t, app, "mail", unverified.ID); got != 0 {
		t.Fatalf("mails to an unverified address = %d, want 0", got)
	}

	// The feed has it for everyone, opted out or not.
	var feed []struct {
		ID int64 `json:"id"`
	}
	app.As(optedOut).Get("/api/v1/notification/feed").Status(t, http.StatusOK).Data(t, &feed)
	if len(feed) == 0 || feed[0].ID != id {
		t.Fatalf("feed of the opted-out user = %+v, want the notification", feed)
	}

	// A retried fan-out sends nothing twice.
	if err := app.Deps.Platform.Notifications.ProcessFanOut(ctx, id); err != nil {
		t.Fatalf("second fan-out: %v", err)
	}
	if got := notificationDeliveries(t, app, "mail", reader.ID); got != 1 {
		t.Fatalf("mails after a retry = %d, want 1", got)
	}

	// The mail carries the action URL and a one-click unsubscribe link.
	data := app.Queued(t, "notification")
	if data["url"] != "https://app.test/billing" || !strings.Contains(data["unsubscribe"].(string), "/api/v1/notification/unsubscribe?token=") {
		t.Fatalf("notification data = %v", data)
	}
}

func TestOneClickUnsubscribe(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	sendNotification(t, app, map[string]any{
		"event_type": notifications.EventRenewalReminder, "priority": "INFO", "title": "Renew soon",
		"targets": []map[string]any{{"type": "USER", "id": user.ID}}, "tenant_id": testkit.DefaultTenant,
		"channels": []string{"mail"},
	})
	link, err := url.Parse(app.Queued(t, "notification")["unsubscribe"].(string))
	if err != nil {
		t.Fatalf("unsubscribe link: %v", err)
	}
	app.Guest().Get(link.RequestURI()).Status(t, http.StatusOK)
	requireErrorCode(t, app.Guest().Get("/api/v1/notification/unsubscribe?token=forged"), http.StatusUnprocessableEntity, "VALIDATION_FAILED")

	var preferences []struct {
		EventType string `json:"event_type"`
		Channel   string `json:"channel"`
		Enabled   bool   `json:"enabled"`
	}
	app.As(user).Get("/api/v1/notification/preferences").Status(t, http.StatusOK).Data(t, &preferences)
	for _, preference := range preferences {
		if preference.EventType == notifications.EventRenewalReminder && preference.Enabled {
			t.Fatalf("after unsubscribing, %s is still on", preference.Channel)
		}
	}
	app.As(user).Put("/api/v1/notification/preferences", map[string]any{
		"preferences": []map[string]any{{"event_type": notifications.EventRenewalReminder, "channel": "push", "enabled": true}},
	}).Status(t, http.StatusUnprocessableEntity) // the event does not use push
}

func TestNotificationSMSNeedsThePlanAVerifiedPhoneAndRespectsTheCap(t *testing.T) {
	t.Setenv("SMS_DAILY_LIMIT_PER_TENANT", "1")
	app := testkit.New(t)
	ctx := context.Background()
	withPhone := func(user *testkit.User, phone string) {
		t.Helper()
		if _, err := app.Pool.Exec(ctx, `UPDATE users SET phone = $2, phone_verified_at = now() WHERE id = $1`, user.ID, phone); err != nil {
			t.Fatalf("phone: %v", err)
		}
	}
	// The seeded tenant's plan gets sms.notifications; another tenant's does not.
	if _, err := app.Pool.Exec(ctx, `UPDATE subscriptions SET overrides = '{"sms.notifications": true}' WHERE tenant_id = $1`, testkit.DefaultTenant); err != nil {
		t.Fatalf("override: %v", err)
	}
	first, second, noPhone := app.User(t), app.User(t), app.User(t)
	withPhone(first, "+8801700000001")
	withPhone(second, "+8801700000002")
	otherTenant := app.Tenant(t, "No SMS plan")
	outsider := app.User(t, testkit.InTenant(otherTenant))
	withPhone(outsider, "+8801700000003")

	sendNotification(t, app, map[string]any{
		"event_type": notifications.EventRenewalReminder, "priority": "INFO", "title": "Renew soon",
		"tenant_id": testkit.DefaultTenant, "targets": []map[string]any{{"type": "TENANT"}}, "channels": []string{"sms"},
	})
	if sent := notificationDeliveries(t, app, "sms", first.ID) + notificationDeliveries(t, app, "sms", second.ID); sent != 1 {
		t.Fatalf("SMS sent = %d, want 1 (the daily cap)", sent)
	}
	if got := notificationDeliveries(t, app, "sms", noPhone.ID); got != 0 {
		t.Fatalf("SMS to a user without a phone = %d", got)
	}

	sendNotification(t, app, map[string]any{
		"event_type": notifications.EventRenewalReminder, "priority": "INFO", "title": "Renew soon",
		"tenant_id": otherTenant, "targets": []map[string]any{{"type": "TENANT"}}, "channels": []string{"sms"},
	})
	if got := notificationDeliveries(t, app, "sms", outsider.ID); got != 0 {
		t.Fatalf("SMS without the plan feature = %d, want 0", got)
	}
}
