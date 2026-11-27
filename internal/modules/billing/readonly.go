package billing

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// ReadOnlyReason is the meta reason of the 402 a read-only organization gets.
const ReadOnlyReason = "read_only"

// ReadOnlyChecker tells whether a tenant is read-only (a paid plan lapsed
// beyond its grace period). *service.Guard implements it.
type ReadOnlyChecker interface {
	ReadOnly(ctx context.Context, tenantID int64) (bool, error)
}

// ReadOnlyOptions configures ReadOnly.
type ReadOnlyOptions struct {
	// Allow are the writes a read-only organization may still make, named
	// as chi routes them ("POST /api/v1/billing/subscribe",
	// "DELETE /api/v1/user/{id}"), like dedupe's Skip. Everything else that
	// writes is refused, so a module added later is covered without being
	// listed: no lock-out means reading, copying and exporting, not adding.
	Allow []string
}

// ReadOnly refuses writes from a read-only organization with 402
// SUBSCRIPTION_REQUIRED (meta.reason "read_only"), except opts.Allow
// (docs/guides/billing.md#grace-and-read-only). It runs after
// tenancy.Resolve. Reads, requests without a tenant and platform staff acting
// inside an organization pass.
func ReadOnly(checker ReadOnlyChecker, responder *httpx.Responder, opts ReadOnlyOptions) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(opts.Allow))
	for _, route := range opts.Allow {
		allowed[route] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				next.ServeHTTP(w, r)
				return
			}
			scope := tenancy.From(r)
			tenantID := scope.EffectiveTenantID()
			route := r.Method + " " + chi.RouteContext(r.Context()).RoutePattern()
			if tenantID == 0 || scope.IsImpersonating() || allowed[route] {
				next.ServeHTTP(w, r)
				return
			}
			readOnly, err := checker.ReadOnly(r.Context(), tenantID)
			if err != nil {
				responder.Error(w, r, err)
				return
			}
			if readOnly {
				responder.Error(w, r, apperr.New(apperr.SubscriptionRequired,
					"Your plan has lapsed, so the organization is read-only. Choose a plan to make changes again.").
					WithMeta(map[string]any{"reason": ReadOnlyReason}))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
