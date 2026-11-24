package dashboard

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the organization dashboard (tenant-resolved; every
// member, each section behind its own permission).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/dashboard", h.tenant)
}

// RegisterInternalRoutes mounts the platform dashboard (InternalOnly; each
// section behind its own permission).
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.Get("/internal/dashboard", h.platform)
}
