package tenancy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// CheckTTL is how long tenant/branch existence checks are cached.
const CheckTTL = 5 * time.Minute

// ResolveOptions configures tenant resolution.
type ResolveOptions struct {
	Queries   *dbgen.Queries
	Cache     cache.Store
	Checker   *authz.Checker
	Responder *httpx.Responder
	// Audit records act-as (X-Acting-User) in the system audit log. Nil
	// records nothing (unit tests).
	Audit ImpersonationAudit
}

// Resolve determines the effective tenant/branch scope for authenticated
// requests, honoring impersonation headers and cached membership checks.
// X-Acting-User runs the request as another user (acting_user.go);
// X-Acting-Tenant / X-Acting-Branch keep the caller and change the tenant.
func Resolve(opts ResolveOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			identity, ok := authn.IdentityFromContext(ctx)
			if !ok {
				opts.Responder.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
				return
			}
			// A suspended tenant's users are refused here, before any scope is
			// resolved. The caller's own tenant counts: a platform account
			// acting inside a suspended tenant (support) passes.
			if identity.TenantSuspended() {
				opts.Responder.Error(w, r, authn.ErrTenantSuspended())
				return
			}
			// A setup-only token (two-factor required, not yet on) reaches
			// only the session routes, which run without tenancy.
			if identity.SetupOnly {
				opts.Responder.Error(w, r, authn.ErrTwoFactorSetupRequired())
				return
			}

			if raw := r.Header.Get(ActingUserHeader); raw != "" {
				actAsUser(opts, next, w, r, identity, raw)
				return
			}

			canImpersonate, err := opts.Checker.Can(ctx, identity.UserID, authz.PermUserTenantSwitch)
			if err != nil {
				opts.Responder.Error(w, r, err)
				return
			}

			if canImpersonate {
				actingTenant := parseIDHeader(r.Header.Get("X-Acting-Tenant"))
				actingBranch := parseIDHeader(r.Header.Get("X-Acting-Branch"))

				switch {
				case actingTenant == 0 && actingBranch == 0:
					if isWriteMethod(r.Method) {
						opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "Impersonating writes require the X-Acting-Tenant and X-Acting-Branch headers."))
						return
					}
					serve(next, w, r, Scope{Acting: ActingGlobal})
				case actingTenant == 0:
					opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "X-Acting-Tenant is required when X-Acting-Branch is provided."))
				default:
					exists, err := tenantExists(ctx, opts, actingTenant)
					if err != nil {
						opts.Responder.Error(w, r, err)
						return
					}
					if !exists {
						opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The acting tenant does not exist."))
						return
					}
					if actingBranch != 0 {
						belongs, err := branchBelongsToTenant(ctx, opts, actingTenant, actingBranch)
						if err != nil {
							opts.Responder.Error(w, r, err)
							return
						}
						if !belongs {
							opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The selected branch does not belong to the tenant."))
							return
						}
					}
					scope := Scope{
						Acting:         ActingBranch,
						ActingTenantID: actingTenant,
						ActingBranchID: actingBranch,
					}
					if actingBranch == 0 {
						scope.Acting = ActingTenant
					}
					serve(next, w, r, scope)
				}
				return
			}

			resolveTenantUser(opts, next, w, r, identity)
		})
	}
}

// resolveTenantUser scopes a request to the identity's own tenant and active
// branch: the path for tenant users, and for anyone acting as one.
func resolveTenantUser(opts ResolveOptions, next http.Handler, w http.ResponseWriter, r *http.Request, identity *authn.Identity) {
	tenantID := identity.TenantID
	if tenantID == 0 {
		opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "No tenant is assigned to this account."))
		return
	}
	branchID := identity.EffectiveBranchID()
	if branchID == 0 {
		opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "No active branch is assigned to this account."))
		return
	}
	belongs, err := branchBelongsToTenant(r.Context(), opts, tenantID, branchID)
	if err != nil {
		opts.Responder.Error(w, r, err)
		return
	}
	if !belongs {
		opts.Responder.Error(w, r, apperr.New(apperr.Forbidden, "The assigned branch does not belong to the tenant."))
		return
	}
	allBranches, err := seesAllBranches(r.Context(), opts, identity)
	if err != nil {
		opts.Responder.Error(w, r, err)
		return
	}
	serve(next, w, r, Scope{TenantID: tenantID, BranchID: branchID, AllBranches: allBranches})
}

// seesAllBranches reports whether a member's lists span every branch: they
// have not switched to one and may switch (view every branch and switch).
func seesAllBranches(ctx context.Context, opts ResolveOptions, identity *authn.Identity) (bool, error) {
	if identity.ActiveBranchID != 0 || opts.Checker == nil {
		return false, nil
	}
	for _, permission := range []string{authz.PermBranchViewAll, authz.PermUserBranchSwitch} {
		allowed, err := opts.Checker.Can(ctx, identity.UserID, permission)
		if err != nil || !allowed {
			return false, err
		}
	}
	return true, nil
}

// Exempt marks requests that operate outside tenant scoping (global/internal
// endpoints such as tenant management or permission administration).
//
// X-Acting-User is refused here (422): exempt routes are not resolved per
// tenant, so the request would otherwise silently run as the caller.
func Exempt(responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(ActingUserHeader) != "" {
				responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "Acting as a user is only available on tenant routes."))
				return
			}
			if identity, ok := authn.IdentityFromContext(r.Context()); ok {
				if identity.TenantSuspended() {
					responder.Error(w, r, authn.ErrTenantSuspended())
					return
				}
				if identity.SetupOnly {
					responder.Error(w, r, authn.ErrTwoFactorSetupRequired())
					return
				}
			}
			scope := Scope{Internal: true}
			if existing, ok := FromContext(r.Context()); ok {
				scope = existing
				scope.Internal = true
			}
			next.ServeHTTP(w, r.WithContext(WithScope(r.Context(), scope)))
		})
	}
}

// InternalOnly rejects requests from identities that belong to a tenant.
// Mounted (per route family) inside the tenant-exempt group so a tenant-bound
// caller can never reach cross-tenant handlers even with a granted system
// permission: "internal" is a property of the account, not something a route
// group can assert. Tenantless platform accounts pass through — including
// impersonating super-admins, whose own identities are tenantless. Route
// families that deliberately serve tenant-bound callers (files, permission
// metadata, self-service profile handlers) must not mount this gate.
func InternalOnly(responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if identity, ok := authn.IdentityFromContext(r.Context()); ok && identity.TenantID != 0 {
				responder.Error(w, r, apperr.New(apperr.Forbidden, "This endpoint is restricted to internal accounts."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BranchCheckKey namespaces cached branch-membership checks.
func BranchCheckKey(tenantID, branchID int64) string {
	return fmt.Sprintf("branch_check:%d:%d", branchID, tenantID)
}

// InvalidateBranchCheck drops a cached membership check after branch mutation.
func InvalidateBranchCheck(ctx context.Context, store cache.Store, tenantID, branchID int64) {
	_ = store.Delete(ctx, BranchCheckKey(tenantID, branchID))
	// Public routes cache the branch name and the tenant's first branch.
	_ = store.Delete(ctx, publicBranchKey(tenantID, branchID))
	_ = store.Delete(ctx, firstBranchKey(tenantID))
}

// InvalidateTenantCheck drops a cached tenant-existence check after a tenant is
// created, updated or deleted.
func InvalidateTenantCheck(ctx context.Context, store cache.Store, tenantID int64) {
	_ = store.Delete(ctx, tenantCheckKey(tenantID))
}

func tenantCheckKey(tenantID int64) string {
	return fmt.Sprintf("tenant_check:%d", tenantID)
}

func serve(next http.Handler, w http.ResponseWriter, r *http.Request, scope Scope) {
	httpx.NoteScope(r.Context(), scope.EffectiveTenantID(), scope.EffectiveBranchID())
	next.ServeHTTP(w, r.WithContext(WithScope(r.Context(), scope)))
}

func branchBelongsToTenant(ctx context.Context, opts ResolveOptions, tenantID, branchID int64) (bool, error) {
	key := BranchCheckKey(tenantID, branchID)
	var cached bool
	found, err := opts.Cache.Get(ctx, key, &cached)
	if err != nil {
		return false, apperr.Wrap(apperr.InternalServerError, "read branch check cache", err)
	}
	if found {
		return cached, nil
	}
	belongs, err := opts.Queries.BranchBelongsToTenant(ctx, dbgen.BranchBelongsToTenantParams{
		BranchID: branchID,
		TenantID: tenantID,
	})
	if err != nil {
		return false, err
	}
	_ = opts.Cache.Set(ctx, key, belongs, CheckTTL)
	return belongs, nil
}

func tenantExists(ctx context.Context, opts ResolveOptions, tenantID int64) (bool, error) {
	key := tenantCheckKey(tenantID)
	var cached bool
	found, err := opts.Cache.Get(ctx, key, &cached)
	if err != nil {
		return false, apperr.Wrap(apperr.InternalServerError, "read tenant check cache", err)
	}
	if found {
		return cached, nil
	}
	_, err = opts.Queries.GetTenant(ctx, tenantID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	exists := err == nil
	_ = opts.Cache.Set(ctx, key, exists, CheckTTL)
	return exists, nil
}

func parseIDHeader(raw string) int64 {
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
