// Package delivery is the one way the application reaches a person: a
// message, rendered from a template (or given raw), sent on one or more
// channels (mail, SMS, push). Every feature that contacts a user — one-time
// codes, recovery links, invitations, notifications — calls Service.Send;
// the channel drivers (internal/platform/mail, internal/platform/sms) are
// transports behind it.
//
// Send renders once to validate, writes one row per channel to the delivery
// log (no body, no data, a masked address) and queues one delivery.send job
// per channel, so a slow SMS provider never delays the mail and each channel
// retries on its own. The job renders again when it runs, so a template fix
// applies to retries.
package delivery

import (
	"context"
	"strconv"
	"time"
)

// Channel is a way to reach a person.
type Channel string

// Channels.
const (
	Mail Channel = "mail"
	SMS  Channel = "sms"
	// Push is reserved for push notifications: the channel exists with a log
	// sender until a provider (FCM, APNs) is added as a Sender.
	Push Channel = "push"
)

// Channels lists every channel in a stable order.
var Channels = []Channel{Mail, SMS, Push}

// Delivery statuses.
const (
	StatusQueued  = "queued"
	StatusSent    = "sent"
	StatusFailed  = "failed"
	StatusSkipped = "skipped" // no address for the channel; nothing was queued
)

// Recipient is the person a message goes to. Missing addresses skip their
// channel. Locale picks the template variant ("bn" → mail.bn.txt); empty
// uses the request's locale, then the fallback locale.
type Recipient struct {
	UserID   int64  `json:"user_id,omitempty"`
	TenantID int64  `json:"tenant_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Locale   string `json:"locale,omitempty"`
}

// Message is one thing to send. Give a Template (with Data), or a raw
// Subject + Text (and optional HTML) for a message without a template.
type Message struct {
	To       Recipient      `json:"to"`
	Template string         `json:"template,omitempty"`
	Data     map[string]any `json:"data,omitempty"`

	// Raw message (no Template): Subject and Text for mail, Text for SMS and
	// push; HTML is optional and wrapped in the mail layout.
	Subject string `json:"subject,omitempty"`
	Text    string `json:"text,omitempty"`
	HTML    string `json:"html,omitempty"`

	// Channels to send on; empty means mail.
	Channels []Channel `json:"channels,omitempty"`

	// Mail only.
	CC          []string     `json:"cc,omitempty"`
	BCC         []string     `json:"bcc,omitempty"`
	ReplyTo     string       `json:"reply_to,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	// Headers are extra mail headers (List-Unsubscribe).
	Headers map[string]string `json:"headers,omitempty"`

	// Sensitive marks messages carrying secrets (one-time codes, links that
	// sign someone in): their rendered body is never shown in the dashboard
	// outside local development, and drivers log bodies only in debug.
	Sensitive bool `json:"sensitive,omitempty"`
}

// Attachment is a file on a storage disk, read when the mail is sent; the
// queue carries the reference, never the bytes.
type Attachment struct {
	Disk        string `json:"disk"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	ContentType string `json:"content_type,omitempty"`
}

// Content is a message rendered for one channel.
type Content struct {
	Channel Channel `json:"channel"`
	To      string  `json:"to"`
	Subject string  `json:"subject,omitempty"` // mail
	Title   string  `json:"title,omitempty"`   // push
	Text    string  `json:"text"`
	HTML    string  `json:"html,omitempty"` // mail
	// Files are the mail's attachments, read just before sending.
	Files []File `json:"-"`
}

// Sender delivers rendered content on one channel. It returns the provider's
// message id ("" when there is none).
type Sender interface {
	Send(ctx context.Context, message Message, content Content) (string, error)
	Name() string
}

// Record is one row of the delivery log.
type Record struct {
	ID                int64      `json:"id"`
	TenantID          *int64     `json:"tenant_id"`
	UserID            *int64     `json:"user_id"`
	Channel           string     `json:"channel"`
	Address           string     `json:"address"`
	Template          string     `json:"template"`
	Status            string     `json:"status"`
	Attempts          int32      `json:"attempts"`
	Error             *string    `json:"error"`
	ProviderMessageID *string    `json:"provider_message_id"`
	CreatedAt         time.Time  `json:"created_at"`
	SentAt            *time.Time `json:"sent_at"`
}

// RawTemplate is the delivery log's template name for a message without one.
const RawTemplate = "raw"

func (m Message) channels() []Channel {
	if len(m.Channels) == 0 {
		return []Channel{Mail}
	}
	return m.Channels
}

func (m Message) templateName() string {
	if m.Template == "" {
		return RawTemplate
	}
	return m.Template
}

// address is the recipient's address on a channel ("" when it has none).
func (r Recipient) address(channel Channel) string {
	switch channel {
	case Mail:
		return r.Email
	case SMS:
		return r.Phone
	case Push:
		if r.UserID > 0 {
			return "user:" + strconv.FormatInt(r.UserID, 10)
		}
	}
	return ""
}
