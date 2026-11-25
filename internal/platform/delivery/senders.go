package delivery

import (
	"context"
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/mail"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/sms"
)

// MailSender sends the mail channel through a mail driver (log or smtp).
type MailSender struct{ Driver mail.Driver }

// Name implements Sender.
func (m MailSender) Name() string { return m.Driver.Name() }

// Send implements Sender.
func (m MailSender) Send(ctx context.Context, message Message, content Content) (string, error) {
	attachments := make([]mail.Attachment, 0, len(content.Files))
	for _, file := range content.Files {
		attachments = append(attachments, mail.Attachment{Name: file.Name, ContentType: file.ContentType, Data: file.Data})
	}
	return "", m.Driver.Send(ctx, mail.Message{
		To:          content.To,
		CC:          message.CC,
		BCC:         message.BCC,
		ReplyTo:     message.ReplyTo,
		Subject:     content.Subject,
		Text:        content.Text,
		HTML:        content.HTML,
		Attachments: attachments,
		Headers:     message.Headers,
	})
}

// SMSSender sends the sms channel through an SMS driver (log or twilio).
type SMSSender struct{ Driver sms.Driver }

// Name implements Sender.
func (s SMSSender) Name() string { return s.Driver.Name() }

// Send implements Sender.
func (s SMSSender) Send(ctx context.Context, _ Message, content Content) (string, error) {
	return s.Driver.Send(ctx, sms.Message{To: content.To, Body: content.Text})
}

// LogPushSender is the push channel until a provider is added: it logs the
// title and, with debug on outside production, the body.
type LogPushSender struct {
	Logger *slog.Logger
	Debug  bool
}

// Name implements Sender.
func (LogPushSender) Name() string { return "log" }

// Send implements Sender.
func (p LogPushSender) Send(_ context.Context, message Message, content Content) (string, error) {
	attrs := []any{"user_id", message.To.UserID, "title", content.Title}
	if p.Debug {
		attrs = append(attrs, "body", content.Text)
	}
	p.Logger.Info("push delivered (log driver)", attrs...)
	return "", nil
}
