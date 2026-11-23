package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// ModelTypeUser is the tokenable/RBAC model type used for user rows.
const ModelTypeUser = "user"

// Token is an issued access token; PlainText is the only time the secret is
// available and has the wire format "id|secret".
type Token struct {
	ID        int64
	PlainText string
	ExpiresAt *time.Time
}

// Authenticator issues, verifies and revokes access tokens. The opaque
// implementation ships today; a JWT driver can replace it without touching
// middleware, handlers or services.
type Authenticator interface {
	Issue(ctx context.Context, userID int64, device string) (Token, error)
	// IssueSetup issues a setup-only token: it reaches only the session
	// routes (two-factor enrolment, logout), for a user who must turn on
	// two-factor authentication before working.
	IssueSetup(ctx context.Context, userID int64, device string) (Token, error)
	Verify(ctx context.Context, raw string) (*Identity, error)
	Revoke(ctx context.Context, userID int64, device string) error
	RevokeAll(ctx context.Context, userID int64) error
	// RevokeOthers deletes every token of the user except keepTokenID
	// (Laravel's logoutOtherDevices).
	RevokeOthers(ctx context.Context, userID, keepTokenID int64) error
}

// OpaqueAuthenticator stores SHA-256 hashed secrets in personal_access_tokens.
type OpaqueAuthenticator struct {
	queries *dbgen.Queries
	users   UserLookup
	cfg     config.Auth
	now     func() time.Time
}

// NewOpaqueAuthenticator creates the database-backed token engine.
func NewOpaqueAuthenticator(queries *dbgen.Queries, users UserLookup, cfg config.Auth) *OpaqueAuthenticator {
	return &OpaqueAuthenticator{queries: queries, users: users, cfg: cfg, now: time.Now}
}

// AbilityTwoFactorSetup marks a setup-only token (IssueSetup).
const AbilityTwoFactorSetup = "two-factor:setup"

// Issue creates a per-device token and returns its plaintext once.
func (a *OpaqueAuthenticator) Issue(ctx context.Context, userID int64, device string) (Token, error) {
	return a.issue(ctx, userID, device, nil)
}

// IssueSetup implements Authenticator.
func (a *OpaqueAuthenticator) IssueSetup(ctx context.Context, userID int64, device string) (Token, error) {
	abilities := `["` + AbilityTwoFactorSetup + `"]`
	return a.issue(ctx, userID, device, &abilities)
}

func (a *OpaqueAuthenticator) issue(ctx context.Context, userID int64, device string, abilities *string) (Token, error) {
	secret, err := randomSecret()
	if err != nil {
		return Token{}, apperr.Wrap(apperr.InternalServerError, "generate token secret", err)
	}
	sum := sha256.Sum256([]byte(secret))

	var expiresAt pgtype.Timestamptz
	var expires *time.Time
	if a.cfg.TokenExpiration > 0 {
		t := a.now().Add(a.cfg.TokenExpiration)
		expiresAt = pgtype.Timestamptz{Time: t, Valid: true}
		expires = &t
	}

	row, err := a.queries.CreateToken(ctx, dbgen.CreateTokenParams{
		TokenableType: ModelTypeUser,
		TokenableID:   userID,
		Name:          fmt.Sprintf("user_%d_%s", userID, device),
		Device:        device,
		Token:         hex.EncodeToString(sum[:]),
		Abilities:     abilities,
		ExpiresAt:     expiresAt,
		// Where the session was opened, so its owner can recognise it
		// (GET /api/v1/sessions). Empty outside an HTTP request.
		Ip:        optionalText(httpx.ClientIP(ctx)),
		UserAgent: optionalText(truncateRunes(httpx.UserAgent(ctx), maxUserAgent)),
	})
	if err != nil {
		return Token{}, database.MapError("authn.issue_token", err)
	}
	return Token{ID: row.ID, PlainText: fmt.Sprintf("%d|%s", row.ID, secret), ExpiresAt: expires}, nil
}

// Verify resolves a raw token into an Identity.
func (a *OpaqueAuthenticator) Verify(ctx context.Context, raw string) (*Identity, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
	}
	tokenID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
	}

	row, err := a.queries.GetToken(ctx, tokenID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
		}
		return nil, database.MapError("authn.get_token", err)
	}

	sum := sha256.Sum256([]byte(parts[1]))
	expected, err := hex.DecodeString(row.Token)
	if err != nil || subtle.ConstantTimeCompare(sum[:], expected) != 1 {
		return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
	}
	if row.ExpiresAt.Valid && a.now().After(row.ExpiresAt.Time) {
		return nil, apperr.New(apperr.TokenExpired, "The access token has expired.")
	}

	record, err := a.users.FindByID(ctx, row.TokenableID)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return nil, apperr.New(apperr.TokenInvalid, "The provided access token is invalid.")
		}
		return nil, err
	}

	a.touchLastUsed(ctx, row)

	scopes, audience, setupOnly := parseAbilities(row.Abilities)
	var grantID int64
	if row.OauthGrantID != nil {
		grantID = *row.OauthGrantID
	}

	return &Identity{
		Scopes:         scopes,
		Audience:       audience,
		OAuthGrantID:   grantID,
		ExpiresAt:      row.ExpiresAt.Time,
		UserID:         record.ID,
		TokenID:        row.ID,
		Name:           record.Name,
		Email:          record.Email,
		TenantID:       record.TenantID,
		BranchID:       record.BranchID,
		ActiveBranchID: record.ActiveBranchID,
		IsActive:       record.IsActive,
		TenantActive:   record.TenantActive,
		SetupOnly:      setupOnly,
	}, nil
}

// Revoke deletes the token row for one device.
func (a *OpaqueAuthenticator) Revoke(ctx context.Context, userID int64, device string) error {
	if _, err := a.queries.DeleteTokenByDevice(ctx, dbgen.DeleteTokenByDeviceParams{
		TokenableType: ModelTypeUser,
		TokenableID:   userID,
		Device:        device,
	}); err != nil {
		return database.MapError("authn.revoke_token", err)
	}
	return nil
}

// RevokeAll deletes every token for the user (password change, ban), and
// revokes their MCP connections so a refresh token cannot bring one back.
func (a *OpaqueAuthenticator) RevokeAll(ctx context.Context, userID int64) error {
	if err := a.revokeConnections(ctx, userID); err != nil {
		return err
	}
	if _, err := a.queries.DeleteTokensForUser(ctx, dbgen.DeleteTokensForUserParams{
		TokenableType: ModelTypeUser,
		TokenableID:   userID,
	}); err != nil {
		return database.MapError("authn.revoke_all_tokens", err)
	}
	return nil
}

// RevokeOthers deletes every token of the user except keepTokenID, MCP
// connections included.
func (a *OpaqueAuthenticator) RevokeOthers(ctx context.Context, userID, keepTokenID int64) error {
	if err := a.revokeConnections(ctx, userID); err != nil {
		return err
	}
	if _, err := a.queries.DeleteOtherTokensForUser(ctx, dbgen.DeleteOtherTokensForUserParams{
		TokenableType: ModelTypeUser,
		TokenableID:   userID,
		KeepID:        keepTokenID,
	}); err != nil {
		return database.MapError("authn.revoke_other_tokens", err)
	}
	return nil
}

// revokeConnections revokes the user's OAuth grants and drops their refresh
// tokens; their access tokens go with the user's other tokens.
func (a *OpaqueAuthenticator) revokeConnections(ctx context.Context, userID int64) error {
	ids, err := a.queries.RevokeUserOAuthGrants(ctx, userID)
	if err != nil {
		return database.MapError("authn.revoke_connections", err)
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := a.queries.DeleteOAuthGrantRefreshTokens(ctx, ids); err != nil {
		return database.MapError("authn.revoke_connections", err)
	}
	if _, err := a.queries.DeleteOAuthGrantTokens(ctx, ids); err != nil {
		return database.MapError("authn.revoke_connections", err)
	}
	return nil
}

// AbilityAudiencePrefix marks the audience entry in an MCP token's abilities.
const AbilityAudiencePrefix = "aud:"

// EncodeAbilities is the abilities column of an MCP access token: its scopes
// and the resource it was issued for.
func EncodeAbilities(scopes []string, audience string) string {
	list := append(append([]string{}, scopes...), AbilityAudiencePrefix+audience)
	encoded, _ := json.Marshal(list)
	return string(encoded)
}

func parseAbilities(raw *string) (scopes []string, audience string, setupOnly bool) {
	if raw == nil || *raw == "" {
		return nil, "", false
	}
	var list []string
	if json.Unmarshal([]byte(*raw), &list) != nil {
		return nil, "", false
	}
	for _, ability := range list {
		switch {
		case ability == AbilityTwoFactorSetup:
			setupOnly = true
		case strings.HasPrefix(ability, AbilityAudiencePrefix):
			audience = strings.TrimPrefix(ability, AbilityAudiencePrefix)
		default:
			scopes = append(scopes, ability)
		}
	}
	return scopes, audience, setupOnly
}

func (a *OpaqueAuthenticator) touchLastUsed(ctx context.Context, row dbgen.PersonalAccessToken) {
	if row.LastUsedAt.Valid && a.now().Sub(row.LastUsedAt.Time) < time.Minute {
		return
	}
	_, _ = a.queries.TouchToken(ctx, dbgen.TouchTokenParams{
		ID:         row.ID,
		LastUsedAt: pgtype.Timestamptz{Time: a.now(), Valid: true},
	})
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
