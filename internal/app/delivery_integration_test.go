package app_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// The delivery system (phase 26.2): account mails go through it, the log
// keeps no body and a masked address, and the dashboard lists the log and
// previews templates (docs/guides/delivery.md).

type deliveryRow struct {
	ID       int64   `json:"id"`
	UserID   *int64  `json:"user_id"`
	Channel  string  `json:"channel"`
	Address  string  `json:"address"`
	Template string  `json:"template"`
	Status   string  `json:"status"`
	Error    *string `json:"error"`
}

func TestAccountMailsGoThroughTheDeliveryLog(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	application := testkit.New(t)
	user := application.User(t, testkit.Email("jane.doe@acme.test"))
	viewer := application.User(t, testkit.Role(application.Role(t, "Dashboard viewer", authz.PermDevView)))

	application.Guest().Post("/api/v1/forgot-password", map[string]any{"email": "jane.doe@acme.test"}).
		Status(t, http.StatusOK)

	var rows []deliveryRow
	application.As(viewer).Get("/dev/api/deliveries?template=password-reset").Status(t, http.StatusOK).Data(t, &rows)
	if len(rows) != 1 {
		t.Fatalf("password-reset deliveries = %+v, want 1", rows)
	}
	row := rows[0]
	if row.Channel != "mail" || row.Address != "j***@acme.test" || row.UserID == nil || *row.UserID != user.ID {
		t.Fatalf("row = %+v, want a masked mail row for the user", row)
	}
	// The log stores no body: nothing in the row can carry the reset link.
	var stored string
	if err := application.Pool.QueryRow(context.Background(),
		`SELECT row_to_json(d)::text FROM deliveries d WHERE id = $1`, row.ID).Scan(&stored); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if strings.Contains(stored, "token") || strings.Contains(stored, "jane.doe") {
		t.Fatalf("delivery row leaks the address or link: %s", stored)
	}

	application.As(viewer).Get("/dev/api/deliveries/"+strconv.FormatInt(row.ID, 10)).Status(t, http.StatusOK)
	application.As(user).Get("/dev/api/deliveries").Status(t, http.StatusForbidden)
}

func TestDashboardPreviewsDeliveryTemplates(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	application := testkit.New(t)
	viewer := application.User(t, testkit.Role(application.Role(t, "Dashboard viewer", authz.PermDevView)))

	var templates struct {
		Templates []struct {
			Name     string   `json:"name"`
			Channels []string `json:"channels"`
		} `json:"templates"`
		Channels []struct {
			Channel string `json:"channel"`
			Driver  string `json:"driver"`
		} `json:"channels"`
	}
	application.As(viewer).Get("/dev/api/deliveries/templates").Status(t, http.StatusOK).Data(t, &templates)
	names := map[string]bool{}
	for _, template := range templates.Templates {
		names[template.Name] = true
	}
	if !names["otp-code"] || !names["password-reset"] || !names["email-verify"] || len(templates.Channels) != 3 {
		t.Fatalf("templates = %+v", templates)
	}

	var content struct {
		Subject string `json:"subject"`
		Text    string `json:"text"`
		HTML    string `json:"html"`
	}
	application.As(viewer).Get("/dev/api/deliveries/templates/otp-code/preview?channel=mail&locale=bn").
		Status(t, http.StatusOK).Data(t, &content)
	if !strings.Contains(content.Text, "482913") || !strings.Contains(content.Text, "কোড") || content.HTML == "" {
		t.Fatalf("preview = %+v, want the Bangla sample", content)
	}
	application.As(viewer).Get("/dev/api/deliveries/templates/nope/preview").Status(t, http.StatusNotFound)
	application.As(viewer).Get("/dev/api/deliveries/templates/otp-code/preview?channel=fax").Status(t, http.StatusUnprocessableEntity)
}

func TestDeliveryTestMessageSendsNow(t *testing.T) {
	application := testkit.New(t)
	service := application.Deps.Platform.Delivery

	message, err := app.DeliveryTestMessage(service, delivery.Mail, "ops@acme.test", "", "")
	if err != nil {
		t.Fatalf("DeliveryTestMessage() error = %v", err)
	}
	if err := service.SendNow(context.Background(), message); err != nil {
		t.Fatalf("SendNow() error = %v", err)
	}
	records, err := service.List(context.Background(), delivery.ListFilter{Template: delivery.RawTemplate})
	if err != nil || len(records) != 1 || records[0].Status != delivery.StatusSent {
		t.Fatalf("records = %+v, %v; want one sent raw mail", records, err)
	}

	if _, err := app.DeliveryTestMessage(service, delivery.Push, "not-a-user", "", ""); err == nil {
		t.Fatal("push to a non-numeric user id was accepted")
	}
	if _, err := app.DeliveryTestMessage(service, delivery.Mail, "ops@acme.test", "nope", ""); err == nil {
		t.Fatal("an unknown template was accepted")
	}
}
