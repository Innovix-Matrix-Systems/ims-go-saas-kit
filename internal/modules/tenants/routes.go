package tenants

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterRoutes mounts tenant management routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermTenantViewAll)).Get("/tenant", h.index)
	r.With(h.can(authz.PermTenantView)).Get("/tenant/{id}", h.show)
	r.With(h.can(authz.PermTenantCreate)).Post("/tenant", h.store)
	r.With(h.can(authz.PermTenantUpdate)).Patch("/tenant/{id}", h.update)
	r.With(h.can(authz.PermTenantDelete)).Delete("/tenant/{id}", h.destroy)
}

// RegisterPublicTenantRoutes mounts routes used before sign-in. They run
// behind tenancy.Public: the tenant comes from X-Tenant-Code.
func (h *Handler) RegisterPublicTenantRoutes(r chi.Router) {
	r.Get("/public/tenant", h.publicTenant)
}
