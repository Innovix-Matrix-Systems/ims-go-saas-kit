package authz

import (
	"net/http"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

// Require gates a route on a permission — the equivalent of Laravel's
// `can:` middleware. Permission checks live at the HTTP edge (this middleware
// or MustCan at the top of a handler); services enforce tenancy and
// business invariants, not permissions.
func Require(checker *Checker, permission string, responder *httpx.Responder) func(http.Handler) http.Handler {
	return RequireAny(checker, responder, permission)
}

// RequireAny gates a route on holding at least one of the permissions, for
// routes whose permissions select what the caller sees (activity.view for one
// tenant, activity.view.all for every tenant and the system log).
func RequireAny(checker *Checker, responder *httpx.Responder, permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return &Gate{checker: checker, permissions: permissions, responder: responder, next: next}
	}
}

// Gate is the handler Require wraps a route with. It is a named type so
// tooling (the dev dashboard's route inspector) can read which permission a
// route needs.
type Gate struct {
	checker     *Checker
	permissions []string
	responder   *httpx.Responder
	next        http.Handler
}

// Permission is the permission this gate requires ("a or b" for RequireAny).
func (g *Gate) Permission() string { return strings.Join(g.permissions, " or ") }

// Next is the wrapped handler.
func (g *Gate) Next() http.Handler { return g.next }

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		g.responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return
	}
	for _, permission := range g.permissions {
		allowed, err := g.checker.Can(r.Context(), identity.UserID, permission)
		if err != nil {
			g.responder.Error(w, r, err)
			return
		}
		if allowed {
			g.next.ServeHTTP(w, r)
			return
		}
	}
	g.responder.Error(w, r, apperr.New(apperr.InsufficientPermissions, "You do not have permission to perform this action."))
}
