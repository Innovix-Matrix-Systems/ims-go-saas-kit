package dataprocessing

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// RegisterRoutes mounts the processing-job routes (tenant-resolved; ownership
// checks inside the handlers).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermDataProcessingViewAll)).Get("/data-processing/jobs", h.index)
	r.Get("/data-processing/jobs/user", h.userJobs)
	r.Get("/data-processing/job/{jobId}", h.show)
	r.Get("/data-processing/job/{jobId}/download", h.download)
	r.With(h.can(authz.PermDataProcessingViewAll)).Get("/data-processing/settings/cleanup", h.cleanupSettings)
	r.With(h.can(authz.PermDataProcessingViewAll)).Post("/data-processing/settings/cleanup", h.updateCleanupSettings)
}
