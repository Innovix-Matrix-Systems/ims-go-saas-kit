// Package mail is the mail transport: the driver abstraction (log and smtp)
// behind the delivery system's mail channel (internal/platform/delivery).
// The mail.send queue job and Enqueue remain for code written before the
// delivery system; new code sends through delivery.Service.
package mail

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
)

// Message is one outbound mail. Text is always sent; with HTML the mail is
// multipart/alternative (text + HTML), and attachments make it
// multipart/mixed. BCC recipients receive it without appearing in a header.
type Message struct {
	To          string
	CC          []string
	BCC         []string
	ReplyTo     string
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
	// Headers are extra headers (List-Unsubscribe); values are sanitized.
	Headers map[string]string
}

// Attachment is a file attached to a mail. Callers read it (usually from a
// storage disk) just before sending; queue payloads carry a reference, not
// the bytes.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Recipients returns every envelope recipient: To, CC and BCC.
func (m Message) Recipients() []string {
	all := make([]string, 0, 1+len(m.CC)+len(m.BCC))
	all = append(all, m.To)
	all = append(all, m.CC...)
	return append(all, m.BCC...)
}

// Driver delivers mail messages.
type Driver interface {
	Send(ctx context.Context, message Message) error
	Name() string
}

// New selects the configured mail driver.
func New(cfg config.Mail, logger *slog.Logger, debug bool) (Driver, error) {
	switch cfg.Driver {
	case "log", "":
		return &LogDriver{logger: logger, debug: debug, from: cfg.FromAddress}, nil
	case "smtp":
		return NewSMTPDriver(cfg), nil
	default:
		return nil, fmt.Errorf("mail: unsupported driver %q", cfg.Driver)
	}
}
