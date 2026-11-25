package activity

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// scopePredicate is the tenant part every feed query shares.
type scopePredicate struct {
	SystemOnly bool
	AllTenants bool
	TenantID   *int64
}

// FeedQuery is one read of the feed: the rows the reader may see, narrowed
// by the filter, before a cursor, at most Limit rows.
type FeedQuery struct {
	scopePredicate
	SubjectType      string
	SubjectID        int64
	ActorID          int64
	ImpersonatorID   int64
	ImpersonatedOnly bool
	Action           string
	From, To         time.Time
	Search           string
	BeforeAt         time.Time
	BeforeID         int64
	Limit            int
}

// NewEntry is a row to store.
type NewEntry struct {
	TenantID, ActorID, ImpersonatorID, SubjectID int64
	ImpersonatorName                             string
	Action, SubjectType                          string
	Changes                                      []byte
	IP, RequestID                                string
}

// Repository is the persistence contract for the activity log.
type Repository interface {
	Insert(ctx context.Context, entry NewEntry) error
	List(ctx context.Context, query FeedQuery) ([]Item, error)
	Count(ctx context.Context, query FeedQuery) (int64, error)
	Facets(ctx context.Context, where scopePredicate) (*Facets, error)
	Purge(ctx context.Context, days int) (int64, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) Insert(ctx context.Context, entry NewEntry) error {
	return r.queries.InsertActivity(ctx, dbgen.InsertActivityParams{
		TenantID:         optional(entry.TenantID),
		ActorID:          optional(entry.ActorID),
		ImpersonatorID:   optional(entry.ImpersonatorID),
		ImpersonatorName: optionalString(entry.ImpersonatorName),
		Action:           entry.Action,
		SubjectType:      entry.SubjectType,
		SubjectID:        optional(entry.SubjectID),
		Changes:          entry.Changes,
		Ip:               optionalString(entry.IP),
		RequestID:        optionalString(entry.RequestID),
	})
}

func (r *repository) List(ctx context.Context, query FeedQuery) ([]Item, error) {
	params := dbgen.ListActivityParams{
		SystemOnly: query.SystemOnly, AllTenants: query.AllTenants, TenantID: query.TenantID,
		SubjectType: optionalString(query.SubjectType), SubjectID: optional(query.SubjectID), ActorID: optional(query.ActorID),
		ImpersonatorID: optional(query.ImpersonatorID), ImpersonatedOnly: query.ImpersonatedOnly,
		Action: optionalString(query.Action), FromAt: optionalTime(query.From), ToAt: optionalTime(query.To),
		Search: optionalString(query.Search), RowLimit: int32(query.Limit),
	}
	if query.BeforeID > 0 {
		params.BeforeAt = pgtype.Timestamptz{Time: query.BeforeAt, Valid: true}
		params.BeforeID = &query.BeforeID
	}
	rows, err := r.queries.ListActivity(ctx, params)
	if err != nil {
		return nil, database.MapError("activity.list", err)
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, Item{
			ID: row.ID, TenantID: row.TenantID, ActorID: row.ActorID, ActorName: row.ActorName,
			ImpersonatorID: row.ImpersonatorID, ImpersonatorName: row.ImpersonatorName,
			Action: row.Action, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
			Changes: json.RawMessage(row.Changes), IP: row.Ip, RequestID: row.RequestID,
			CreatedAt: row.CreatedAt.Time,
		})
	}
	return items, nil
}

func (r *repository) Count(ctx context.Context, query FeedQuery) (int64, error) {
	total, err := r.queries.CountActivity(ctx, dbgen.CountActivityParams{
		SystemOnly: query.SystemOnly, AllTenants: query.AllTenants, TenantID: query.TenantID,
		SubjectType: optionalString(query.SubjectType), SubjectID: optional(query.SubjectID), ActorID: optional(query.ActorID),
		ImpersonatorID: optional(query.ImpersonatorID), ImpersonatedOnly: query.ImpersonatedOnly,
		Action: optionalString(query.Action), FromAt: optionalTime(query.From), ToAt: optionalTime(query.To),
		Search: optionalString(query.Search),
	})
	if err != nil {
		return 0, database.MapError("activity.count", err)
	}
	return total, nil
}

func (r *repository) Facets(ctx context.Context, where scopePredicate) (*Facets, error) {
	out := &Facets{Actions: []Count{}, Subjects: []Count{}, Actors: []ActorCount{}}
	actions, err := r.queries.ActivityActionFacets(ctx, dbgen.ActivityActionFacetsParams(where))
	if err != nil {
		return nil, database.MapError("activity.facets", err)
	}
	for _, row := range actions {
		out.Actions = append(out.Actions, Count{Value: row.Action, Total: row.Total})
	}
	subjects, err := r.queries.ActivitySubjectFacets(ctx, dbgen.ActivitySubjectFacetsParams(where))
	if err != nil {
		return nil, database.MapError("activity.facets", err)
	}
	for _, row := range subjects {
		out.Subjects = append(out.Subjects, Count{Value: row.SubjectType, Total: row.Total})
	}
	actors, err := r.queries.ActivityActorFacets(ctx, dbgen.ActivityActorFacetsParams(where))
	if err != nil {
		return nil, database.MapError("activity.facets", err)
	}
	for _, row := range actors {
		out.Actors = append(out.Actors, ActorCount{ID: row.ActorID, Name: row.ActorName, Total: row.Total})
	}
	if out.LastDay, err = r.queries.ActivityLastDay(ctx, dbgen.ActivityLastDayParams(where)); err != nil {
		return nil, database.MapError("activity.facets", err)
	}
	return out, nil
}

func (r *repository) Purge(ctx context.Context, days int) (int64, error) {
	deleted, err := r.queries.PurgeActivity(ctx, int32(days))
	if err != nil {
		return 0, database.MapError("activity.purge", err)
	}
	return deleted, nil
}

func optionalTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: !value.IsZero()}
}
