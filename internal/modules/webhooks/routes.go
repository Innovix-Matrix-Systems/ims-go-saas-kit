package webhooks

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
)

// RegisterRoutes mounts the tenant-scoped webhook routes (webhook.manage).
func (h *Handler) RegisterRoutes(r chi.Router) {
	manage := r.With(h.can(authz.PermWebhookManage))
	manage.Get("/webhook/events", h.events)
	manage.Get("/webhook", h.index)
	manage.Post("/webhook", h.store)
	manage.Get("/webhook/{id}", h.show)
	manage.Patch("/webhook/{id}", h.update)
	manage.Delete("/webhook/{id}", h.destroy)
	manage.Post("/webhook/{id}/rotate-secret", h.rotateSecret)
	manage.Post("/webhook/{id}/test", h.test)
	manage.Get("/webhook/{id}/deliveries", h.deliveries)
	manage.Post("/webhook/deliveries/{id}/redeliver", h.redeliver)
}
