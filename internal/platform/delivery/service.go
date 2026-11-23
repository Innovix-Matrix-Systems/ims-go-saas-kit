package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/crypto"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/i18n"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
)

// KindSend is the delivery job kind: one message on one channel.
const KindSend = "delivery.send"

// BrandSource gives the brand a tenant's messages show (tenant 0: the
// platform's own).
type BrandSource interface {
	Brand(ctx context.Context, tenantID int64) Brand
}

// StaticBrand is a BrandSource that always returns the same brand.
type StaticBrand Brand

// Brand implements BrandSource.
func (b StaticBrand) Brand(context.Context, int64) Brand { return Brand(b) }

// AttachmentOpener opens an attachment on a storage disk.
type AttachmentOpener interface {
	Open(ctx context.Context, disk, path string) (io.ReadCloser, error)
}

// File is an attachment read for sending.
type File struct {
	Name        string
	ContentType string
	Data        []byte
}

// Service sends messages. Build it once (app.NewPlatform) and pass it to
// whatever needs to reach a person.
type Service struct {
	templates      *Registry
	repo           Repository
	queue          queue.Engine
	senders        map[Channel]Sender
	brand          BrandSource
	attachments    AttachmentOpener
	encrypter      *crypto.Encrypter
	maxAttachBytes int64
	fallback       string
	logger         *slog.Logger
	now            func() time.Time
}

// NewService creates the delivery service. Add a Sender per channel with
// WithSender; a channel without one is refused at Send.
func NewService(templates *Registry, repo Repository, engine queue.Engine, logger *slog.Logger) *Service {
	return &Service{
		templates:      templates,
		repo:           repo,
		queue:          engine,
		senders:        map[Channel]Sender{},
		brand:          StaticBrand{Color: "#2563eb"},
		maxAttachBytes: 10 << 20,
		fallback:       "en",
		logger:         logger,
		now:            time.Now,
	}
}

// WithSender sets the sender of a channel.
func (s *Service) WithSender(channel Channel, sender Sender) *Service {
	s.senders[channel] = sender
	return s
}

// WithBrand sets where the mail layout's brand comes from.
func (s *Service) WithBrand(source BrandSource) *Service {
	s.brand = source
	return s
}

// WithAttachments lets mail carry attachments from storage disks, up to
// maxBytes in total per mail.
func (s *Service) WithAttachments(opener AttachmentOpener, maxBytes int64) *Service {
	s.attachments = opener
	s.maxAttachBytes = maxBytes
	return s
}

// WithEncrypter seals the data of Sensitive messages (one-time codes, links
// that sign someone in) in the queue payload, so they are not readable in
// the jobs table.
func (s *Service) WithEncrypter(encrypter *crypto.Encrypter) *Service {
	s.encrypter = encrypter
	return s
}

// WithFallbackLocale sets the locale used when a recipient has none and the
// context carries none (APP_FALLBACK_LOCALE).
func (s *Service) WithFallbackLocale(locale string) *Service {
	if locale != "" {
		s.fallback = locale
	}
	return s
}

// Templates returns the template registry (modules register theirs on it).
func (s *Service) Templates() *Registry { return s.templates }

// Channels lists the channels that have a sender.
func (s *Service) Channels() []Channel {
	var channels []Channel
	for _, channel := range Channels {
		if _, ok := s.senders[channel]; ok {
			channels = append(channels, channel)
		}
	}
	return channels
}

// Driver names the driver behind a channel ("" when it has none).
func (s *Service) Driver(channel Channel) string {
	if sender, ok := s.senders[channel]; ok {
		return sender.Name()
	}
	return ""
}

// job is the delivery.send payload: one message, one channel, one log row.
// A Sensitive message's data travels sealed (SealedData) instead of in
// Message.Data.
type job struct {
	DeliveryID int64   `json:"delivery_id"`
	Channel    Channel `json:"channel"`
	Message    Message `json:"message"`
	SealedData string  `json:"sealed_data,omitempty"`
}

// RegisterJob adds the delivery.send job.
func (s *Service) RegisterJob(registry *queue.Registry) error {
	return registry.Register(queue.Definition{
		Kind:        KindSend,
		Description: "Delivers one message on one channel (mail, SMS or push) and records the result.",
		Queue:       queue.QueueDefault,
		Timeout:     2 * time.Minute,
		// The payload names its own recipient: no tenant scope is needed.
		SystemJob: true,
		Validate: func(payload []byte) error {
			var input job
			if err := json.Unmarshal(payload, &input); err != nil {
				return apperr.Wrap(apperr.InvalidInput, "Invalid job payload.", err)
			}
			if input.DeliveryID <= 0 || input.Channel == "" {
				return errors.New("delivery.send payload requires delivery_id and channel")
			}
			return nil
		},
		Handle: func(ctx context.Context, payload []byte) error {
			var input job
			if err := json.Unmarshal(payload, &input); err != nil {
				return err
			}
			message, err := s.unseal(input)
			if err != nil {
				_ = s.repo.MarkFailed(ctx, input.DeliveryID, err.Error())
				return err
			}
			return s.deliver(ctx, input.DeliveryID, input.Channel, message)
		},
	})
}

// Send queues a message on each of its channels. It renders every channel
// first, so a missing template, file or data key is an error here rather
// than a failed job. A channel the recipient has no address for is logged
// as skipped and not queued.
func (s *Service) Send(ctx context.Context, message Message) error {
	return s.dispatch(ctx, message, false)
}

// SendNow delivers a message on each of its channels before returning (the
// CLI's delivery:test). Everything else uses Send.
func (s *Service) SendNow(ctx context.Context, message Message) error {
	return s.dispatch(ctx, message, true)
}

func (s *Service) dispatch(ctx context.Context, message Message, now bool) error {
	if message.To.Locale == "" {
		message.To.Locale = string(i18n.FromContext(ctx))
	}
	if message.Template != "" && !s.templates.Has(message.Template) {
		return apperr.Newf(apperr.InternalServerError, "Delivery template %q is not registered.", message.Template)
	}
	for _, channel := range message.channels() {
		if _, ok := s.senders[channel]; !ok {
			return apperr.Newf(apperr.InternalServerError, "Delivery channel %q has no sender.", channel)
		}
		if _, err := s.render(ctx, channel, message); err != nil {
			return apperr.Wrap(apperr.InternalServerError, "The message could not be rendered.", err)
		}
	}
	var failed error
	for _, channel := range message.channels() {
		if err := s.queueOne(ctx, channel, message, now); err != nil {
			failed = errors.Join(failed, err)
		}
	}
	if failed != nil {
		return apperr.Wrap(apperr.ServiceUnavailable, "The message could not be sent.", failed)
	}
	return nil
}

func (s *Service) queueOne(ctx context.Context, channel Channel, message Message, now bool) error {
	address := message.To.address(channel)
	entry := Entry{
		TenantID: message.To.TenantID,
		UserID:   message.To.UserID,
		Channel:  channel,
		Address:  MaskAddress(channel, address),
		Template: message.templateName(),
		Status:   StatusQueued,
	}
	if address == "" {
		entry.Status, entry.Error = StatusSkipped, "the recipient has no address for this channel"
		_, err := s.repo.Create(ctx, entry)
		return err
	}
	id, err := s.repo.Create(ctx, entry)
	if err != nil {
		return err
	}
	if now {
		return s.deliver(ctx, id, channel, message)
	}
	payload, err := s.seal(job{DeliveryID: id, Channel: channel, Message: message})
	if err != nil {
		_ = s.repo.MarkFailed(ctx, id, err.Error())
		return err
	}
	if err := s.queue.Enqueue(ctx, KindSend, payload, queue.EnqueueOptions{}); err != nil {
		// The sync engine runs the job inside Enqueue and the job has
		// recorded its own failure; only a job that never ran is marked here.
		if record, getErr := s.repo.Get(ctx, id); getErr == nil && record.Status == StatusQueued {
			_ = s.repo.MarkFailed(ctx, id, "enqueue: "+err.Error())
		}
		return err
	}
	return nil
}

// deliver renders and sends one channel and records the outcome. A returned
// error makes the queue retry the job.
func (s *Service) deliver(ctx context.Context, id int64, channel Channel, message Message) error {
	sender, ok := s.senders[channel]
	if !ok {
		err := fmt.Errorf("delivery: channel %q has no sender", channel)
		_ = s.repo.MarkFailed(ctx, id, err.Error())
		return err
	}
	content, err := s.render(ctx, channel, message)
	if err != nil {
		_ = s.repo.MarkFailed(ctx, id, err.Error())
		return err
	}
	if channel == Mail && len(message.Attachments) > 0 {
		if content.Files, err = s.readAttachments(ctx, message.Attachments); err != nil {
			_ = s.repo.MarkFailed(ctx, id, err.Error())
			return err
		}
	}
	providerID, err := sender.Send(ctx, message, content)
	if err != nil {
		_ = s.repo.MarkFailed(ctx, id, err.Error())
		return err
	}
	return s.repo.MarkSent(ctx, id, providerID)
}

// render produces a channel's content: from the template, or from the raw
// Subject/Text/HTML.
func (s *Service) render(ctx context.Context, channel Channel, message Message) (Content, error) {
	brand := s.brand.Brand(ctx, message.To.TenantID)
	var (
		content Content
		err     error
	)
	if message.Template != "" {
		view := View{To: message.To, Data: message.Data, Brand: brand}
		if view.Data == nil {
			view.Data = map[string]any{}
		}
		content, err = s.templates.Render(message.Template, channel, []string{message.To.Locale, s.fallback}, view)
		if err != nil {
			return Content{}, err
		}
	} else {
		if message.Text == "" || (channel == Mail && message.Subject == "") {
			return Content{}, errors.New("delivery: a message without a template needs text (and a subject for mail)")
		}
		content = Content{Channel: channel, Subject: message.Subject, Text: message.Text}
		if channel == Push {
			content.Title, content.Subject = message.Subject, ""
		}
		if channel == Mail && message.HTML != "" {
			if content.HTML, err = s.templates.Layout(brand, message.Subject, trustedHTML(message.HTML)); err != nil {
				return Content{}, err
			}
		}
	}
	content.To = message.To.address(channel)
	return content, nil
}

func (s *Service) readAttachments(ctx context.Context, attachments []Attachment) ([]File, error) {
	if s.attachments == nil {
		return nil, errors.New("delivery: attachments are not configured")
	}
	files := make([]File, 0, len(attachments))
	var total int64
	for _, attachment := range attachments {
		reader, err := s.attachments.Open(ctx, attachment.Disk, attachment.Path)
		if err != nil {
			return nil, fmt.Errorf("delivery: open attachment %q: %w", attachment.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(reader, s.maxAttachBytes-total+1))
		_ = reader.Close()
		if err != nil {
			return nil, fmt.Errorf("delivery: read attachment %q: %w", attachment.Name, err)
		}
		total += int64(len(data))
		if total > s.maxAttachBytes {
			return nil, fmt.Errorf("delivery: attachments exceed %d MB", s.maxAttachBytes>>20)
		}
		files = append(files, File{Name: attachment.Name, ContentType: attachment.ContentType, Data: data})
	}
	return files, nil
}

// Preview renders a template for a channel without sending it. Data nil
// uses the template's sample.json.
func (s *Service) Preview(ctx context.Context, name string, channel Channel, locale string, data map[string]any) (Content, error) {
	if !s.templates.Has(name) {
		return Content{}, apperr.Newf(apperr.ResourceNotFound, "Delivery template %q was not found.", name)
	}
	if data == nil {
		sample, err := s.templates.Sample(name)
		if err != nil {
			return Content{}, apperr.Wrap(apperr.InternalServerError, "The template's sample data could not be read.", err)
		}
		data = sample
	}
	message := Message{
		To:       Recipient{Name: "Sample User", Email: "sample@example.com", Phone: "+8801700000000", UserID: 1, Locale: locale},
		Template: name, Data: data, Channels: []Channel{channel},
	}
	content, err := s.render(ctx, channel, message)
	if err != nil {
		return Content{}, apperr.Wrap(apperr.BusinessLogicError, err.Error(), err)
	}
	return content, nil
}

// List returns delivery log rows, newest first.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]Record, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	return s.repo.List(ctx, filter)
}

// Get returns one delivery log row.
func (s *Service) Get(ctx context.Context, id int64) (*Record, error) { return s.repo.Get(ctx, id) }

// Prune deletes delivery log rows older than days (0 keeps everything).
func (s *Service) Prune(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	return s.repo.Prune(ctx, s.now().AddDate(0, 0, -days))
}

// seal moves a Sensitive message's data into SealedData (with an encrypter).
func (s *Service) seal(payload job) (job, error) {
	if !payload.Message.Sensitive || s.encrypter == nil || len(payload.Message.Data) == 0 {
		return payload, nil
	}
	raw, err := json.Marshal(payload.Message.Data)
	if err != nil {
		return job{}, fmt.Errorf("delivery: encode data: %w", err)
	}
	if payload.SealedData, err = s.encrypter.Encrypt(raw); err != nil {
		return job{}, err
	}
	payload.Message.Data = nil
	return payload, nil
}

// unseal restores the data of a sealed payload.
func (s *Service) unseal(payload job) (Message, error) {
	message := payload.Message
	if payload.SealedData == "" {
		return message, nil
	}
	if s.encrypter == nil {
		return Message{}, errors.New("delivery: the payload is sealed but no encrypter is configured")
	}
	raw, err := s.encrypter.Decrypt(payload.SealedData)
	if err != nil {
		return Message{}, fmt.Errorf("delivery: unseal data: %w", err)
	}
	if err := json.Unmarshal(raw, &message.Data); err != nil {
		return Message{}, fmt.Errorf("delivery: decode data: %w", err)
	}
	return message, nil
}
