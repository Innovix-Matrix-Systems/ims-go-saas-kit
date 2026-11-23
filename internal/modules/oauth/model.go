package oauth

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// ClientRecord is a client an admin registered (oauth_clients).
type ClientRecord struct {
	ID                      int64
	ClientID                string
	Name                    string
	Type                    string // TypePublic or TypeConfidential
	SecretHash              string
	PreviousSecretHash      string
	PreviousSecretExpiresAt *time.Time
	RedirectURIs            []string
	Scopes                  []string
	TenantID                int64 // 0 = any tenant's users
	IsActive                bool
	CreatedBy               int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// Grant is one connection: a user allowed a client, with scopes, in a
// tenant (oauth_grants).
type Grant struct {
	ID          int64
	UserID      int64
	ClientID    string
	ClientName  string
	RedirectURI string
	Scopes      []string
	TenantID    int64 // the tenant a platform account picked (0 = none)
	BranchID    int64
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// Revoked reports whether the connection was revoked.
func (g *Grant) Revoked() bool { return g.RevokedAt != nil }

// CodeUse is an authorization code being redeemed.
type CodeUse struct {
	GrantID       int64
	RedirectURI   string
	CodeChallenge string
	Resource      string
	ExpiresAt     time.Time
	// Reused is true when the code had been redeemed before.
	Reused bool
}

// RefreshUse is a refresh token being rotated.
type RefreshUse struct {
	GrantID   int64
	ExpiresAt time.Time
	// Reused is true when the token had been rotated before.
	Reused bool
}

// ConsentTenant is a tenant a platform account can pick on the consent page.
type ConsentTenant struct {
	ID   int64
	Name string
	Code string
}

func mapClient(row dbgen.OauthClient) ClientRecord {
	return ClientRecord{
		ID:                      row.ID,
		ClientID:                row.ClientID,
		Name:                    row.Name,
		Type:                    row.Type,
		SecretHash:              deref(row.SecretHash),
		PreviousSecretHash:      deref(row.PreviousSecretHash),
		PreviousSecretExpiresAt: timePtr(row.PreviousSecretExpiresAt),
		RedirectURIs:            row.RedirectUris,
		Scopes:                  row.Scopes,
		TenantID:                derefInt(row.TenantID),
		IsActive:                row.IsActive,
		CreatedBy:               derefInt(row.CreatedBy),
		CreatedAt:               row.CreatedAt.Time,
		UpdatedAt:               row.UpdatedAt.Time,
	}
}

func mapGrant(row dbgen.OauthGrant) Grant {
	return Grant{
		ID:          row.ID,
		UserID:      row.UserID,
		ClientID:    row.ClientID,
		ClientName:  row.ClientName,
		RedirectURI: row.RedirectUri,
		Scopes:      row.Scopes,
		TenantID:    derefInt(row.TenantID),
		BranchID:    derefInt(row.BranchID),
		CreatedAt:   row.CreatedAt.Time,
		LastUsedAt:  timePtr(row.LastUsedAt),
		RevokedAt:   timePtr(row.RevokedAt),
	}
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func optional(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
