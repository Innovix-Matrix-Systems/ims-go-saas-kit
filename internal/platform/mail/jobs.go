package mail

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
)

// KindSend is the mail delivery job kind.
const KindSend = "mail.send"

type sendPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// RegisterJob adds the mail.send job definition backed by the driver.
func RegisterJob(registry *queue.Registry, driver Driver) error {
	return registry.Register(queue.Definition{
		Kind:        KindSend,
		Description: "Delivers an email through the configured mail driver.",
		Queue:       queue.QueueDefault,
		Timeout:     time.Minute,
		SystemJob:   true,
		Validate:    validateSendPayload,
		Handle: func(ctx context.Context, payload []byte) error {
			var input sendPayload
			if err := json.Unmarshal(payload, &input); err != nil {
				return err
			}
			return driver.Send(ctx, Message{To: input.To, Subject: input.Subject, Text: input.Text})
		},
	})
}

// Enqueue submits a mail for background delivery.
func Enqueue(ctx context.Context, engine queue.Engine, message Message) error {
	return engine.Enqueue(ctx, KindSend, sendPayload{
		To:      message.To,
		Subject: message.Subject,
		Text:    message.Text,
	}, queue.EnqueueOptions{})
}

// EngineEnqueuer adapts a queue engine to interfaces that enqueue plain mail
// (e.g. the authn OTP service).
type EngineEnqueuer struct {
	Engine queue.Engine
}

// EnqueueMail implements the authn mail enqueuer contract.
func (e EngineEnqueuer) EnqueueMail(ctx context.Context, to, subject, text string) error {
	return Enqueue(ctx, e.Engine, Message{To: to, Subject: subject, Text: text})
}

func validateSendPayload(payload []byte) error {
	var input sendPayload
	if err := json.Unmarshal(payload, &input); err != nil {
		return apperr.Wrap(apperr.InvalidInput, "Invalid job payload.", err)
	}
	if input.To == "" || input.Subject == "" {
		return errors.New("mail.send payload requires to and subject")
	}
	return nil
}
