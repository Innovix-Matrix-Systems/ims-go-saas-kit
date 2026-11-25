package onboarding

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
)

// RegisterPublicRoutes mounts sign-up (no authentication).
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Post("/signup", h.signUp)
}

// RegisterRoutes mounts the first-run setup routes (tenant-resolved). Every
// member may read the progress; only the owner completes it (checked in the
// service), and never while someone acts as them.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/onboarding", h.progress)
	r.With(authn.DenyImpersonated(h.Responder)).Post("/onboarding/complete", h.complete)
}
