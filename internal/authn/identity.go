// Package authn implements authentication primitives: opaque personal access
// tokens behind the Authenticator interface, OTP flows, login throttling and
// the bearer middleware.
package authn

import (
	"context"
	"net/http"
	"time"
)

// Identity is the authenticated caller established by the bearer middleware.
type Identity struct {
	UserID         int64
	TokenID        int64
	Name           string
	Email          string
	TenantID       int64
	BranchID       int64
	ActiveBranchID int64
	IsActive       bool
	// TenantActive is false when the user's tenant is suspended (inactive)
	// or deleted; always true for tenantless platform accounts.
	TenantActive bool
	// SetupOnly marks a setup-only token: the user must turn on two-factor
	// authentication, and only the session routes accept the token.
	SetupOnly bool

	// ImpersonatorID is set when another user is acting as this one
	// (X-Acting-User). UserID and every field above are then the target's;
	// ImpersonatorID and ImpersonatorName are the person really calling.
	ImpersonatorID   int64
	ImpersonatorName string

	// Scopes are set only for an MCP access token (mcp:read, mcp:write);
	// Audience is the resource it was issued for and OAuthGrantID the
	// connection it belongs to. Session tokens leave all three empty.
	Scopes       []string
	Audience     string
	OAuthGrantID int64
	// ExpiresAt is when the token expires (zero = never).
	ExpiresAt time.Time
}

// IsMCP reports whether the identity came from an MCP access token. Such
// tokens are accepted only on /mcp; Authenticate refuses them elsewhere.
func (i *Identity) IsMCP() bool { return i != nil && len(i.Scopes) > 0 }

// HasScope reports whether an MCP token carries scope.
func (i *Identity) HasScope(scope string) bool {
	if i == nil {
		return false
	}
	for _, s := range i.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Impersonated reports whether this identity is acted as by someone else.
func (i *Identity) Impersonated() bool { return i != nil && i.ImpersonatorID != 0 }

// TenantSuspended reports whether the caller belongs to a tenant that may not
// work right now (inactive or deleted).
func (i *Identity) TenantSuspended() bool {
	return i != nil && i.TenantID != 0 && !i.TenantActive
}

// EffectiveBranchID returns the branch the user is currently acting in.
func (i *Identity) EffectiveBranchID() int64 {
	if i.ActiveBranchID != 0 {
		return i.ActiveBranchID
	}
	return i.BranchID
}

// UserRecord is the user data the authentication engine needs.
type UserRecord struct {
	ID             int64
	Name           string
	Email          string
	Password       string
	IsActive       bool
	EmailVerified  bool
	PhoneVerified  bool
	TenantID       int64
	BranchID       int64
	ActiveBranchID int64
	// TenantActive: see Identity.TenantActive.
	TenantActive bool
}

// UserLookup is the persistence contract the auth engine requires. It is
// implemented by the identity module's repository.
type UserLookup interface {
	FindByEmail(ctx context.Context, email string) (*UserRecord, error)
	FindByID(ctx context.Context, id int64) (*UserRecord, error)
	TouchLogin(ctx context.Context, userID int64, device string) error
}

type contextKey string

const (
	identityKey   contextKey = "authn_identity"
	dispatchedKey contextKey = "authn_dispatched"
)

// Dispatched marks ctx as an in-process sub-request made for an MCP tool call
// on behalf of identity. Authenticate then uses that identity instead of
// reading Authorization: the MCP token never travels with the sub-request.
func Dispatched(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, dispatchedKey, identity)
}

func dispatchedIdentity(ctx context.Context) (*Identity, bool) {
	identity, ok := ctx.Value(dispatchedKey).(*Identity)
	return identity, ok && identity != nil
}

// WithIdentity stores the authenticated identity in the context.
func WithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

// IdentityFromContext returns the authenticated identity, if any.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	identity, ok := ctx.Value(identityKey).(*Identity)
	return identity, ok
}

// UserID returns the authenticated user's id, or 0 for a guest (Laravel's
// auth()->id()).
func UserID(r *http.Request) int64 {
	return UserIDFromContext(r.Context())
}

// UserIDFromContext is UserID for code that only has a context (services).
func UserIDFromContext(ctx context.Context) int64 {
	if identity, ok := IdentityFromContext(ctx); ok && identity != nil {
		return identity.UserID
	}
	return 0
}
