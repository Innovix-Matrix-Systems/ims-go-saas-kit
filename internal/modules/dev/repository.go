package dev

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// InsertActionInput carries one audit row.
type InsertActionInput struct {
	ActorUserID  int64
	Action       string
	Target       *string
	Params       json.RawMessage
	Status       string
	ErrorMessage *string
	DurationMS   int32
	IP           *string
	UserAgent    *string
}

// ActionFilter narrows the audit log.
type ActionFilter struct {
	Search   string // actor e-mail, target, IP prefix or params text
	Action   string
	ActorID  int64
	Status   string // ok | error
	From     *time.Time
	To       *time.Time
	BeforeID int64
	Limit    int32
}

// ActionKind is an action key with how often it occurs.
type ActionKind struct {
	Action string `json:"action"`
	Total  int64  `json:"total"`
}

// ActionActor is someone who appears in the audit log.
type ActionActor struct {
	UserID int64   `json:"user_id"`
	Email  *string `json:"email"`
	Total  int64   `json:"total"`
}

// ActionStats is the audit log header.
type ActionStats struct {
	LastDay       int64 `json:"last_day"`
	FailedSignIns int64 `json:"failed_sign_ins"`
	RetentionDays int   `json:"retention_days"`
}

// ActionRepository persists the developer action audit log.
type ActionRepository interface {
	Insert(ctx context.Context, input InsertActionInput) (*Action, error)
	List(ctx context.Context, beforeID int64, limit int32) ([]Action, error)
	Search(ctx context.Context, filter ActionFilter) ([]Action, int64, error)
	Get(ctx context.Context, id int64) (*Action, error)
	Kinds(ctx context.Context) ([]ActionKind, error)
	Actors(ctx context.Context) ([]ActionActor, error)
	Stats(ctx context.Context) (ActionStats, error)
	Prune(ctx context.Context, retentionDays int) (int64, error)
}

type actionRepository struct {
	queries *dbgen.Queries
}

// NewActionRepository creates the sqlc-backed action audit repository.
func NewActionRepository(queries *dbgen.Queries) ActionRepository {
	return &actionRepository{queries: queries}
}

func (r *actionRepository) Insert(ctx context.Context, input InsertActionInput) (*Action, error) {
	params := input.Params
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	row, err := r.queries.InsertDevAction(ctx, dbgen.InsertDevActionParams{
		ActorUserID:  input.ActorUserID,
		Action:       input.Action,
		Target:       input.Target,
		Params:       params,
		Status:       input.Status,
		ErrorMessage: input.ErrorMessage,
		DurationMs:   input.DurationMS,
		Ip:           input.IP,
		UserAgent:    input.UserAgent,
	})
	if err != nil {
		return nil, database.MapError("dev.insert_action", err)
	}
	action := mapAction(row)
	return &action, nil
}

func (r *actionRepository) List(ctx context.Context, beforeID int64, limit int32) ([]Action, error) {
	actions, _, err := r.Search(ctx, ActionFilter{BeforeID: beforeID, Limit: limit})
	return actions, err
}

func (r *actionRepository) Search(ctx context.Context, filter ActionFilter) ([]Action, int64, error) {
	from, to := optionalTime(filter.From), optionalTime(filter.To)
	rows, err := r.queries.SearchDevActions(ctx, dbgen.SearchDevActionsParams{
		Action: filter.Action, ActorID: filter.ActorID, Status: filter.Status, FromAt: from, ToAt: to,
		Search: filter.Search, BeforeID: filter.BeforeID, RowLimit: filter.Limit,
	})
	if err != nil {
		return nil, 0, database.MapError("dev.search_actions", err)
	}
	total, err := r.queries.CountDevActionsFiltered(ctx, dbgen.CountDevActionsFilteredParams{
		Action: filter.Action, ActorID: filter.ActorID, Status: filter.Status, FromAt: from, ToAt: to,
		Search: filter.Search,
	})
	if err != nil {
		return nil, 0, database.MapError("dev.count_actions", err)
	}
	actions := make([]Action, 0, len(rows))
	for _, row := range rows {
		action := mapAction(dbgen.DevAction{
			ID: row.ID, ActorUserID: row.ActorUserID, Action: row.Action, Target: row.Target, Params: row.Params,
			Status: row.Status, ErrorMessage: row.ErrorMessage, DurationMs: row.DurationMs, CreatedAt: row.CreatedAt,
			Ip: row.Ip, UserAgent: row.UserAgent,
		})
		action.ActorEmail, action.ActorName = row.ActorEmail, row.ActorName
		actions = append(actions, action)
	}
	return actions, total, nil
}

func (r *actionRepository) Get(ctx context.Context, id int64) (*Action, error) {
	row, err := r.queries.GetDevActionDetail(ctx, id)
	if err != nil {
		return nil, database.MapError("dev.get_action", err)
	}
	action := mapAction(dbgen.DevAction{
		ID: row.ID, ActorUserID: row.ActorUserID, Action: row.Action, Target: row.Target, Params: row.Params,
		Status: row.Status, ErrorMessage: row.ErrorMessage, DurationMs: row.DurationMs, CreatedAt: row.CreatedAt,
		Ip: row.Ip, UserAgent: row.UserAgent,
	})
	action.ActorEmail, action.ActorName = row.ActorEmail, row.ActorName
	return &action, nil
}

func (r *actionRepository) Kinds(ctx context.Context) ([]ActionKind, error) {
	rows, err := r.queries.DevActionKinds(ctx)
	if err != nil {
		return nil, database.MapError("dev.action_kinds", err)
	}
	out := make([]ActionKind, 0, len(rows))
	for _, row := range rows {
		out = append(out, ActionKind{Action: row.Action, Total: row.Total})
	}
	return out, nil
}

func (r *actionRepository) Actors(ctx context.Context) ([]ActionActor, error) {
	rows, err := r.queries.DevActionActors(ctx)
	if err != nil {
		return nil, database.MapError("dev.action_actors", err)
	}
	out := make([]ActionActor, 0, len(rows))
	for _, row := range rows {
		out = append(out, ActionActor{UserID: row.ActorUserID, Email: row.Email, Total: row.Total})
	}
	return out, nil
}

func (r *actionRepository) Stats(ctx context.Context) (ActionStats, error) {
	row, err := r.queries.DevActionStats(ctx)
	if err != nil {
		return ActionStats{}, database.MapError("dev.action_stats", err)
	}
	return ActionStats{LastDay: row.LastDay, FailedSignIns: row.FailedSignIns}, nil
}

func (r *actionRepository) Prune(ctx context.Context, retentionDays int) (int64, error) {
	deleted, err := r.queries.PruneDevActions(ctx, int32(retentionDays))
	if err != nil {
		return 0, database.MapError("dev.prune_actions", err)
	}
	return deleted, nil
}

func optionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}
