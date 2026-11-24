// Package tenancy resolves and propagates the tenant/branch execution scope
// for every request, job and command.
package tenancy

import (
	"context"
	"net/http"
)

// Acting describes impersonation state for the current request.
type Acting int

const (
	ActingNone Acting = iota
	ActingGlobal
	ActingTenant
	ActingBranch
)

// Scope is the resolved tenant/branch execution context.
type Scope struct {
	TenantID       int64
	BranchID       int64
	Acting         Acting
	ActingTenantID int64
	ActingBranchID int64
	Internal       bool
	// Public is set by tenancy.Public: the request is anonymous and the
	// tenant came from X-Tenant-Code. Services refuse writes that must not
	// happen without a signed-in user.
	Public bool
	// AllBranches is set for a member who may switch branches and has not
	// picked one: branch-scoped lists show every branch. BranchID stays the
	// member's own branch, which new records are stamped with.
	AllBranches bool
}

// EffectiveTenantID returns the tenant used for data access
// (impersonation overrides identity; zero means no tenant filter). An explicit
// global acting scope always yields zero — no tenant filter.
func (s Scope) EffectiveTenantID() int64 {
	switch s.Acting {
	case ActingGlobal:
		return 0
	case ActingTenant, ActingBranch:
		return s.ActingTenantID
	}
	return s.TenantID
}

// EffectiveBranchID returns the branch used for data access
// (zero means no branch filter).
func (s Scope) EffectiveBranchID() int64 {
	switch s.Acting {
	case ActingBranch:
		return s.ActingBranchID
	case ActingTenant, ActingGlobal:
		return 0
	}
	return s.BranchID
}

// ListBranchID returns the branch that branch-scoped lists show (members,
// the notification feed); zero means every branch of the tenant. It is the
// branch the member switched to, the acting branch, or the member's own
// branch when they may not switch.
func (s Scope) ListBranchID() int64 {
	if s.Acting == ActingNone && s.AllBranches {
		return 0
	}
	return s.EffectiveBranchID()
}

// IsImpersonating reports whether an acting scope overrides identity.
func (s Scope) IsImpersonating() bool {
	return s.Acting == ActingGlobal || s.Acting == ActingTenant || s.Acting == ActingBranch
}

// AllTenants reports whether reads should span every tenant: internal requests
// (tenant.exempt) and impersonators performing a global read (no acting
// headers). Writes never reach services with a global acting scope — the
// resolve middleware rejects them with 403 — so this is safe to use on read
// paths only.
func (s Scope) AllTenants() bool {
	return s.Internal || s.Acting == ActingGlobal
}

type contextKey string

const scopeKey contextKey = "tenancy_scope"

// WithScope stores the resolved scope in the context.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey, scope)
}

// FromContext returns the resolved scope, if any.
func FromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey).(Scope)
	return scope, ok
}

// Filter is the tenant predicate a repository applies to a single-row query:
// either every tenant (All) or exactly one tenant. A zero Filter matches no
// tenant-owned rows (tenant_id = 0 never exists), so a forgotten scope fails
// closed instead of leaking.
type Filter struct {
	All      bool
	TenantID int64
}

// Filter returns the tenant predicate for this scope: every tenant for
// internal/global reads, otherwise the effective tenant.
func (s Scope) Filter() Filter {
	if s.AllTenants() {
		return Filter{All: true}
	}
	return Filter{TenantID: s.EffectiveTenantID()}
}

// InTenant restricts a query to one tenant.
func InTenant(tenantID int64) Filter { return Filter{TenantID: tenantID} }

// Unscoped spans every tenant. Use it only for trusted lookups that are not
// driven by a tenant request: the caller's own record, auth, seeding, jobs.
func Unscoped() Filter { return Filter{All: true} }

// From returns the request's resolved tenant scope (zero Scope when the route
// is not behind tenancy.Resolve).
func From(r *http.Request) Scope {
	scope, _ := FromContext(r.Context())
	return scope
}
