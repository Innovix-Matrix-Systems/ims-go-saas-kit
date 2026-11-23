package authn

import (
	"net/http"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Authenticate resolves the bearer token into an Identity in the context.
func Authenticate(auth Authenticator, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// An MCP tool call's sub-request: the /mcp endpoint already
			// verified the token and placed the identity here.
			if identity, ok := dispatchedIdentity(r.Context()); ok {
				httpx.NoteUser(r.Context(), identity.UserID, 0)
				next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
				return
			}

			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				responder.Error(w, r, apperr.New(apperr.TokenMissing, "Authentication is required."))
				return
			}
			raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			if raw == "" {
				responder.Error(w, r, apperr.New(apperr.TokenMissing, "Authentication is required."))
				return
			}

			identity, err := auth.Verify(r.Context(), raw)
			if err != nil {
				responder.Error(w, r, err)
				return
			}
			// An MCP access token works only on /mcp: a leaked agent token
			// cannot call the REST API or the dashboard.
			if identity.IsMCP() {
				responder.Error(w, r, apperr.New(apperr.TokenInvalid, "The provided access token is invalid."))
				return
			}
			if !identity.IsActive {
				responder.Error(w, r, apperr.New(apperr.AccountDeactivated, "Your account is deactivated."))
				return
			}
			httpx.NoteUser(r.Context(), identity.UserID, 0)
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}

// ErrTenantSuspended is the error for a caller whose tenant is suspended
// (inactive) or deleted. Sign-in returns it after the password matches;
// tenancy.Resolve and tenancy.Exempt return it on every authenticated route
// except the session routes (logout stays open).
func ErrTenantSuspended() *apperr.Error {
	return apperr.New(apperr.TenantSuspended, "This organization's account is suspended.")
}

// ErrTwoFactorSetupRequired is the error for a setup-only token anywhere but
// the session routes: the user must turn on two-factor authentication first.
func ErrTwoFactorSetupRequired() *apperr.Error {
	return apperr.New(apperr.TwoFactorSetupRequired, "Turn on two-factor authentication to continue.")
}

// DenyImpersonated refuses a route while another user is acting as the
// caller. Mount it on endpoints that change the account itself (password,
// profile, avatar): acting as a user is for doing their work, not for
// taking over their account.
func DenyImpersonated(responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if identity, ok := IdentityFromContext(r.Context()); ok && identity.Impersonated() {
				responder.Error(w, r, apperr.New(apperr.ImpersonationForbidden, "This action is not available while acting as another user."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
