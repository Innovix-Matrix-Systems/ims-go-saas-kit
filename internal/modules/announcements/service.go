package announcements

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Channels an announcement can also be delivered through.
var allowedChannels = map[string]bool{"database": true, "mail": true, "sms": true, "push": true}

// Notifier delivers an announcement to one organization's members through
// the notification system (implemented by the app's wiring).
type Notifier func(ctx context.Context, tenantID int64, announcement Announcement) error

// Service runs announcements.
type Service struct {
	queries  *dbgen.Queries
	tx       *database.TxManager
	activity *activity.Recorder
	notify   Notifier
	logger   *slog.Logger
	now      func() time.Time
}

// NewService creates the announcement service.
func NewService(queries *dbgen.Queries, tx *database.TxManager, logger *slog.Logger) *Service {
	return &Service{queries: queries, tx: tx, logger: logger, now: time.Now}
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

func audience(tenantIDs []int64) string {
	if len(tenantIDs) == 0 {
		return AudienceAll
	}
	return AudienceTenants
}

func timestamp(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// Create adds an announcement and its audience.
func (s *Service) Create(ctx context.Context, actorID int64, input Input) (*Announcement, error) {
	input, err := s.normalize(input)
	if err != nil {
		return nil, err
	}
	var created dbgen.Announcement
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.CreateAnnouncement(ctx, dbgen.CreateAnnouncementParams{
			Title: input.Title, Body: input.Body, Kind: input.Kind, CtaLabel: input.CTALabel, CtaUrl: input.CTAURL,
			Audience: audience(input.TenantIDs), StartsAt: timestamp(input.StartsAt), EndsAt: timestamp(input.EndsAt),
			Dismissible: input.Dismissible, Status: input.Status, Channels: input.Channels, ActorID: actor(actorID),
		})
		if err != nil {
			return database.MapError("announcements.create", err)
		}
		created = row
		return setAudience(ctx, q, row.ID, input.TenantIDs)
	})
	if err != nil {
		return nil, err
	}
	out := mapAnnouncement(created)
	s.record(ctx, "announcement.created", out)
	return &out, nil
}

// Update replaces an announcement's content, window and audience.
func (s *Service) Update(ctx context.Context, actorID, id int64, input Input) (*Announcement, error) {
	input, err := s.normalize(input)
	if err != nil {
		return nil, err
	}
	var updated dbgen.Announcement
	err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.UpdateAnnouncement(ctx, dbgen.UpdateAnnouncementParams{
			ID: id, Title: input.Title, Body: input.Body, Kind: input.Kind, CtaLabel: input.CTALabel, CtaUrl: input.CTAURL,
			Audience: audience(input.TenantIDs), StartsAt: timestamp(input.StartsAt), EndsAt: timestamp(input.EndsAt),
			Dismissible: input.Dismissible, Status: input.Status, Channels: input.Channels, ActorID: actor(actorID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return database.MapError("announcements.update", err)
		}
		updated = row
		if err := q.DeleteAnnouncementTenants(ctx, id); err != nil {
			return database.MapError("announcements.update", err)
		}
		return setAudience(ctx, q, id, input.TenantIDs)
	})
	if err != nil {
		return nil, err
	}
	out := mapAnnouncement(updated)
	s.record(ctx, "announcement.updated", out)
	return &out, nil
}

func setAudience(ctx context.Context, q *dbgen.Queries, id int64, tenantIDs []int64) error {
	if len(tenantIDs) == 0 {
		return nil
	}
	if err := q.AddAnnouncementTenants(ctx, dbgen.AddAnnouncementTenantsParams{AnnouncementID: id, TenantIds: tenantIDs}); err != nil {
		return database.MapError("announcements.audience", err)
	}
	return nil
}

// Delete removes an announcement with its audience and counts.
func (s *Service) Delete(ctx context.Context, id int64) error {
	current, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.queries.DeleteAnnouncement(ctx, id); err != nil {
		return database.MapError("announcements.delete", err)
	}
	s.record(ctx, "announcement.deleted", *current)
	return nil
}

// Get loads one announcement.
func (s *Service) Get(ctx context.Context, id int64) (*Announcement, error) {
	row, err := s.queries.GetAnnouncement(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if err != nil {
		return nil, database.MapError("announcements.get", err)
	}
	out := mapAnnouncement(row)
	return &out, nil
}

// Listed is an announcement in the staff list, with its reach.
type Listed struct {
	Announcement
	TenantCount int64
	Totals      Totals
}

// List returns announcements newest first, optionally of one status.
func (s *Service) List(ctx context.Context, status string, limit, offset int32) ([]Listed, int64, error) {
	var filter *string
	if status != "" {
		filter = &status
	}
	rows, err := s.queries.ListAnnouncements(ctx, dbgen.ListAnnouncementsParams{Status: filter, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, 0, database.MapError("announcements.list", err)
	}
	total, err := s.queries.CountAnnouncements(ctx, filter)
	if err != nil {
		return nil, 0, database.MapError("announcements.list", err)
	}
	out := make([]Listed, 0, len(rows))
	for _, row := range rows {
		out = append(out, Listed{
			Announcement: mapAnnouncement(dbgen.Announcement{
				ID: row.ID, Title: row.Title, Body: row.Body, Kind: row.Kind, CtaLabel: row.CtaLabel, CtaUrl: row.CtaUrl,
				Audience: row.Audience, StartsAt: row.StartsAt, EndsAt: row.EndsAt, Dismissible: row.Dismissible,
				Status: row.Status, Channels: row.Channels, DeliveredAt: row.DeliveredAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}),
			TenantCount: row.TenantCount,
			Totals:      Totals{Views: row.Views, Clicks: row.Clicks, Dismissals: row.Dismissals},
		})
	}
	return out, total, nil
}

// TenantRef names an organization in an announcement's audience or counts.
type TenantRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TenantTotals is one organization's counts.
type TenantTotals struct {
	TenantRef
	Totals
}

// Detail is an announcement with its audience and counts, overall and per
// organization.
type Detail struct {
	Announcement
	Tenants  []TenantRef
	Totals   Totals
	ByTenant []TenantTotals
}

// Detail loads an announcement for staff.
func (s *Service) Detail(ctx context.Context, id int64) (*Detail, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	tenants, err := s.queries.ListAnnouncementTenants(ctx, id)
	if err != nil {
		return nil, database.MapError("announcements.detail", err)
	}
	totals, err := s.queries.AnnouncementTotals(ctx, id)
	if err != nil {
		return nil, database.MapError("announcements.detail", err)
	}
	byTenant, err := s.queries.AnnouncementTotalsByTenant(ctx, id)
	if err != nil {
		return nil, database.MapError("announcements.detail", err)
	}
	out := &Detail{Announcement: *current, Tenants: []TenantRef{}, Totals: Totals(totals), ByTenant: []TenantTotals{}}
	for _, row := range tenants {
		out.Tenants = append(out.Tenants, TenantRef{ID: row.ID, Name: row.Name})
	}
	for _, row := range byTenant {
		out.ByTenant = append(out.ByTenant, TenantTotals{
			TenantRef: TenantRef{ID: row.TenantID, Name: row.Name},
			Totals:    Totals{Views: row.Views, Clicks: row.Clicks, Dismissals: row.Dismissals},
		})
	}
	return out, nil
}

// Live lists what a member of the tenant should see now, without what they
// dismissed.
func (s *Service) Live(ctx context.Context, tenantID, userID int64) ([]Announcement, error) {
	rows, err := s.queries.ListLiveAnnouncements(ctx, dbgen.ListLiveAnnouncementsParams{TenantID: tenantID, UserID: userID})
	if err != nil {
		return nil, database.MapError("announcements.live", err)
	}
	out := make([]Announcement, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapAnnouncement(row))
	}
	return out, nil
}

// Track records that a member viewed, clicked or dismissed a live
// announcement their organization is in the audience of (once per person).
func (s *Service) Track(ctx context.Context, tenantID, userID, id int64, eventType string) error {
	row, err := s.queries.GetLiveAnnouncementForTenant(ctx, dbgen.GetLiveAnnouncementForTenantParams{ID: id, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound()
	}
	if err != nil {
		return database.MapError("announcements.track", err)
	}
	if eventType == EventDismiss && !row.Dismissible {
		return apperr.New(apperr.InvalidState, "This announcement cannot be dismissed.")
	}
	tenant := tenantID
	if err := s.queries.RecordAnnouncementEvent(ctx, dbgen.RecordAnnouncementEventParams{
		AnnouncementID: id, UserID: userID, TenantID: &tenant, Type: eventType,
	}); err != nil {
		return database.MapError("announcements.track", err)
	}
	return nil
}

// DeliverDue sends live announcements with channels that have not gone out
// yet, once each: the claim is taken before sending, so a crash part-way
// leaves it sent to fewer organizations rather than twice to some.
func (s *Service) DeliverDue(ctx context.Context) (int, error) {
	if s.notify == nil {
		return 0, nil
	}
	due, err := s.queries.ListAnnouncementsToDeliver(ctx)
	if err != nil {
		return 0, database.MapError("announcements.deliver", err)
	}
	delivered := 0
	for _, row := range due {
		claimed, err := s.queries.MarkAnnouncementDelivered(ctx, row.ID)
		if err != nil {
			return delivered, database.MapError("announcements.deliver", err)
		}
		if claimed == 0 {
			continue
		}
		tenantIDs, err := s.queries.ListAnnouncementAudienceTenantIDs(ctx, dbgen.ListAnnouncementAudienceTenantIDsParams{
			Audience: row.Audience, AnnouncementID: row.ID,
		})
		if err != nil {
			return delivered, database.MapError("announcements.deliver", err)
		}
		announcement := mapAnnouncement(row)
		for _, tenantID := range tenantIDs {
			if err := s.notify(ctx, tenantID, announcement); err != nil && s.logger != nil {
				s.logger.WarnContext(ctx, "announcement delivery failed", "announcement_id", row.ID, "tenant_id", tenantID, "error", err)
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

func notFound() error { return apperr.New(apperr.ResourceNotFound, "Announcement not found.") }
