package files

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
)

// RegisterRoutesResolve mounts the tenant file routes (behind
// tenancy.Resolve): every read and upload is scoped to the effective tenant,
// so an impersonating platform account uploads into the acting tenant.
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.With(h.can(authz.PermFileViewAll)).Get("/files", h.index)
	r.With(h.can(authz.PermFileCreate)).Post("/files", h.store)
	r.Get("/files/{id}", h.show)
	r.Get("/files/{id}/download", h.download)
	r.Delete("/files/{id}", h.destroy)
}

// RegisterRoutesExempt mounts the internal file routes (tenant-exempt,
// InternalOnly): reads span every tenant and uploads are global files.
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.With(h.can(authz.PermFileInternalViewAll)).Get("/internal/files", h.indexInternal)
	r.With(h.can(authz.PermFileInternalCreate)).Post("/internal/files", h.storeInternal)
	r.Get("/internal/files/{id}", h.showInternal)
	r.Get("/internal/files/{id}/download", h.downloadInternal)
	r.Delete("/internal/files/{id}", h.destroyInternal)
}
