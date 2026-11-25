// Package featureflags provides the `feature:<key>` gate middleware. It depends
// on a minimal resolver so it stays free of business-package imports.
package featureflags

import (
	"context"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// Resolver resolves a feature flag for the caller.
type Resolver interface {
	IsEnabled(ctx context.Context, scope tenancy.Scope, userID int64, key string) (bool, error)
}

// Require gates a route on a feature flag. A disabled flag returns 403 with the
// flag key in the problem `meta` (mirroring the reference `feature_flag` field).
func Require(resolver Resolver, key string, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := authn.IdentityFromContext(r.Context())
			if !ok {
				responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
				return
			}
			scope, _ := tenancy.FromContext(r.Context())
			enabled, err := resolver.IsEnabled(r.Context(), scope, identity.UserID, key)
			if err != nil {
				responder.Error(w, r, err)
				return
			}
			if !enabled {
				err := apperr.New(apperr.Forbidden, "This feature is currently disabled.").
					WithMeta(map[string]any{"feature_flag": key})
				responder.Error(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
