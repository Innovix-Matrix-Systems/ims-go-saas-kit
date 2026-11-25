package announcements

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// Repository is the persistence contract for announcements.
type Repository interface {
	Create(ctx context.Context, input Input, actorID *int64) (*Announcement, error)
	Update(ctx context.Context, id int64, input Input, actorID *int64) (*Announcement, error)
	SetAudience(ctx context.Context, id int64, tenantIDs []int64) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*Announcement, error)
	List(ctx context.Context, status *string, limit, offset int32) ([]Listed, error)
	Count(ctx context.Context, status *string) (int64, error)
	Audience(ctx context.Context, id int64) ([]TenantRef, error)
	Totals(ctx context.Context, id int64) (Totals, error)
	TenantTotals(ctx context.Context, id int64, search *string, limit, offset int32) ([]TenantTotals, error)
	CountTenantTotals(ctx context.Context, id int64, search *string) (int64, error)
	Live(ctx context.Context, tenantID, userID int64) ([]Announcement, error)
	GetLive(ctx context.Context, tenantID, id int64) (*Announcement, error)
	RecordEvent(ctx context.Context, tenantID, userID, id int64, eventType string) error
	ToDeliver(ctx context.Context) ([]Announcement, error)
	ClaimDelivery(ctx context.Context, id int64) (bool, error)
	AudienceTenantIDs(ctx context.Context, a Announcement) ([]int64, error)
	StartedOrEnded(ctx context.Context, since, until time.Time) ([]Announcement, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Create(ctx context.Context, input Input, actorID *int64) (*Announcement, error) {
	row, err := r.queries.CreateAnnouncement(ctx, dbgen.CreateAnnouncementParams{
		Title: input.Title, Body: input.Body, Kind: input.Kind, CtaLabel: input.CTALabel, CtaUrl: input.CTAURL,
		Audience: audience(input.TenantIDs), StartsAt: timestamp(input.StartsAt), EndsAt: timestamp(input.EndsAt),
		Dismissible: input.Dismissible, Status: input.Status, Channels: input.Channels, ActorID: actorID,
	})
	if err != nil {
		return nil, database.MapError("announcements.create", err)
	}
	out := mapAnnouncement(row)
	return &out, nil
}

func (r *repository) Update(ctx context.Context, id int64, input Input, actorID *int64) (*Announcement, error) {
	row, err := r.queries.UpdateAnnouncement(ctx, dbgen.UpdateAnnouncementParams{
		ID: id, Title: input.Title, Body: input.Body, Kind: input.Kind, CtaLabel: input.CTALabel, CtaUrl: input.CTAURL,
		Audience: audience(input.TenantIDs), StartsAt: timestamp(input.StartsAt), EndsAt: timestamp(input.EndsAt),
		Dismissible: input.Dismissible, Status: input.Status, Channels: input.Channels, ActorID: actorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if err != nil {
		return nil, database.MapError("announcements.update", err)
	}
	out := mapAnnouncement(row)
	return &out, nil
}

// SetAudience replaces the chosen organizations (none: every organization).
func (r *repository) SetAudience(ctx context.Context, id int64, tenantIDs []int64) error {
	if err := r.queries.DeleteAnnouncementTenants(ctx, id); err != nil {
		return database.MapError("announcements.audience", err)
	}
	if len(tenantIDs) == 0 {
		return nil
	}
	if err := r.queries.AddAnnouncementTenants(ctx, dbgen.AddAnnouncementTenantsParams{AnnouncementID: id, TenantIds: tenantIDs}); err != nil {
		return database.MapError("announcements.audience", err)
	}
	return nil
}

func (r *repository) Delete(ctx context.Context, id int64) error {
	if _, err := r.queries.DeleteAnnouncement(ctx, id); err != nil {
		return database.MapError("announcements.delete", err)
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id int64) (*Announcement, error) {
	row, err := r.queries.GetAnnouncement(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if err != nil {
		return nil, database.MapError("announcements.get", err)
	}
	out := mapAnnouncement(row)
	return &out, nil
}

func (r *repository) List(ctx context.Context, status *string, limit, offset int32) ([]Listed, error) {
	rows, err := r.queries.ListAnnouncements(ctx, dbgen.ListAnnouncementsParams{Status: status, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, database.MapError("announcements.list", err)
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
	return out, nil
}

func (r *repository) Count(ctx context.Context, status *string) (int64, error) {
	total, err := r.queries.CountAnnouncements(ctx, status)
	if err != nil {
		return 0, database.MapError("announcements.list", err)
	}
	return total, nil
}

func (r *repository) Audience(ctx context.Context, id int64) ([]TenantRef, error) {
	rows, err := r.queries.ListAnnouncementTenants(ctx, id)
	if err != nil {
		return nil, database.MapError("announcements.detail", err)
	}
	out := make([]TenantRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, TenantRef{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

func (r *repository) Totals(ctx context.Context, id int64) (Totals, error) {
	row, err := r.queries.AnnouncementTotals(ctx, id)
	if err != nil {
		return Totals{}, database.MapError("announcements.detail", err)
	}
	return Totals(row), nil
}

func (r *repository) TenantTotals(ctx context.Context, id int64, search *string, limit, offset int32) ([]TenantTotals, error) {
	rows, err := r.queries.ListAnnouncementTenantTotals(ctx, dbgen.ListAnnouncementTenantTotalsParams{
		AnnouncementID: id, Search: search, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, database.MapError("announcements.tenant_totals", err)
	}
	out := make([]TenantTotals, 0, len(rows))
	for _, row := range rows {
		out = append(out, TenantTotals{
			TenantRef: TenantRef{ID: row.TenantID, Name: row.Name},
			Totals:    Totals{Views: row.Views, Clicks: row.Clicks, Dismissals: row.Dismissals},
		})
	}
	return out, nil
}

func (r *repository) CountTenantTotals(ctx context.Context, id int64, search *string) (int64, error) {
	total, err := r.queries.CountAnnouncementTenantTotals(ctx, dbgen.CountAnnouncementTenantTotalsParams{AnnouncementID: id, Search: search})
	if err != nil {
		return 0, database.MapError("announcements.tenant_totals", err)
	}
	return total, nil
}

func (r *repository) Live(ctx context.Context, tenantID, userID int64) ([]Announcement, error) {
	rows, err := r.queries.ListLiveAnnouncements(ctx, dbgen.ListLiveAnnouncementsParams{TenantID: tenantID, UserID: userID})
	if err != nil {
		return nil, database.MapError("announcements.live", err)
	}
	return mapAnnouncements(rows), nil
}

func (r *repository) GetLive(ctx context.Context, tenantID, id int64) (*Announcement, error) {
	row, err := r.queries.GetLiveAnnouncementForTenant(ctx, dbgen.GetLiveAnnouncementForTenantParams{ID: id, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if err != nil {
		return nil, database.MapError("announcements.track", err)
	}
	out := mapAnnouncement(row)
	return &out, nil
}

func (r *repository) RecordEvent(ctx context.Context, tenantID, userID, id int64, eventType string) error {
	if err := r.queries.RecordAnnouncementEvent(ctx, dbgen.RecordAnnouncementEventParams{
		AnnouncementID: id, UserID: userID, TenantID: &tenantID, Type: eventType,
	}); err != nil {
		return database.MapError("announcements.track", err)
	}
	return nil
}

func (r *repository) ToDeliver(ctx context.Context) ([]Announcement, error) {
	rows, err := r.queries.ListAnnouncementsToDeliver(ctx)
	if err != nil {
		return nil, database.MapError("announcements.deliver", err)
	}
	return mapAnnouncements(rows), nil
}

// ClaimDelivery marks an announcement delivered; false when another worker
// claimed it first.
func (r *repository) ClaimDelivery(ctx context.Context, id int64) (bool, error) {
	claimed, err := r.queries.MarkAnnouncementDelivered(ctx, id)
	if err != nil {
		return false, database.MapError("announcements.deliver", err)
	}
	return claimed > 0, nil
}

func (r *repository) AudienceTenantIDs(ctx context.Context, a Announcement) ([]int64, error) {
	ids, err := r.queries.ListAnnouncementAudienceTenantIDs(ctx, dbgen.ListAnnouncementAudienceTenantIDsParams{
		Audience: a.Audience, AnnouncementID: a.ID,
	})
	if err != nil {
		return nil, database.MapError("announcements.deliver", err)
	}
	return ids, nil
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

// StartedOrEnded lists published announcements whose window opened or
// closed in (since, until].
func (r *repository) StartedOrEnded(ctx context.Context, since, until time.Time) ([]Announcement, error) {
	rows, err := r.queries.ListAnnouncementsStartedOrEnded(ctx, dbgen.ListAnnouncementsStartedOrEndedParams{
		Since: timestamp(&since), Until: timestamp(&until),
	})
	if err != nil {
		return nil, database.MapError("announcements.started_or_ended", err)
	}
	return mapAnnouncements(rows), nil
}

func mapAnnouncements(rows []dbgen.Announcement) []Announcement {
	out := make([]Announcement, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapAnnouncement(row))
	}
	return out
}
