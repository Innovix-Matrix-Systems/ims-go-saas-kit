// Package oauth is the OAuth 2.1 authorization server behind the MCP
// connector: a client (Claude, ChatGPT, Cursor…) sends the user to
// /oauth/authorize, the user signs in and consents, and the client gets an
// MCP access token for /mcp.
//
// Clients identify themselves with a Client ID Metadata Document (no
// registration) or are registered by an admin. One grant type
// (authorization code with PKCE S256), refresh tokens rotated on every use.
//
// Layering as everywhere: handler.go, token_handler.go and authorize.go are
// HTTP; service.go and clients_admin.go hold the rules and own transactions;
// repository.go is the only code that reads or writes through dbgen.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
)

// Activity actions written by this module.
const (
	ActionAuthorized          = "oauth.authorized"
	ActionRevoked             = "oauth.revoked"
	ActionClientCreated       = "oauth_client.created"
	ActionClientUpdated       = "oauth_client.updated"
	ActionClientSecretRotated = "oauth_client.secret_rotated"
	ActionClientDeleted       = "oauth_client.deleted"
)

const codeTTL = time.Minute

// Settings is what the service needs from the configuration.
type Settings struct {
	Issuer          string // APP_URL
	Resource        string // APP_URL + /mcp
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	SecretGrace     time.Duration
}

// Service is the authorization server's logic.
type Service struct {
	repo     Repository
	tx       *database.TxManager
	clients  *Clients
	auth     authn.Authenticator
	users    authn.UserLookup
	activity *activity.Recorder
	settings Settings
	now      func() time.Time
}

// NewService creates the service.
func NewService(repo Repository, clients *Clients, auth authn.Authenticator, users authn.UserLookup, settings Settings) *Service {
	return &Service{repo: repo, clients: clients, auth: auth, users: users, settings: settings, now: time.Now}
}

// WithTx makes multi-row writes (a grant with its code, a token pair)
// atomic.
func (s *Service) WithTx(tx *database.TxManager) *Service {
	s.tx = tx
	return s
}

// WithActivity records connections and client changes in the activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// Settings returns the service's settings.
func (s *Service) Settings() Settings { return s.settings }

// ResolveClient returns the client for client_id (CIMD or registered).
func (s *Service) ResolveClient(ctx context.Context, clientID string) (*Client, error) {
	return s.clients.Resolve(ctx, clientID)
}

// inTx runs fn on one transaction, or on the plain repository when the
// service has no transaction manager (unit tests).
func (s *Service) inTx(ctx context.Context, fn func(repo Repository) error) error {
	if s.tx == nil {
		return fn(s.repo)
	}
	return s.tx.WithTx(ctx, func(q *dbgen.Queries) error { return fn(NewRepository(q)) })
}

// ── Discovery ──────────────────────────────────────────────────────

// Metadata is the RFC 8414 authorization server metadata. Claude uses the
// client's metadata document (no registration) only when both
// client_id_metadata_document_supported and "none" are advertised.
func (s *Service) Metadata() map[string]any {
	issuer := s.settings.Issuer
	methods := []string{"none", "client_secret_basic", "client_secret_post"}
	doc := map[string]any{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + AuthorizePath,
		"token_endpoint":                                 issuer + TokenPath,
		"revocation_endpoint":                            issuer + RevokePath,
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          methods,
		"revocation_endpoint_auth_methods_supported":     methods,
		"scopes_supported":                               []string{mcp.ScopeRead, mcp.ScopeWrite},
		"authorization_response_iss_parameter_supported": true,
	}
	if s.clients.cimd {
		doc["client_id_metadata_document_supported"] = true
	}
	return doc
}

// ── Authorization ──────────────────────────────────────────────────

// Approval is what the user allowed on the consent page.
type Approval struct {
	UserID        int64
	UserTenantID  int64
	Client        *Client
	RedirectURI   string
	Scopes        []string
	TenantID      int64 // the tenant a platform account picked (0 = none)
	BranchID      int64
	CodeChallenge string
	Resource      string
}

// Approve stores the grant and returns a one-time authorization code.
func (s *Service) Approve(ctx context.Context, a Approval) (_ string, err error) {
	ctx, end := tracing.Begin(ctx, "oauth.approve")
	defer end(&err)
	code, err := randomToken()
	if err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "generate authorization code", err)
	}
	var grant *Grant
	err = s.inTx(ctx, func(repo Repository) error {
		var err error
		grant, err = repo.CreateGrant(ctx, NewGrant{
			UserID: a.UserID, ClientID: a.Client.ID, ClientName: a.Client.Name, RedirectURI: a.RedirectURI,
			Scopes: a.Scopes, TenantID: a.TenantID, BranchID: a.BranchID,
		})
		if err != nil {
			return err
		}
		return repo.CreateCode(ctx, NewCode{
			CodeHash: hashSecret(code), GrantID: grant.ID, RedirectURI: a.RedirectURI,
			CodeChallenge: a.CodeChallenge, Resource: a.Resource, ExpiresAt: s.now().Add(codeTTL),
		})
	})
	if err != nil {
		return "", err
	}

	// A tenant user's connection is in their tenant's log; a platform
	// account's is in the system log, whatever tenant it picked.
	s.activity.Record(ctx, activity.Entry{
		TenantID: a.UserTenantID, ActorID: a.UserID, Action: ActionAuthorized,
		Subject: "oauth_grant", SubjectID: grant.ID,
		Changes: map[string]any{
			"client_id": a.Client.ID, "client_name": a.Client.Name,
			"redirect_uri": a.RedirectURI, "scopes": a.Scopes,
			"tenant_id": a.TenantID, "registered": a.Client.Registered,
		},
	})
	return code, nil
}

// ── Token endpoint ─────────────────────────────────────────────────

// TokenRequest is a /oauth/token request with the client already
// authenticated.
type TokenRequest struct {
	Client       *Client
	GrantType    string
	Code         string
	RedirectURI  string
	CodeVerifier string
	RefreshToken string
	Resource     string
}

// TokenResponse is the RFC 6749 §5.1 success body.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// Error is an RFC 6749 §5.2 error.
type Error struct {
	Code        string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, description string) *Error { return &Error{Code: code, Description: description} }

// Token handles a token request by grant type.
func (s *Service) Token(ctx context.Context, req TokenRequest) (_ *TokenResponse, err error) {
	ctx, end := tracing.Begin(ctx, "oauth.token", attribute.String("oauth.grant_type", req.GrantType))
	defer end(&err)
	switch req.GrantType {
	case "authorization_code":
		return s.Exchange(ctx, req)
	case "refresh_token":
		return s.Refresh(ctx, req)
	default:
		return nil, oauthErr("unsupported_grant_type", "Use authorization_code or refresh_token.")
	}
}

// AuthenticateClient finds the client of a token request: a confidential
// registered client must present its secret, a public one must not.
func (s *Service) AuthenticateClient(ctx context.Context, clientID, secret string) (*Client, *Error) {
	if clientID == "" {
		return nil, oauthErr("invalid_client", "client_id is required.")
	}
	client, err := s.clients.Resolve(ctx, clientID)
	if err != nil {
		return nil, oauthErr("invalid_client", err.Error())
	}
	switch {
	case client.Confidential && !client.CheckSecret(secret, s.now()):
		return nil, oauthErr("invalid_client", "The client secret is not valid.")
	case !client.Confidential && secret != "":
		return nil, oauthErr("invalid_client", "A public client has no secret.")
	}
	return client, nil
}

// Exchange redeems an authorization code. A code presented a second time
// revokes the grant it made (OAuth 2.1 §4.1.3).
func (s *Service) Exchange(ctx context.Context, req TokenRequest) (_ *TokenResponse, err error) {
	ctx, end := tracing.Begin(ctx, "oauth.exchange_code")
	defer end(&err)
	if req.Code == "" || req.CodeVerifier == "" {
		return nil, oauthErr("invalid_request", "code and code_verifier are required.")
	}
	use, err := s.repo.UseCode(ctx, hashSecret(req.Code))
	if apperr.Is(err, apperr.ResourceNotFound) {
		return nil, oauthErr("invalid_grant", "The authorization code is not valid.")
	}
	if err != nil {
		return nil, err
	}
	if use.Reused {
		s.revokeGrant(ctx, use.GrantID, "code_reused")
		return nil, oauthErr("invalid_grant", "The authorization code was already used.")
	}
	if s.now().After(use.ExpiresAt) {
		return nil, oauthErr("invalid_grant", "The authorization code has expired.")
	}
	grant, err := s.repo.GetGrant(ctx, use.GrantID)
	if err != nil {
		return nil, err
	}
	switch {
	case grant.Revoked():
		return nil, oauthErr("invalid_grant", "The authorization was revoked.")
	case grant.ClientID != req.Client.ID:
		return nil, oauthErr("invalid_grant", "The code was issued to another client.")
	case req.RedirectURI != use.RedirectURI:
		return nil, oauthErr("invalid_grant", "redirect_uri does not match the authorization request.")
	case !pkceMatches(req.CodeVerifier, use.CodeChallenge):
		return nil, oauthErr("invalid_grant", "code_verifier does not match the code_challenge.")
	case req.Resource != "" && canonical(req.Resource) != use.Resource:
		return nil, oauthErr("invalid_target", "resource does not match the authorization request.")
	}
	return s.issue(ctx, grant)
}

// Refresh rotates a refresh token. A rotated token presented again is reuse:
// the grant is revoked, so a stolen copy stops working for both parties.
func (s *Service) Refresh(ctx context.Context, req TokenRequest) (_ *TokenResponse, err error) {
	ctx, end := tracing.Begin(ctx, "oauth.refresh")
	defer end(&err)
	if req.RefreshToken == "" {
		return nil, oauthErr("invalid_request", "refresh_token is required.")
	}
	if req.Resource != "" && canonical(req.Resource) != s.settings.Resource {
		return nil, oauthErr("invalid_target", "This server issues tokens only for "+s.settings.Resource+".")
	}
	use, err := s.repo.RotateRefreshToken(ctx, hashSecret(req.RefreshToken))
	if apperr.Is(err, apperr.ResourceNotFound) {
		return nil, oauthErr("invalid_grant", "The refresh token is not valid.")
	}
	if err != nil {
		return nil, err
	}
	if use.Reused {
		s.revokeGrant(ctx, use.GrantID, "refresh_token_reused")
		return nil, oauthErr("invalid_grant", "The refresh token was already used.")
	}
	if s.now().After(use.ExpiresAt) {
		return nil, oauthErr("invalid_grant", "The refresh token has expired.")
	}
	grant, err := s.repo.GetGrant(ctx, use.GrantID)
	if err != nil {
		return nil, err
	}
	if grant.Revoked() {
		return nil, oauthErr("invalid_grant", "The authorization was revoked.")
	}
	if grant.ClientID != req.Client.ID {
		return nil, oauthErr("invalid_grant", "The refresh token was issued to another client.")
	}
	return s.issue(ctx, grant)
}

// issue mints an access token (a personal access token tied to the grant)
// and a refresh token.
func (s *Service) issue(ctx context.Context, grant *Grant) (*TokenResponse, error) {
	user, err := s.users.FindByID(ctx, grant.UserID)
	if err != nil || !user.IsActive {
		s.revokeGrant(ctx, grant.ID, "user_inactive")
		return nil, oauthErr("invalid_grant", "The account is not active.")
	}

	secret, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate token", err)
	}
	refresh, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate token", err)
	}

	var tokenID int64
	err = s.inTx(ctx, func(repo Repository) error {
		var err error
		tokenID, err = repo.CreateAccessToken(ctx, NewAccessToken{
			UserID:    grant.UserID,
			GrantID:   grant.ID,
			Device:    "mcp:" + strconv.FormatInt(grant.ID, 10),
			TokenHash: hashSecret(secret),
			Abilities: authn.EncodeAbilities(grant.Scopes, s.settings.Resource),
			ExpiresAt: s.now().Add(s.settings.AccessTokenTTL),
		})
		if err != nil {
			return err
		}
		return repo.CreateRefreshToken(ctx, grant.ID, hashSecret(refresh), s.now().Add(s.settings.RefreshTokenTTL))
	})
	if err != nil {
		return nil, err
	}
	return &TokenResponse{
		AccessToken:  fmt.Sprintf("%d|%s", tokenID, secret),
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.settings.AccessTokenTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        strings.Join(grant.Scopes, " "),
	}, nil
}

// Revoke is RFC 7009: an access token is deleted; a refresh token revokes
// its whole grant. Unknown tokens and other clients' tokens are ignored.
func (s *Service) Revoke(ctx context.Context, client *Client, token string) {
	ctx, end := tracing.Begin(ctx, "oauth.revoke")
	defer end(nil)
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	if strings.Contains(token, "|") {
		identity, err := s.auth.Verify(ctx, token)
		if err == nil && identity.OAuthGrantID != 0 {
			if grant, err := s.repo.GetGrant(ctx, identity.OAuthGrantID); err == nil && grant.ClientID == client.ID {
				_ = s.repo.DropGrantTokens(ctx, []int64{grant.ID})
			}
		}
		return
	}
	use, err := s.repo.RotateRefreshToken(ctx, hashSecret(token))
	if err != nil {
		return
	}
	if grant, err := s.repo.GetGrant(ctx, use.GrantID); err == nil && grant.ClientID == client.ID {
		s.revokeGrant(ctx, grant.ID, "client_revoked")
	}
}

// ── /mcp token check ───────────────────────────────────────────────

var _ mcp.TokenVerifier = (*Service)(nil)

// VerifyMCP checks an MCP access token: an MCP token (not a session token),
// issued for this server, for an active user, on a grant still in force.
func (s *Service) VerifyMCP(ctx context.Context, raw string) (*mcp.Connection, time.Time, error) {
	identity, err := s.auth.Verify(ctx, raw)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %v", mcp.ErrInvalidToken, err)
	}
	if !identity.IsMCP() || identity.OAuthGrantID == 0 || identity.Audience != s.settings.Resource {
		return nil, time.Time{}, fmt.Errorf("%w: not an MCP token for this server", mcp.ErrInvalidToken)
	}
	if !identity.IsActive || identity.ExpiresAt.IsZero() {
		return nil, time.Time{}, fmt.Errorf("%w: inactive account or token without expiry", mcp.ErrInvalidToken)
	}
	grant, err := s.repo.GetGrant(ctx, identity.OAuthGrantID)
	if err != nil || grant.Revoked() {
		return nil, time.Time{}, fmt.Errorf("%w: connection revoked", mcp.ErrInvalidToken)
	}
	_ = s.repo.TouchGrant(ctx, grant.ID)

	return &mcp.Connection{
		Identity: identity, Client: grant.ClientName, TenantID: grant.TenantID, BranchID: grant.BranchID,
	}, identity.ExpiresAt, nil
}

// ── Connections (the user's grants) ────────────────────────────────

// Connections lists the user's grants in force.
func (s *Service) Connections(ctx context.Context, userID int64) ([]Grant, error) {
	return s.repo.ListUserGrants(ctx, userID)
}

// Disconnect revokes one of the user's grants.
func (s *Service) Disconnect(ctx context.Context, userID, grantID int64) (err error) {
	ctx, end := tracing.Begin(ctx, "oauth.disconnect",
		attribute.Int64("user.id", userID),
		attribute.Int64("grant.id", grantID),
	)
	defer end(&err)
	grant, err := s.repo.GetGrant(ctx, grantID)
	if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return err
	}
	if err != nil || grant.UserID != userID || grant.Revoked() {
		return apperr.New(apperr.ResourceNotFound, "The connection was not found.")
	}
	s.revokeGrant(ctx, grantID, "user")
	return nil
}

// revokeGrant revokes a grant, drops its tokens and records it. Already
// revoked grants only lose any tokens left.
func (s *Service) revokeGrant(ctx context.Context, grantID int64, reason string) {
	grant, err := s.repo.RevokeGrant(ctx, grantID)
	_ = s.repo.DropGrantTokens(ctx, []int64{grantID})
	if err != nil {
		return
	}
	s.activity.Record(ctx, activity.Entry{
		TenantID: s.userTenant(ctx, grant.UserID), ActorID: grant.UserID, Action: ActionRevoked,
		Subject: "oauth_grant", SubjectID: grant.ID,
		Changes: map[string]any{"client_id": grant.ClientID, "client_name": grant.ClientName, "reason": reason},
	})
}

// userTenant is where a grant's activity goes: the user's tenant, or the
// system log (0) for a platform account.
func (s *Service) userTenant(ctx context.Context, userID int64) int64 {
	if user, err := s.users.FindByID(ctx, userID); err == nil {
		return user.TenantID
	}
	return 0
}

// ConsentTenants are the tenants a platform account can pick.
func (s *Service) ConsentTenants(ctx context.Context) ([]ConsentTenant, error) {
	return s.repo.ConsentTenants(ctx)
}

// TenantPickable reports whether a platform account may pick tenantID.
func (s *Service) TenantPickable(ctx context.Context, tenantID int64) bool {
	tenants, err := s.repo.ConsentTenants(ctx)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(tenants, func(t ConsentTenant) bool { return t.ID == tenantID })
}

// FirstBranch is the branch a connection works in when a platform account
// picked a tenant.
func (s *Service) FirstBranch(ctx context.Context, tenantID int64) (int64, error) {
	id, err := s.repo.FirstBranch(ctx, tenantID)
	if apperr.Is(err, apperr.ResourceNotFound) {
		return 0, apperr.New(apperr.ResourceNotFound, "The tenant has no branch.")
	}
	return id, err
}

// ── helpers ────────────────────────────────────────────────────────

// GrantScopes is what a grant gets: read, plus write when the user allowed
// changes, capped by a registered client's maximum.
func GrantScopes(client *Client, allowWrite bool) []string {
	scopes := []string{mcp.ScopeRead}
	if allowWrite {
		scopes = append(scopes, mcp.ScopeWrite)
	}
	if client.Registered && len(client.MaxScopes) > 0 {
		scopes = slices.DeleteFunc(scopes, func(s string) bool { return !slices.Contains(client.MaxScopes, s) })
	}
	return scopes
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

// canonical is a resource URI as compared here: no trailing slash.
func canonical(uri string) string { return strings.TrimRight(strings.TrimSpace(uri), "/") }

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
