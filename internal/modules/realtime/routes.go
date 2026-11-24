// Package realtime serves the live-update stream: one Server-Sent Events
// connection per signed-in browser that says when the caller's notifications
// or announcements changed, so the app refetches them instead of polling
// (docs/guides/realtime.md). The events come from internal/platform/pubsub.
package realtime

import "github.com/go-chi/chi/v5"

// StreamPath is the stream's URL path; the request timeout skips it.
const StreamPath = "/api/v1/realtime/stream"

// RegisterRoutes mounts the stream in the authenticated group without tenant
// resolution: every signed-in account, tenantless ones included, may hear
// that its own data changed, and the events carry no data. Registered even
// when REALTIME_ENABLED is off (it then answers 404) so the route table stays
// stable.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/realtime/stream", h.stream)
}
