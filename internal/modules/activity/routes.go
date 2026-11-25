package activity

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterRoutes mounts the tenant-resolved activity routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(authz.RequireAny(h.checker, h.Responder, authz.PermActivityView, authz.PermActivityViewAll)).
		Get("/activity", h.index)
	r.With(authz.RequireAny(h.checker, h.Responder, authz.PermActivityView, authz.PermActivityViewAll)).
		Get("/activity/facets", h.facets)
}
