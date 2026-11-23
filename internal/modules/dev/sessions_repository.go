package dev

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// SessionRepository is the persistence contract for the Sessions panel.
type SessionRepository interface {
	Stats(ctx context.Context) (SessionStats, error)
	ListUsers(ctx context.Context, filter SessionUserFilter) ([]SessionUser, error)
	UserExists(ctx context.Context, userID int64) (bool, error)
	ListTokens(ctx context.Context, userID int64) ([]SessionToken, error)
	DeleteToken(ctx context.Context, tokenID int64) (bool, error)
	DeleteUserTokens(ctx context.Context, userID int64) (int64, error)
	DeleteAllExcept(ctx context.Context, keepTokenID int64) (int64, error)
	Prune(ctx context.Context, idleDays int32, dryRun bool) (int64, error)
}

type sessionRepository struct {
	queries *dbgen.Queries
	now     func() time.Time
}

// NewSessionRepository creates the sqlc-backed session repository.
func NewSessionRepository(queries *dbgen.Queries) SessionRepository {
	return &sessionRepository{queries: queries, now: time.Now}
}

func (r *sessionRepository) Stats(ctx context.Context) (SessionStats, error) {
	row, err := r.queries.SessionTokenStats(ctx)
	if err != nil {
		return SessionStats{}, database.MapError("dev.session_stats", err)
	}
	return SessionStats{
		Users:        row.Users,
		ActiveTokens: row.ActiveTokens,
		Used24h:      row.Used24h,
		IdleTokens:   row.IdleTokens,
		Inactive30d:  row.Inactive30d,
	}, nil
}

func (r *sessionRepository) ListUsers(ctx context.Context, filter SessionUserFilter) ([]SessionUser, error) {
	rows, err := r.queries.ListSessionUsers(ctx, dbgen.ListSessionUsersParams{
		BeforeID: filter.BeforeID,
		Search:   searchArg(filter.Search),
		RowLimit: filter.Limit,
	})
	if err != nil {
		return nil, database.MapError("dev.list_session_users", err)
	}
	users := make([]SessionUser, 0, len(rows))
	for _, row := range rows {
		users = append(users, SessionUser{
			UserID:       row.UserID,
			Name:         row.Name,
			Email:        row.Email,
			TenantID:     row.TenantID,
			ActiveTokens: row.ActiveTokens,
			TotalTokens:  row.TotalTokens,
			LastUsedAt:   timePtr(row.LastUsedAt),
		})
	}
	return users, nil
}

func (r *sessionRepository) UserExists(ctx context.Context, userID int64) (bool, error) {
	exists, err := r.queries.SessionUserExists(ctx, userID)
	if err != nil {
		return false, database.MapError("dev.session_user_exists", err)
	}
	return exists, nil
}

func (r *sessionRepository) ListTokens(ctx context.Context, userID int64) ([]SessionToken, error) {
	rows, err := r.queries.ListSessionTokens(ctx, userID)
	if err != nil {
		return nil, database.MapError("dev.list_session_tokens", err)
	}
	now := r.now()
	tokens := make([]SessionToken, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, SessionToken{
			ID:         row.ID,
			Name:       row.Name,
			Device:     row.Device,
			CreatedAt:  row.CreatedAt.Time,
			LastUsedAt: timePtr(row.LastUsedAt),
			ExpiresAt:  timePtr(row.ExpiresAt),
			Expired:    row.ExpiresAt.Valid && now.After(row.ExpiresAt.Time),
		})
	}
	return tokens, nil
}

func (r *sessionRepository) DeleteToken(ctx context.Context, tokenID int64) (bool, error) {
	deleted, err := r.queries.DeleteSessionToken(ctx, tokenID)
	if err != nil {
		return false, database.MapError("dev.delete_session_token", err)
	}
	return deleted > 0, nil
}

func (r *sessionRepository) DeleteUserTokens(ctx context.Context, userID int64) (int64, error) {
	deleted, err := r.queries.DeleteSessionUserTokens(ctx, userID)
	if err != nil {
		return 0, database.MapError("dev.delete_session_user_tokens", err)
	}
	return deleted, nil
}

func (r *sessionRepository) DeleteAllExcept(ctx context.Context, keepTokenID int64) (int64, error) {
	deleted, err := r.queries.DeleteAllSessionTokensExcept(ctx, keepTokenID)
	if err != nil {
		return 0, database.MapError("dev.delete_all_session_tokens", err)
	}
	return deleted, nil
}

func (r *sessionRepository) Prune(ctx context.Context, idleDays int32, dryRun bool) (int64, error) {
	if dryRun {
		count, err := r.queries.CountPrunableSessionTokens(ctx, idleDays)
		if err != nil {
			return 0, database.MapError("dev.count_prunable_sessions", err)
		}
		return count, nil
	}
	deleted, err := r.queries.DeletePrunableSessionTokens(ctx, idleDays)
	if err != nil {
		return 0, database.MapError("dev.prune_sessions", err)
	}
	return deleted, nil
}

func searchArg(search string) *string {
	if search == "" {
		return nil
	}
	return &search
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time
	return &at
}
