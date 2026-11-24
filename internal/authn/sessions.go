package authn

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// maxUserAgent caps the user agent stored with a token.
const maxUserAgent = 512

// Session is one signed-in device of a user: a session token. MCP
// connections are not sessions; they are listed with the user's connections.
type Session struct {
	ID         int64      `json:"id"`
	Device     string     `json:"device"`
	IP         *string    `json:"ip"`
	UserAgent  *string    `json:"user_agent"`
	Browser    string     `json:"browser"` // "Chrome 131 on Windows"
	Current    bool       `json:"current"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// SessionAudit records session revocations (implemented by
// activity.Recorder; an interface keeps authn free of modules).
type SessionAudit interface {
	SessionsRevoked(ctx context.Context, user *Identity, devices []string, others bool)
}

// Sessions lets users see and revoke their own sessions.
type Sessions struct {
	queries *dbgen.Queries
	auth    Authenticator
	audit   SessionAudit
}

// NewSessions creates the self-service session service.
func NewSessions(queries *dbgen.Queries, auth Authenticator) *Sessions {
	return &Sessions{queries: queries, auth: auth}
}

// WithAudit records revocations in the activity log.
func (s *Sessions) WithAudit(audit SessionAudit) *Sessions {
	s.audit = audit
	return s
}

// List returns the caller's sessions; the one making the request is marked
// current.
func (s *Sessions) List(ctx context.Context, caller *Identity) ([]Session, error) {
	rows, err := s.queries.ListUserSessions(ctx, caller.UserID)
	if err != nil {
		return nil, database.MapError("authn.list_sessions", err)
	}
	sessions := make([]Session, 0, len(rows))
	for _, row := range rows {
		session := Session{
			ID:        row.ID,
			Device:    row.Device,
			IP:        row.Ip,
			UserAgent: row.UserAgent,
			Current:   row.ID == caller.TokenID,
			CreatedAt: row.CreatedAt.Time,
		}
		if row.UserAgent != nil {
			session.Browser = httpx.DescribeUserAgent(*row.UserAgent)
		}
		if row.LastUsedAt.Valid {
			used := row.LastUsedAt.Time
			session.LastUsedAt = &used
		}
		if row.ExpiresAt.Valid {
			expires := row.ExpiresAt.Time
			session.ExpiresAt = &expires
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// Revoke signs one of the caller's sessions out. Another user's session, or
// an MCP token, is "not found".
func (s *Sessions) Revoke(ctx context.Context, caller *Identity, sessionID int64) error {
	device, err := s.queries.DeleteUserSession(ctx, dbgen.DeleteUserSessionParams{ID: sessionID, UserID: caller.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Newf(apperr.ResourceNotFound, "Session with ID '%d' was not found.", sessionID)
	}
	if err != nil {
		return database.MapError("authn.revoke_session", err)
	}
	if s.audit != nil {
		s.audit.SessionsRevoked(ctx, caller, []string{device}, false)
	}
	return nil
}

// RevokeOthers signs every other session of the caller out, keeping the one
// making the request. Like a password change, it also disconnects the
// caller's MCP connections: "sign out everywhere else" after a lost device
// must leave nothing behind.
func (s *Sessions) RevokeOthers(ctx context.Context, caller *Identity) error {
	others, err := s.List(ctx, caller)
	if err != nil {
		return err
	}
	if err := s.auth.RevokeOthers(ctx, caller.UserID, caller.TokenID); err != nil {
		return err
	}
	devices := make([]string, 0, len(others))
	for _, session := range others {
		if !session.Current {
			devices = append(devices, session.Device)
		}
	}
	if s.audit != nil && len(devices) > 0 {
		s.audit.SessionsRevoked(ctx, caller, devices, true)
	}
	return nil
}

// Upgrade swaps the caller's setup-only token for a full token on the same
// device, once two-factor is on. The setup token stops working.
func (s *Sessions) Upgrade(ctx context.Context, caller *Identity) (Token, error) {
	row, err := s.queries.GetToken(ctx, caller.TokenID)
	if err != nil {
		return Token{}, database.MapError("authn.get_token", err)
	}
	token, err := s.auth.Issue(ctx, caller.UserID, row.Device)
	if err != nil {
		return Token{}, err
	}
	if _, err := s.queries.DeleteUserSession(ctx, dbgen.DeleteUserSessionParams{ID: caller.TokenID, UserID: caller.UserID}); err != nil {
		return Token{}, database.MapError("authn.revoke_setup_token", err)
	}
	return token, nil
}

// PruneTokens deletes expired tokens and, with idleDays > 0, session tokens
// unused for that long (the auth.prune-tokens task).
func (s *Sessions) PruneTokens(ctx context.Context, idleDays int) (int64, error) {
	expired, err := s.queries.DeleteExpiredTokens(ctx)
	if err != nil {
		return 0, database.MapError("authn.prune_expired_tokens", err)
	}
	if idleDays <= 0 {
		return expired, nil
	}
	idle, err := s.queries.DeleteIdleSessionTokens(ctx, int32(idleDays))
	if err != nil {
		return expired, database.MapError("authn.prune_idle_tokens", err)
	}
	return expired + idle, nil
}

func optionalText(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
