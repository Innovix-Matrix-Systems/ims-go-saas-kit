package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	billingservice "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/pubsub"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// Payment notification events.
const (
	eventPaymentToReview = "billing.payment_to_review"
	eventPaymentPending  = "billing.payment_submitted"
	eventPaymentReceived = "billing.payment_received"
	eventPaymentRejected = "billing.payment_rejected"
)

// billingRealtimeKind is the live-update event the app answers by refetching
// billing (plan, payments, the console's approvals).
const billingRealtimeKind = "billing"

// receiptDisk holds the invoice PDFs attached to receipt mails.
const receiptDisk = "private"

// billingNotices implements billing's PaymentNotifier: who hears about a
// payment, by in-app notification, mail and live update.
//
//   - submitted (bank transfer, cash): staff who can review payments get "a
//     payment to review"; the organization's billing managers and the
//     submitter get "submitted, waiting for confirmation".
//   - received (approved, or a gateway payment): billing managers and the
//     submitter get an in-app notice and a receipt mail with the invoice PDF.
//   - rejected: the same people, with the reviewer's note.
//
// Every step logs and carries on: a payment never fails because a notice
// could not go out.
type billingNotices struct {
	notifications *notifications.Service
	invoices      *billingservice.InvoiceService
	delivery      *delivery.Service
	disks         *storage.Registry
	queries       *dbgen.Queries
	publisher     pubsub.Publisher
	appURL        string
	logger        *slog.Logger
}

func newBillingNotices(n billingNotices) *billingNotices {
	notifications.RegisterEvent(eventPaymentToReview, notifications.Event{
		Description: "An organization submitted a payment that needs approval (staff).",
		Channels:    []delivery.Channel{delivery.Mail},
	})
	notifications.RegisterEvent(eventPaymentPending, notifications.Event{
		Description: "A payment was submitted and waits for confirmation.",
		Channels:    []delivery.Channel{delivery.Mail},
	})
	// The receipt mail (with the invoice PDF) is always sent; the in-app
	// notice has no other channel.
	notifications.RegisterEvent(eventPaymentReceived, notifications.Event{
		Description: "A payment was received and the plan is active.",
		Mandatory:   true,
	})
	notifications.RegisterEvent(eventPaymentRejected, notifications.Event{
		Description: "A submitted payment was rejected.",
		Channels:    []delivery.Channel{delivery.Mail},
		Mandatory:   true,
	})
	return &n
}

// PaymentSubmitted implements billingservice.PaymentNotifier.
func (b *billingNotices) PaymentSubmitted(ctx context.Context, notice billingservice.PaymentNotice) {
	invoice, ok := b.invoice(ctx, notice)
	if !ok {
		return
	}
	amount := notifications.Money(invoice.Money)

	staff := b.recipients(ctx, 0, "payment_approval.review", nil)
	if len(staff) > 0 {
		b.notify(ctx, nil, staff, notifications.CreateInput{
			EventType: eventPaymentToReview,
			Priority:  notifications.PriorityWarning,
			Message: &notifications.Message{Key: "notices.payment_to_review", Params: map[string]any{
				"Organization": invoice.TenantName, "Amount": amount, "Plan": invoice.PackageName,
				"Method": notifications.Term(paymentMethodKey(invoice.PaymentMethod), invoice.PaymentMethod),
			}},
			Channels: []string{string(delivery.Mail)},
		}, notice, "/console/approvals")
	}

	members := b.recipients(ctx, notice.TenantID, authz.PermBillingManage, notice.SubmittedBy)
	b.notify(ctx, &notice.TenantID, members, notifications.CreateInput{
		EventType: eventPaymentPending,
		Priority:  notifications.PriorityInfo,
		Message: &notifications.Message{Key: "notices.payment_submitted", Params: map[string]any{
			"Amount": amount, "Plan": invoice.PackageName,
		}},
		Channels: []string{string(delivery.Mail)},
	}, notice, "/org/billing")
	b.publish(ctx, notice.TenantID)
}

// PaymentReceived implements billingservice.PaymentNotifier.
func (b *billingNotices) PaymentReceived(ctx context.Context, notice billingservice.PaymentNotice) {
	invoice, ok := b.invoice(ctx, notice)
	if !ok {
		return
	}
	amount := notifications.Money(invoice.Money)
	members := b.recipients(ctx, notice.TenantID, authz.PermBillingManage, notice.SubmittedBy)
	b.notify(ctx, &notice.TenantID, members, notifications.CreateInput{
		EventType: eventPaymentReceived,
		Priority:  notifications.PriorityInfo,
		Message: &notifications.Message{Key: "notices.payment_received", Params: map[string]any{
			"Amount": amount, "Plan": invoice.PackageName,
		}},
	}, notice, "/org/billing")
	b.sendReceipts(ctx, invoice, members, notice)
	b.publish(ctx, notice.TenantID)
}

// PaymentRejected implements billingservice.PaymentNotifier.
func (b *billingNotices) PaymentRejected(ctx context.Context, notice billingservice.PaymentNotice) {
	invoice, ok := b.invoice(ctx, notice)
	if !ok {
		return
	}
	message := notifications.Message{Key: "notices.payment_rejected", Params: map[string]any{
		"Amount": notifications.Money(invoice.Money), "Plan": invoice.PackageName,
	}}
	if notice.Note != nil && strings.TrimSpace(*notice.Note) != "" {
		message.Key = "notices.payment_rejected_with_reason"
		message.Params["Reason"] = strings.TrimSpace(*notice.Note)
	}
	members := b.recipients(ctx, notice.TenantID, authz.PermBillingManage, notice.SubmittedBy)
	b.notify(ctx, &notice.TenantID, members, notifications.CreateInput{
		EventType: eventPaymentRejected,
		Priority:  notifications.PriorityWarning,
		Message:   &message,
		Channels:  []string{string(delivery.Mail)},
	}, notice, "/org/billing")
	b.publish(ctx, notice.TenantID)
}

// PaymentWithdrawn implements billingservice.PaymentNotifier: nobody needs
// a message, but open billing and approval screens refresh.
func (b *billingNotices) PaymentWithdrawn(ctx context.Context, notice billingservice.PaymentNotice) {
	b.publish(ctx, notice.TenantID)
}

func (b *billingNotices) invoice(ctx context.Context, notice billingservice.PaymentNotice) (*billingdomain.Invoice, bool) {
	invoice, err := b.invoices.For(ctx, notice.TenantID, notice.PaymentID)
	if err != nil {
		b.logger.WarnContext(ctx, "billing: payment notice skipped, invoice unreadable", "payment_id", notice.PaymentID, "error", err)
		return nil, false
	}
	if invoice.PackageName == "" {
		invoice.PackageName = "subscription"
	}
	return invoice, true
}

// recipients lists the active accounts of a tenant (0: platform staff) that
// hold permission, plus the submitter when given.
func (b *billingNotices) recipients(ctx context.Context, tenantID int64, permission string, submitter *int64) []dbgen.ListUsersWithPermissionRow {
	rows, err := b.queries.ListUsersWithPermission(ctx, dbgen.ListUsersWithPermissionParams{
		TenantID: tenantID, Permission: permission, SuperAdminRole: authz.SuperAdminRole,
	})
	if err != nil {
		b.logger.WarnContext(ctx, "billing: payment notice recipients unreadable", "tenant_id", tenantID, "error", err)
		return nil
	}
	if submitter != nil && !slices.ContainsFunc(rows, func(row dbgen.ListUsersWithPermissionRow) bool { return row.ID == *submitter }) {
		if user, err := b.queries.GetUser(ctx, dbgen.GetUserParams{ID: *submitter, AllTenants: true}); err == nil && !user.DeletedAt.Valid && user.IsActive == 1 {
			rows = append(rows, dbgen.ListUsersWithPermissionRow{ID: user.ID, TenantID: user.TenantID, Name: user.Name, Email: user.Email, Locale: user.Locale})
		}
	}
	return rows
}

// notify creates one notification addressed to the listed users (the
// fan-out job then delivers it, mails included, and nudges their apps).
func (b *billingNotices) notify(ctx context.Context, tenantID *int64, users []dbgen.ListUsersWithPermissionRow, input notifications.CreateInput, notice billingservice.PaymentNotice, path string) {
	if len(users) == 0 {
		return
	}
	data, _ := json.Marshal(map[string]any{"payment_id": notice.PaymentID, "tenant_id": notice.TenantID, "url": path})
	input.TenantID = tenantID
	input.Data = data
	for _, user := range users {
		id := user.ID
		input.Targets = append(input.Targets, notifications.TargetInput{Type: notifications.TargetUser, ID: &id})
	}
	if _, _, err := b.notifications.Create(ctx, tenancy.Scope{Internal: true}, 0, input); err != nil {
		b.logger.WarnContext(ctx, "billing: payment notification not created", "event", input.EventType, "payment_id", notice.PaymentID, "error", err)
	}
}

// sendReceipts mails the receipt with the invoice PDF to each recipient.
func (b *billingNotices) sendReceipts(ctx context.Context, invoice *billingdomain.Invoice, users []dbgen.ListUsersWithPermissionRow, notice billingservice.PaymentNotice) {
	if b.delivery == nil || len(users) == 0 {
		return
	}
	pdf, err := billingservice.RenderInvoicePDF(invoice)
	if err != nil {
		b.logger.WarnContext(ctx, "billing: receipt not sent, invoice pdf failed", "payment_id", notice.PaymentID, "error", err)
		return
	}
	disk, err := b.disks.Disk(receiptDisk)
	if err != nil {
		b.logger.WarnContext(ctx, "billing: receipt not sent, no private disk", "error", err)
		return
	}
	name := "invoice-" + invoice.Number + ".pdf"
	path := fmt.Sprintf("billing/invoices/%d/%s", notice.TenantID, name)
	if err := disk.Put(ctx, path, bytes.NewReader(pdf), int64(len(pdf))); err != nil {
		b.logger.WarnContext(ctx, "billing: receipt not sent, invoice pdf not stored", "payment_id", notice.PaymentID, "error", err)
		return
	}
	paidOn := "-"
	if invoice.PaidAt != nil {
		paidOn = invoice.PaidAt.Format("2 January 2006")
	}
	for _, user := range users {
		locale := ""
		if user.Locale != nil {
			locale = *user.Locale
		}
		to := delivery.Recipient{UserID: user.ID, TenantID: notice.TenantID, Name: user.Name, Email: user.Email, Locale: locale}
		// The amount in the recipient's digits, like the rest of the mail.
		amount := invoice.Money.Format(i18n.LanguageFor(b.delivery.LocaleFor(ctx, to)))
		err := b.delivery.Send(ctx, delivery.Message{
			To:       to,
			Template: "payment-receipt",
			Data: map[string]any{
				"amount": amount, "organization": invoice.TenantName,
				"plan": invoice.PackageName, "invoice": invoice.Number, "paid_on": paidOn,
				"url": b.appURL + "/org/billing",
			},
			Channels:    []delivery.Channel{delivery.Mail},
			Attachments: []delivery.Attachment{{Disk: receiptDisk, Path: path, Name: name, ContentType: "application/pdf"}},
		})
		if err != nil {
			b.logger.WarnContext(ctx, "billing: receipt not sent", "payment_id", notice.PaymentID, "user_id", user.ID, "error", err)
		}
	}
}

// publish refreshes the organization's billing screens and the staff's
// approval list.
func (b *billingNotices) publish(ctx context.Context, tenantID int64) {
	if b.publisher == nil {
		return
	}
	event := pubsub.Event{Kind: billingRealtimeKind, TenantIDs: []int64{tenantID}, Platform: true}
	if err := b.publisher.Publish(ctx, event); err != nil {
		b.logger.WarnContext(ctx, "billing: live update not published", "tenant_id", tenantID, "error", err)
	}
}

// paymentMethodKey is the catalog key of a payment method's name
// (notices.method.bank_transfer).
func paymentMethodKey(method string) string { return "notices.method." + method }
