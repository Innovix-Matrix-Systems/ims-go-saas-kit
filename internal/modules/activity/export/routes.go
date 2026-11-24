package activityexport

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// RegisterRoutes mounts POST /activity/export (tenant-resolved): an
// organization's own log, or the organization a staff member acts in.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(authz.RequireAny(h.checker, h.Responder, authz.PermActivityView, authz.PermActivityViewAll)).
		Post("/activity/export", h.start)
}

// RegisterInternalRoutes mounts POST /internal/activity/export for staff:
// the platform log (every organization and the system log). It sits with
// the internal routes because staff without acting headers may not write
// through the tenant routes.
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.With(authz.Require(h.checker, authz.PermActivityViewAll, h.Responder)).
		Post("/internal/activity/export", h.start)
}
