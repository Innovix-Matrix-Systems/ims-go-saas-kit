package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/privacy"
)

// PrivacyContributor covers sign-in sessions (personal_access_tokens) and
// two-factor enrolment. Erasure deletes both; the export lists sessions and
// the 2FA method, never a token, secret or recovery code.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "auth" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string {
	return []string{"personal_access_tokens.tokenable_id", "user_two_factor.user_id"}
}

type privacySession struct {
	Name       string     `json:"name"`
	Device     string     `json:"device"`
	IP         *string    `json:"ip"`
	UserAgent  *string    `json:"user_agent"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  *time.Time `json:"created_at"`
}

type privacyTwoFactor struct {
	Enabled     bool       `json:"enabled"`
	Method      string     `json:"method,omitempty"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
}

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	tokens, err := q.ListUserTokensForPrivacy(ctx, s.UserID)
	if err != nil {
		return database.MapError("auth.privacy_export", err)
	}
	sessions := make([]privacySession, 0, len(tokens))
	for _, t := range tokens {
		sessions = append(sessions, privacySession{
			Name: t.Name, Device: t.Device, IP: t.Ip, UserAgent: t.UserAgent,
			LastUsedAt: privacy.Time(t.LastUsedAt), ExpiresAt: privacy.Time(t.ExpiresAt), CreatedAt: privacy.Time(t.CreatedAt),
		})
	}
	if err := w.JSON("sessions", sessions); err != nil {
		return err
	}
	twoFactor := privacyTwoFactor{}
	row, err := q.GetTwoFactor(ctx, s.UserID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return database.MapError("auth.privacy_export", err)
	default:
		twoFactor = privacyTwoFactor{Enabled: row.ConfirmedAt.Valid, Method: row.Method, ConfirmedAt: privacy.Time(row.ConfirmedAt)}
	}
	return w.JSON("two_factor", twoFactor)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	tokens, err := q.DeleteTokensForUser(ctx, dbgen.DeleteTokensForUserParams{TokenableType: authn.ModelTypeUser, TokenableID: s.UserID})
	if err != nil {
		return privacy.Report{}, database.MapError("auth.privacy_erase", err)
	}
	twoFactor, err := q.DeleteTwoFactor(ctx, s.UserID)
	if err != nil {
		return privacy.Report{}, database.MapError("auth.privacy_erase", err)
	}
	return privacy.Report{Deleted: tokens + twoFactor}, nil
}
