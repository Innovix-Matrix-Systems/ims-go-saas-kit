package notifications

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// RegisterRoutesResolve mounts tenant-scoped notification routes (feed and
// read state for the authenticated user).
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.Get("/notification/feed", h.feed)
	r.Post("/notification/{id}/read", h.markRead)
	r.Post("/notification/read-all", h.markAllRead)
	r.Get("/notification/preferences", h.preferences)
	r.Put("/notification/preferences", h.setPreferences)
}

// RegisterPublicRoutes mounts the one-click unsubscribe link (no sign-in:
// the sealed token names the user).
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/notification/unsubscribe", h.unsubscribe)
	r.Post("/notification/unsubscribe", h.unsubscribe)
}

// RegisterRoutesExempt mounts the internal (tenant-exempt) routes: the same
// feed/read endpoints for identities without a tenant, plus the admin CRUD
// (permission-gated).
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.Get("/internal/notification/feed", h.feed)
	r.Post("/internal/notification/{id}/read", h.markRead)
	r.Post("/internal/notification/read-all", h.markAllRead)
	r.Get("/internal/notification/preferences", h.preferences)
	r.Put("/internal/notification/preferences", h.setPreferences)

	r.With(h.can(authz.PermNotificationViewAll)).Get("/internal/notification", h.index)
	r.With(h.can(authz.PermNotificationView)).Get("/internal/notification/{id}", h.show)
	r.With(h.can(authz.PermNotificationCreate)).Post("/internal/notification", h.store)
	r.With(h.can(authz.PermNotificationUpdate)).Patch("/internal/notification/{id}", h.update)
	r.With(h.can(authz.PermNotificationDelete)).Delete("/internal/notification/{id}", h.destroy)
}
