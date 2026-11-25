package currencies

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterPublicRoutes mounts the currency list clients format amounts with.
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/public/currencies", h.public)
}

// RegisterRoutesExempt mounts currency management for staff (tenant-exempt).
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.With(h.can(authz.PermCurrencyViewAll)).Get("/currencies", h.index)
	r.With(h.can(authz.PermCurrencyCreate)).Post("/currencies", h.store)
	r.With(h.can(authz.PermCurrencyUpdate)).Patch("/currencies/{id}", h.update)
	r.With(h.can(authz.PermCurrencyUpdate)).Post("/currencies/{id}/default", h.makeDefault)
	r.With(h.can(authz.PermCurrencyDelete)).Delete("/currencies/{id}", h.destroy)
}
