package tenancy

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Headers that pick the tenant (and optionally the branch) on public routes.
const (
	TenantCodeHeader = "X-Tenant-Code"
	BranchIDHeader   = "X-Branch-Id"
)

// PublicOptions configures public tenant resolution.
type PublicOptions struct {
	Queries   *dbgen.Queries
	Cache     cache.Store
	Responder *httpx.Responder
}

// PublicTenant is the tenant and branch a public request resolved to.
type PublicTenant struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	BranchID   int64  `json:"branch_id"`
	BranchName string `json:"branch_name"`
}

type publicTenantKey struct{}

// PublicTenantFrom returns the tenant resolved by Public.
func PublicTenantFrom(ctx context.Context) (PublicTenant, bool) {
	tenant, ok := ctx.Value(publicTenantKey{}).(PublicTenant)
	return tenant, ok
}

// cachedTenant is what tenant_code:<code> holds. Found=false caches a miss,
// so probing unknown codes does not reach the database every time.
type cachedTenant struct {
	Found    bool   `json:"found"`
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

// cachedBranch is a branch id and name; ID 0 caches "no such branch".
type cachedBranch struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Public resolves the tenant for routes used before sign-in (branding, a
// public catalogue, a booking form) from X-Tenant-Code, plus an optional
// X-Branch-Id. A missing, unknown, deleted or inactive tenant all get the same
// 422, so codes cannot be probed by the error text. Without X-Branch-Id the
// tenant's first branch is used.
func Public(opts PublicOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			unresolved := func() {
				opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The tenant could not be resolved."))
			}
			code := r.Header.Get(TenantCodeHeader)
			if code == "" {
				unresolved()
				return
			}
			tenant, err := publicTenantByCode(ctx, opts, code)
			if err != nil {
				opts.Responder.Error(w, r, err)
				return
			}
			if !tenant.Found || !tenant.IsActive {
				unresolved()
				return
			}

			var branch cachedBranch
			if raw := r.Header.Get(BranchIDHeader); raw != "" {
				branchID := parseIDHeader(raw)
				if branchID == 0 {
					opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The selected branch does not belong to the tenant."))
					return
				}
				branch, err = publicBranch(ctx, opts, tenant.ID, branchID)
			} else {
				branch, err = publicFirstBranch(ctx, opts, tenant.ID)
			}
			if err != nil {
				opts.Responder.Error(w, r, err)
				return
			}
			if branch.ID == 0 {
				opts.Responder.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The selected branch does not belong to the tenant."))
				return
			}

			resolved := PublicTenant{
				ID: tenant.ID, Name: tenant.Name, Code: code, BranchID: branch.ID, BranchName: branch.Name,
			}
			ctx = context.WithValue(ctx, publicTenantKey{}, resolved)
			ctx = WithScope(ctx, Scope{TenantID: tenant.ID, BranchID: branch.ID, Public: true})
			httpx.NoteScope(ctx, tenant.ID, branch.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// InvalidateTenantCode drops the cached code lookup after a tenant is updated
// or deleted, so deactivation takes effect on public routes at once.
func InvalidateTenantCode(ctx context.Context, store cache.Store, code string) {
	if code != "" {
		_ = store.Delete(ctx, tenantCodeKey(code))
	}
}

func tenantCodeKey(code string) string { return "tenant_code:" + code }

func publicBranchKey(tenantID, branchID int64) string {
	return fmt.Sprintf("public_branch:%d:%d", tenantID, branchID)
}

func firstBranchKey(tenantID int64) string { return fmt.Sprintf("public_first_branch:%d", tenantID) }

func publicTenantByCode(ctx context.Context, opts PublicOptions, code string) (cachedTenant, error) {
	key := tenantCodeKey(code)
	var cached cachedTenant
	if found, err := opts.Cache.Get(ctx, key, &cached); err != nil {
		return cachedTenant{}, apperr.Wrap(apperr.InternalServerError, "read tenant code cache", err)
	} else if found {
		return cached, nil
	}
	row, err := opts.Queries.GetTenantByCode(ctx, code)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		cached = cachedTenant{}
	case err != nil:
		return cachedTenant{}, apperr.Wrap(apperr.DatabaseError, "load tenant by code", err)
	default:
		cached = cachedTenant{Found: true, ID: row.ID, Name: row.Name, IsActive: row.IsActive}
	}
	_ = opts.Cache.Set(ctx, key, cached, CheckTTL)
	return cached, nil
}

func publicBranch(ctx context.Context, opts PublicOptions, tenantID, branchID int64) (cachedBranch, error) {
	key := publicBranchKey(tenantID, branchID)
	var cached cachedBranch
	if found, err := opts.Cache.Get(ctx, key, &cached); err != nil {
		return cachedBranch{}, apperr.Wrap(apperr.InternalServerError, "read branch cache", err)
	} else if found {
		return cached, nil
	}
	name, err := opts.Queries.GetTenantBranchName(ctx, dbgen.GetTenantBranchNameParams{BranchID: branchID, TenantID: tenantID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		cached = cachedBranch{}
	case err != nil:
		return cachedBranch{}, apperr.Wrap(apperr.DatabaseError, "load branch", err)
	default:
		cached = cachedBranch{ID: branchID, Name: name}
	}
	_ = opts.Cache.Set(ctx, key, cached, CheckTTL)
	return cached, nil
}

func publicFirstBranch(ctx context.Context, opts PublicOptions, tenantID int64) (cachedBranch, error) {
	key := firstBranchKey(tenantID)
	var cached cachedBranch
	if found, err := opts.Cache.Get(ctx, key, &cached); err != nil {
		return cachedBranch{}, apperr.Wrap(apperr.InternalServerError, "read branch cache", err)
	} else if found {
		return cached, nil
	}
	row, err := opts.Queries.FirstBranchOfTenant(ctx, tenantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		cached = cachedBranch{}
	case err != nil:
		return cachedBranch{}, apperr.Wrap(apperr.DatabaseError, "load first branch", err)
	default:
		cached = cachedBranch{ID: row.ID, Name: row.Name}
	}
	_ = opts.Cache.Set(ctx, key, cached, CheckTTL)
	return cached, nil
}
