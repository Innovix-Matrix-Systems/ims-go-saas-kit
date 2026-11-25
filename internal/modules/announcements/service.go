package announcements

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/pubsub"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// Channels an announcement can also be delivered through.
var allowedChannels = map[string]bool{"database": true, "mail": true, "sms": true, "push": true}

// Notifier delivers an announcement to one organization's members through
// the notification system (implemented by the app's wiring).
type Notifier func(ctx context.Context, tenantID int64, announcement Announcement) error

// Service runs announcements.
type Service struct {
	repo     Repository
	tx       *database.TxManager
	activity *activity.Recorder
	notify   Notifier
	logger   *slog.Logger
	now      func() time.Time
	// publisher tells open live-update streams what changed (optional).
	publisher pubsub.Publisher
}

// NewService creates the announcement service.
func NewService(repo Repository, tx *database.TxManager, logger *slog.Logger) *Service {
	return &Service{repo: repo, tx: tx, logger: logger, now: time.Now}
}

// WithActivity records staff changes in the system audit log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// WithNotifier enables delivery through channels.
func (s *Service) WithNotifier(notify Notifier) *Service {
	s.notify = notify
	return s
}

// Input is what staff set on an announcement.
type Input struct {
	Title       string
	Body        *string
	Kind        string
	CTALabel    *string
	CTAURL      *string
	TenantIDs   []int64 // empty: every organization
	StartsAt    *time.Time
	EndsAt      *time.Time
	Dismissible bool
	Status      string
	Channels    []string
}

func clean(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// normalize checks the input and fills defaults.
func (s *Service) normalize(input Input) (Input, error) {
	fields := map[string][]string{}
	input.Title = strings.TrimSpace(input.Title)
	input.Body, input.CTALabel, input.CTAURL = clean(input.Body), clean(input.CTALabel), clean(input.CTAURL)
	if (input.CTALabel == nil) != (input.CTAURL == nil) {
		fields["cta_url"] = append(fields["cta_url"], "A button needs both a label and a link.")
	}
	if input.CTAURL != nil {
		parsed, err := url.Parse(*input.CTAURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			fields["cta_url"] = append(fields["cta_url"], "The link must be an http or https address.")
		}
	}
	if input.StartsAt == nil {
		now := s.now()
		input.StartsAt = &now
	}
	if input.EndsAt != nil && !input.EndsAt.After(*input.StartsAt) {
		fields["ends_at"] = append(fields["ends_at"], "The end must be after the start.")
	}
	seen := map[string]bool{}
	channels := []string{}
	for _, channel := range input.Channels {
		if !allowedChannels[channel] {
			fields["channels"] = append(fields["channels"], "The channels must be database, mail, sms or push.")
			break
		}
		if !seen[channel] {
			seen[channel] = true
			channels = append(channels, channel)
		}
	}
	input.Channels = channels
	if input.Status == "" {
		input.Status = StatusPublished
	}
	if len(fields) > 0 {
		return input, apperr.Validation(fields)
	}
	return input, nil
}

// Create adds an announcement and its audience.
func (s *Service) Create(ctx context.Context, actorID int64, input Input) (_ *Announcement, err error) {
	ctx, end := tracing.Begin(ctx, "announcements.create")
	defer end(&err)
	input, err = s.normalize(input)
	if err != nil {
		return nil, err
	}
	var created *Announcement
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		repo := NewRepository(q)
		if created, err = repo.Create(ctx, input, actor(actorID)); err != nil {
			return err
		}
		return repo.SetAudience(ctx, created.ID, input.TenantIDs)
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, "announcement.created", *created)
	s.publishAudiences(ctx, s.liveAudience(ctx, created))
	return created, nil
}

// Update replaces an announcement's content, window and audience.
func (s *Service) Update(ctx context.Context, actorID, id int64, input Input) (_ *Announcement, err error) {
	ctx, end := tracing.Begin(ctx, "announcements.update", attribute.Int64("announcement.id", id))
	defer end(&err)
	input, err = s.normalize(input)
	if err != nil {
		return nil, err
	}
	// Who saw it before the change, for the live update below.
	var seenBy *pubsub.Event
	if s.publisher != nil {
		before, err := s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		seenBy = s.liveAudience(ctx, before)
	}
	var updated *Announcement
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		repo := NewRepository(q)
		if updated, err = repo.Update(ctx, id, input, actor(actorID)); err != nil {
			return err
		}
		return repo.SetAudience(ctx, id, input.TenantIDs)
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, "announcement.updated", *updated)
	s.publishAudiences(ctx, seenBy, s.liveAudience(ctx, updated))
	return updated, nil
}

// Delete removes an announcement with its audience and counts.
func (s *Service) Delete(ctx context.Context, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "announcements.delete", attribute.Int64("announcement.id", id))
	defer end(&err)
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	// Its audience, read before the delete takes the tenant list with it.
	seenBy := s.liveAudience(ctx, current)
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.record(ctx, "announcement.deleted", *current)
	s.publishAudiences(ctx, seenBy)
	return nil
}

// Get loads one announcement.
func (s *Service) Get(ctx context.Context, id int64) (*Announcement, error) {
	return s.repo.Get(ctx, id)
}

// List returns announcements newest first, optionally of one status.
func (s *Service) List(ctx context.Context, status string, limit, offset int32) ([]Listed, int64, error) {
	filter := optional(status)
	items, err := s.repo.List(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Detail loads an announcement for staff.
func (s *Service) Detail(ctx context.Context, id int64) (*Detail, error) {
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	tenants, err := s.repo.Audience(ctx, id)
	if err != nil {
		return nil, err
	}
	totals, err := s.repo.Totals(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Detail{Announcement: *current, Tenants: tenants, Totals: totals}, nil
}

// TenantTotalsPage is one page of the organizations whose members reacted to
// an announcement, most views first, optionally matching a name.
func (s *Service) TenantTotalsPage(ctx context.Context, id int64, search string, limit, offset int32) ([]TenantTotals, int64, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, 0, err
	}
	filter := optional(search)
	items, err := s.repo.TenantTotals(ctx, id, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountTenantTotals(ctx, id, filter)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Live lists what a member of the tenant should see now, without what they
// dismissed.
func (s *Service) Live(ctx context.Context, tenantID, userID int64) ([]Announcement, error) {
	return s.repo.Live(ctx, tenantID, userID)
}

// Track records that a member viewed, clicked or dismissed a live
// announcement their organization is in the audience of (once per person).
func (s *Service) Track(ctx context.Context, tenantID, userID, id int64, eventType string) (err error) {
	ctx, end := tracing.Begin(ctx, "announcements.track",
		attribute.Int64("tenant.id", tenantID),
		attribute.Int64("announcement.id", id),
		attribute.String("announcement.event", eventType),
	)
	defer end(&err)
	live, err := s.repo.GetLive(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if eventType == EventDismiss && !live.Dismissible {
		return apperr.New(apperr.InvalidState, "This announcement cannot be dismissed.")
	}
	return s.repo.RecordEvent(ctx, tenantID, userID, id, eventType)
}

// DeliverDue sends live announcements with channels that have not gone out
// yet, once each: the claim is taken before sending, so a crash part-way
// leaves it sent to fewer organizations rather than twice to some.
func (s *Service) DeliverDue(ctx context.Context) (_ int, err error) {
	ctx, end := tracing.Begin(ctx, "announcements.deliver_due")
	defer end(&err)
	if s.notify == nil {
		return 0, nil
	}
	due, err := s.repo.ToDeliver(ctx)
	if err != nil {
		return 0, err
	}
	delivered := 0
	for _, announcement := range due {
		claimed, err := s.repo.ClaimDelivery(ctx, announcement.ID)
		if err != nil {
			return delivered, err
		}
		if !claimed {
			continue
		}
		tenantIDs, err := s.repo.AudienceTenantIDs(ctx, announcement)
		if err != nil {
			return delivered, err
		}
		for _, tenantID := range tenantIDs {
			if err := s.notify(ctx, tenantID, announcement); err != nil && s.logger != nil {
				s.logger.WarnContext(ctx, "announcement delivery failed", "announcement_id", announcement.ID, "tenant_id", tenantID, "error", err)
			}
		}
		delivered++
	}
	return delivered, nil
}

func (s *Service) record(ctx context.Context, action string, a Announcement) {
	if s.activity == nil {
		return
	}
	s.activity.Record(ctx, activity.Entry{
		Action: action, Subject: "announcement", SubjectID: a.ID,
		Changes: map[string]any{"title": a.Title, "kind": a.Kind, "status": a.Status, "audience": a.Audience},
	})
}

func actor(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// optional is nil for a blank filter.
func optional(value string) *string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return &trimmed
	}
	return nil
}

func notFound() error { return apperr.New(apperr.ResourceNotFound, "Announcement not found.") }
