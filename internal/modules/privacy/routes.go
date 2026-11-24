package privacy

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// RegisterRoutes mounts the caller's own deletion and export routes in the
// authenticated group without tenant resolution (like sessions and
// two-factor): tenantless accounts reach them, and X-Acting-User never
// applies, so the account is always the caller's own.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/user/privacy/deletion", h.deletionStatus)
	r.Post("/user/privacy/deletion", h.requestDeletion)
	r.Delete("/user/privacy/deletion", h.cancelDeletion)
	r.Post("/user/privacy/export", h.exportOwn)
}

// RegisterRoutesResolve mounts the tenant admin routes (the target must be
// in the caller's tenant).
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.With(h.can(authz.PermUserErase)).Post("/user/{id}/erase", h.erase)
	r.With(h.can(authz.PermUserErase)).Delete("/user/{id}/erase", h.cancelErase)
	r.With(h.can(authz.PermUserPrivacyExport)).Post("/user/{id}/privacy-export", h.exportUser)
}

// RegisterRoutesExempt mounts the internal (cross-tenant) admin routes.
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.With(h.can(authz.PermUserInternalErase)).Post("/internal/user/{id}/erase", h.erase)
	r.With(h.can(authz.PermUserInternalErase)).Delete("/internal/user/{id}/erase", h.cancelErase)
	r.With(h.can(authz.PermUserInternalPrivacyExport)).Post("/internal/user/{id}/privacy-export", h.exportUser)
}
