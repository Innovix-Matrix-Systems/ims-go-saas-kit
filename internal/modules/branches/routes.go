package branches

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// RegisterRoutes mounts tenant-scoped branch routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermBranchViewAll)).Get("/branch", h.index)
	r.With(h.can(authz.PermBranchView)).Get("/branch/{id}", h.show)
	r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)
	r.With(h.can(authz.PermBranchUpdate)).Patch("/branch/{id}", h.update)
	r.With(h.can(authz.PermBranchDelete)).Delete("/branch/{id}", h.destroy)
}
