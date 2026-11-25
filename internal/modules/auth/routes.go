package auth

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
)

// RegisterPublicRoutes mounts unauthenticated auth routes.
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	r.Post("/login", h.login)
	r.Post("/verify-otp", h.verifyOTP)
	r.Get("/social/providers", h.socialProviders)
	r.Post("/social/{provider}/login", h.socialLogin)
	if h.recovery != nil {
		r.Post("/forgot-password", h.forgotPassword)
		r.Post("/reset-password", h.resetPassword)
		r.Post("/email/verification-notification", h.resendVerification)
		r.Post("/email/verify", h.verifyEmail)
		r.Post("/invitation/accept", h.acceptInvitation)
		r.Post("/invitation/preview", h.previewInvitation)
		r.Post("/email/change/confirm", h.confirmEmailChange)
	}
}

// RegisterRoutes mounts authenticated session routes (no tenant resolution).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/logout", h.logout)
	if h.recovery != nil {
		// The caller's own address; never while someone acts as them.
		r.With(authn.DenyImpersonated(h.Responder)).Post("/user/profile/change-email", h.requestEmailChange)
	}
	if h.service.TwoFactor() != nil {
		r.Get("/two-factor", h.twoFactorStatus)
		r.Post("/two-factor", h.startTwoFactor)
		r.Post("/two-factor/confirm", h.confirmTwoFactor)
		r.Post("/two-factor/recovery-codes", h.regenerateRecoveryCodes)
		r.Delete("/two-factor", h.disableTwoFactor)
	}
	if h.sessions != nil {
		r.Get("/sessions", h.listSessions)
		r.Post("/sessions/revoke-others", h.revokeOtherSessions)
		r.Delete("/sessions/{id}", h.revokeSession)
	}
}
