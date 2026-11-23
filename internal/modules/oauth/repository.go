package oauth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Repository is the authorization server's persistence. Not-found reads
// answer apperr.ResourceNotFound.
type Repository interface {
	// Registered clients.
	CreateClient(ctx context.Context, in NewClient) (*ClientRecord, error)
	GetClient(ctx context.Context, id int64) (*ClientRecord, error)
	FindClient(ctx context.Context, clientID string) (*ClientRecord, error)
	ListClients(ctx context.Context, search string, limit, offset int32) ([]ClientRecord, error)
	CountClients(ctx context.Context, search string) (int64, error)
	UpdateClient(ctx context.Context, in ClientUpdate) (*ClientRecord, error)
	RotateClientSecret(ctx context.Context, id int64, secretHash string, previousExpires time.Time) (*ClientRecord, error)
	DeleteClient(ctx context.Context, id int64) error

	// Grants (connections).
	CreateGrant(ctx context.Context, in NewGrant) (*Grant, error)
	GetGrant(ctx context.Context, id int64) (*Grant, error)
	ListUserGrants(ctx context.Context, userID int64) ([]Grant, error)
	TouchGrant(ctx context.Context, id int64) error
	// RevokeGrant revokes a grant in force and returns it; a grant already
	// revoked (or gone) answers ResourceNotFound.
	RevokeGrant(ctx context.Context, id int64) (*Grant, error)
	RevokeClientGrants(ctx context.Context, clientID string) ([]int64, error)
	// DropGrantTokens deletes the access and refresh tokens of grants.
	DropGrantTokens(ctx context.Context, grantIDs []int64) error

	// Codes and tokens.
	CreateCode(ctx context.Context, in NewCode) error
	UseCode(ctx context.Context, codeHash string) (*CodeUse, error)
	CreateAccessToken(ctx context.Context, in NewAccessToken) (int64, error)
	CreateRefreshToken(ctx context.Context, grantID int64, tokenHash string, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, tokenHash string) (*RefreshUse, error)

	// Consent page.
	ConsentTenants(ctx context.Context) ([]ConsentTenant, error)
	FirstBranch(ctx context.Context, tenantID int64) (int64, error)
}

// NewClient is a client to register.
type NewClient struct {
	ClientID     string
	Name         string
	Type         string
	SecretHash   string // empty for a public client
	RedirectURIs []string
	Scopes       []string
	TenantID     int64
	CreatedBy    int64
}

// ClientUpdate is a registered client's new editable fields.
type ClientUpdate struct {
	ID           int64
	Name         string
	RedirectURIs []string
	Scopes       []string
	TenantID     int64
	IsActive     bool
}

// NewGrant is a connection to store.
type NewGrant struct {
	UserID      int64
	ClientID    string
	ClientName  string
	RedirectURI string
	Scopes      []string
	TenantID    int64
	BranchID    int64
}

// NewCode is an authorization code to store (hashed).
type NewCode struct {
	CodeHash      string
	GrantID       int64
	RedirectURI   string
	CodeChallenge string
	Resource      string
	ExpiresAt     time.Time
}

// NewAccessToken is an MCP access token to store (hashed).
type NewAccessToken struct {
	UserID    int64
	GrantID   int64
	Device    string
	TokenHash string
	Abilities string
	ExpiresAt time.Time
}

type repository struct {
	q *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository { return &repository{q: queries} }

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.New(apperr.ResourceNotFound, "The "+what+" was not found.")
	}
	return database.MapError("oauth."+strings.ReplaceAll(what, " ", "_"), err)
}

func searchFilter(search string) *string {
	if search = strings.TrimSpace(search); search == "" {
		return nil
	}
	return &search
}

func (r *repository) CreateClient(ctx context.Context, in NewClient) (*ClientRecord, error) {
	row, err := r.q.CreateOAuthClient(ctx, dbgen.CreateOAuthClientParams{
		ClientID:     in.ClientID,
		Name:         in.Name,
		Type:         in.Type,
		SecretHash:   optionalString(in.SecretHash),
		RedirectUris: in.RedirectURIs,
		Scopes:       in.Scopes,
		TenantID:     optional(in.TenantID),
		IsActive:     true,
		CreatedBy:    optional(in.CreatedBy),
	})
	if err != nil {
		return nil, database.MapError("oauth.create_client", err)
	}
	client := mapClient(row)
	return &client, nil
}

func (r *repository) GetClient(ctx context.Context, id int64) (*ClientRecord, error) {
	row, err := r.q.GetOAuthClient(ctx, id)
	if err != nil {
		return nil, notFound(err, "OAuth client")
	}
	client := mapClient(row)
	return &client, nil
}

func (r *repository) FindClient(ctx context.Context, clientID string) (*ClientRecord, error) {
	row, err := r.q.GetOAuthClientByClientID(ctx, clientID)
	if err != nil {
		return nil, notFound(err, "OAuth client")
	}
	client := mapClient(row)
	return &client, nil
}

func (r *repository) ListClients(ctx context.Context, search string, limit, offset int32) ([]ClientRecord, error) {
	rows, err := r.q.ListOAuthClients(ctx, dbgen.ListOAuthClientsParams{Search: searchFilter(search), RowLimit: limit, RowOffset: offset})
	if err != nil {
		return nil, database.MapError("oauth.list_clients", err)
	}
	out := make([]ClientRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapClient(row))
	}
	return out, nil
}

func (r *repository) CountClients(ctx context.Context, search string) (int64, error) {
	total, err := r.q.CountOAuthClients(ctx, searchFilter(search))
	if err != nil {
		return 0, database.MapError("oauth.count_clients", err)
	}
	return total, nil
}

func (r *repository) UpdateClient(ctx context.Context, in ClientUpdate) (*ClientRecord, error) {
	row, err := r.q.UpdateOAuthClient(ctx, dbgen.UpdateOAuthClientParams{
		ID: in.ID, Name: in.Name, RedirectUris: in.RedirectURIs, Scopes: in.Scopes,
		TenantID: optional(in.TenantID), IsActive: in.IsActive,
	})
	if err != nil {
		return nil, notFound(err, "OAuth client")
	}
	client := mapClient(row)
	return &client, nil
}

func (r *repository) RotateClientSecret(ctx context.Context, id int64, secretHash string, previousExpires time.Time) (*ClientRecord, error) {
	row, err := r.q.RotateOAuthClientSecret(ctx, dbgen.RotateOAuthClientSecretParams{
		ID: id, SecretHash: &secretHash, PreviousExpiresAt: timestamptz(previousExpires),
	})
	if err != nil {
		return nil, notFound(err, "OAuth client")
	}
	client := mapClient(row)
	return &client, nil
}

func (r *repository) DeleteClient(ctx context.Context, id int64) error {
	if _, err := r.q.DeleteOAuthClient(ctx, id); err != nil {
		return database.MapError("oauth.delete_client", err)
	}
	return nil
}

func (r *repository) CreateGrant(ctx context.Context, in NewGrant) (*Grant, error) {
	row, err := r.q.CreateOAuthGrant(ctx, dbgen.CreateOAuthGrantParams{
		UserID: in.UserID, ClientID: in.ClientID, ClientName: in.ClientName, RedirectUri: in.RedirectURI,
		Scopes: in.Scopes, TenantID: optional(in.TenantID), BranchID: optional(in.BranchID),
	})
	if err != nil {
		return nil, database.MapError("oauth.create_grant", err)
	}
	grant := mapGrant(row)
	return &grant, nil
}

func (r *repository) GetGrant(ctx context.Context, id int64) (*Grant, error) {
	row, err := r.q.GetOAuthGrant(ctx, id)
	if err != nil {
		return nil, notFound(err, "connection")
	}
	grant := mapGrant(row)
	return &grant, nil
}

func (r *repository) ListUserGrants(ctx context.Context, userID int64) ([]Grant, error) {
	rows, err := r.q.ListUserOAuthGrants(ctx, userID)
	if err != nil {
		return nil, database.MapError("oauth.list_grants", err)
	}
	out := make([]Grant, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapGrant(row))
	}
	return out, nil
}

func (r *repository) TouchGrant(ctx context.Context, id int64) error {
	if err := r.q.TouchOAuthGrant(ctx, id); err != nil {
		return database.MapError("oauth.touch_grant", err)
	}
	return nil
}

func (r *repository) RevokeGrant(ctx context.Context, id int64) (*Grant, error) {
	row, err := r.q.RevokeOAuthGrant(ctx, id)
	if err != nil {
		return nil, notFound(err, "connection")
	}
	grant := mapGrant(row)
	return &grant, nil
}

func (r *repository) RevokeClientGrants(ctx context.Context, clientID string) ([]int64, error) {
	ids, err := r.q.RevokeClientOAuthGrants(ctx, clientID)
	if err != nil {
		return nil, database.MapError("oauth.revoke_client_grants", err)
	}
	return ids, nil
}

func (r *repository) DropGrantTokens(ctx context.Context, grantIDs []int64) error {
	if len(grantIDs) == 0 {
		return nil
	}
	if _, err := r.q.DeleteOAuthGrantRefreshTokens(ctx, grantIDs); err != nil {
		return database.MapError("oauth.drop_refresh_tokens", err)
	}
	if _, err := r.q.DeleteOAuthGrantTokens(ctx, grantIDs); err != nil {
		return database.MapError("oauth.drop_access_tokens", err)
	}
	return nil
}

func (r *repository) CreateCode(ctx context.Context, in NewCode) error {
	if err := r.q.CreateOAuthCode(ctx, dbgen.CreateOAuthCodeParams{
		CodeHash: in.CodeHash, GrantID: in.GrantID, RedirectUri: in.RedirectURI,
		CodeChallenge: in.CodeChallenge, Resource: in.Resource, ExpiresAt: timestamptz(in.ExpiresAt),
	}); err != nil {
		return database.MapError("oauth.create_code", err)
	}
	return nil
}

func (r *repository) UseCode(ctx context.Context, codeHash string) (*CodeUse, error) {
	row, err := r.q.UseOAuthCode(ctx, codeHash)
	if err != nil {
		return nil, notFound(err, "authorization code")
	}
	return &CodeUse{
		GrantID: row.GrantID, RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge,
		Resource: row.Resource, ExpiresAt: row.ExpiresAt.Time, Reused: row.FirstUsedAt.Valid,
	}, nil
}

func (r *repository) CreateAccessToken(ctx context.Context, in NewAccessToken) (int64, error) {
	row, err := r.q.CreateOAuthAccessToken(ctx, dbgen.CreateOAuthAccessTokenParams{
		UserID: in.UserID, Device: in.Device, Token: in.TokenHash, Abilities: &in.Abilities,
		ExpiresAt: timestamptz(in.ExpiresAt), GrantID: &in.GrantID,
	})
	if err != nil {
		return 0, database.MapError("oauth.create_access_token", err)
	}
	return row.ID, nil
}

func (r *repository) CreateRefreshToken(ctx context.Context, grantID int64, tokenHash string, expiresAt time.Time) error {
	if _, err := r.q.CreateOAuthRefreshToken(ctx, dbgen.CreateOAuthRefreshTokenParams{
		TokenHash: tokenHash, GrantID: grantID, ExpiresAt: timestamptz(expiresAt),
	}); err != nil {
		return database.MapError("oauth.create_refresh_token", err)
	}
	return nil
}

func (r *repository) RotateRefreshToken(ctx context.Context, tokenHash string) (*RefreshUse, error) {
	row, err := r.q.RotateOAuthRefreshToken(ctx, tokenHash)
	if err != nil {
		return nil, notFound(err, "refresh token")
	}
	return &RefreshUse{GrantID: row.GrantID, ExpiresAt: row.ExpiresAt.Time, Reused: row.FirstRotatedAt.Valid}, nil
}

func (r *repository) ConsentTenants(ctx context.Context) ([]ConsentTenant, error) {
	rows, err := r.q.ListOAuthConsentTenants(ctx)
	if err != nil {
		return nil, database.MapError("oauth.consent_tenants", err)
	}
	out := make([]ConsentTenant, 0, len(rows))
	for _, row := range rows {
		out = append(out, ConsentTenant{ID: row.ID, Name: row.Name, Code: row.Code})
	}
	return out, nil
}

func (r *repository) FirstBranch(ctx context.Context, tenantID int64) (int64, error) {
	row, err := r.q.FirstBranchOfTenant(ctx, tenantID)
	if err != nil {
		return 0, notFound(err, "branch")
	}
	return row.ID, nil
}
