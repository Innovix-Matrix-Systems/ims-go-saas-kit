package dataprocessing

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterRoutes mounts the processing-job routes (tenant-resolved; ownership
// checks inside the handlers).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermDataProcessingViewAll)).Get("/data-processing/jobs", h.index)
	r.Get("/data-processing/jobs/user", h.userJobs)
	r.Get("/data-processing/job/{jobId}", h.show)
	r.Get("/data-processing/job/{jobId}/download", h.download)
	r.Post("/data-processing/job/{jobId}/retry", h.retry)
	r.Delete("/data-processing/job/{jobId}", h.remove)
	r.With(h.can(authz.PermDataProcessingViewAll)).Get("/data-processing/settings/cleanup", h.cleanupSettings)
	r.With(h.can(authz.PermDataProcessingViewAll)).Post("/data-processing/settings/cleanup", h.updateCleanupSettings)
}

// RegisterInternalRoutes mounts retry and remove for staff working without an
// acting organization, whose writes on organization routes are refused. The
// handlers still only reach the caller's own jobs.
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.Post("/internal/data-processing/job/{jobId}/retry", h.retry)
	r.Delete("/internal/data-processing/job/{jobId}", h.remove)
}
