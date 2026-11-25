package featureflags

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterRoutes mounts the tenant-resolved feature-flag routes (any
// authenticated user reads their effective flags).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/feature-flags", h.index)
}

// RegisterInternalRoutes mounts the internal (cross-tenant) flag admin routes.
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.With(h.can(authz.PermFeatureFlagInternalUpdate)).Get("/internal/feature-flags", h.internalIndex)
	r.With(h.can(authz.PermFeatureFlagInternalUpdate)).Patch("/internal/feature-flags", h.internalUpdate)
	r.With(h.can(authz.PermFeatureFlagInternalUpdate)).Delete("/internal/feature-flags/{key}", h.internalDelete)
}
