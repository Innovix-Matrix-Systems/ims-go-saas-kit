package announcements

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// RegisterRoutes mounts what members use (tenant-resolved): the live
// announcements for their organization and their reactions to them.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/announcements/live", h.live)
	r.Post("/announcements/{id}/events", h.track)
}

// RegisterInternalRoutes mounts the staff management (InternalOnly).
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.With(h.can(authz.PermAnnouncementView)).Get("/internal/announcements", h.index)
	r.With(h.can(authz.PermAnnouncementView)).Get("/internal/announcements/{id}", h.show)
	r.With(h.can(authz.PermAnnouncementView)).Get("/internal/announcements/{id}/organizations", h.organizations)
	r.With(h.can(authz.PermAnnouncementManage)).Post("/internal/announcements", h.store)
	r.With(h.can(authz.PermAnnouncementManage)).Patch("/internal/announcements/{id}", h.update)
	r.With(h.can(authz.PermAnnouncementManage)).Delete("/internal/announcements/{id}", h.destroy)
}
