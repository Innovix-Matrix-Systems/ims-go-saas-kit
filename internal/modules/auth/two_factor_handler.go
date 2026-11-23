package auth

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
)

// Two-factor enrolment (phase 26.4). These are session routes: they run
// without tenant resolution, so platform accounts and setup-only tokens
// (two-factor required, not yet on) reach them; X-Acting-User never applies,
// so the account is always the caller's own.

// TwoFactorStartRequest starts an enrolment.
type TwoFactorStartRequest struct {
	Method   string `json:"method" validate:"required,oneof=totp email"`
	Password string `json:"password" validate:"required,max=72"`
}

// TwoFactorCodeRequest carries a code from the app or the mail.
type TwoFactorCodeRequest struct {
	Code string `json:"code" validate:"required,max=20"`
}

// PasswordRequest confirms the caller's password.
type PasswordRequest struct {
	Password string `json:"password" validate:"required,max=72"`
}

// twoFactorStatusData is the caller's two-factor state and whether the
// policy requires it.
type twoFactorStatusData struct {
	authn.TwoFactorStatus
	Required bool `json:"required"`
}

// twoFactorConfirmData carries the recovery codes (shown once) and, when the
// caller signed in with a setup-only token, the full token that replaces it.
type twoFactorConfirmData struct {
	RecoveryCodes []string `json:"recovery_codes"`
	Token         *string  `json:"token,omitempty"`
}

func (h *Handler) twoFactorStatus(w http.ResponseWriter, r *http.Request) {
	caller, _ := authn.IdentityFromContext(r.Context())
	status, err := h.service.TwoFactor().Status(r.Context(), caller.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	user, err := h.service.Account(r.Context(), caller.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, twoFactorStatusData{TwoFactorStatus: status, Required: h.service.TwoFactorRequired(r.Context(), user)})
}

func (h *Handler) startTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req TwoFactorStartRequest
	if !h.Bind(w, r, &req) {
		return
	}
	caller, _ := authn.IdentityFromContext(r.Context())
	user, err := h.service.ConfirmPassword(r.Context(), caller.UserID, req.Password)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	setup, err := h.service.TwoFactor().Begin(r.Context(), user, req.Method)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, setup)
}

func (h *Handler) confirmTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req TwoFactorCodeRequest
	if !h.Bind(w, r, &req) {
		return
	}
	caller, _ := authn.IdentityFromContext(r.Context())
	user, err := h.service.Account(r.Context(), caller.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	codes, err := h.service.TwoFactor().Confirm(r.Context(), user, req.Code)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := twoFactorConfirmData{RecoveryCodes: codes}
	if caller.SetupOnly && h.sessions != nil {
		token, err := h.sessions.Upgrade(r.Context(), caller)
		if err != nil {
			h.Error(w, r, err)
			return
		}
		data.Token = &token.PlainText
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *Handler) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var req PasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	caller, _ := authn.IdentityFromContext(r.Context())
	user, err := h.service.ConfirmPassword(r.Context(), caller.UserID, req.Password)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	codes, err := h.service.TwoFactor().RegenerateRecoveryCodes(r.Context(), user)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, twoFactorConfirmData{RecoveryCodes: codes})
}

// disableTwoFactor turns two-factor off and signs the caller's other
// sessions out: whoever might have learnt the password can no longer use a
// session that a second factor once protected.
func (h *Handler) disableTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req PasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	caller, _ := authn.IdentityFromContext(r.Context())
	user, err := h.service.ConfirmPassword(r.Context(), caller.UserID, req.Password)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if err := h.service.TwoFactor().Disable(r.Context(), user, false); err != nil {
		h.Error(w, r, err)
		return
	}
	if h.sessions != nil {
		if err := h.sessions.RevokeOthers(r.Context(), caller); err != nil {
			h.Error(w, r, err)
			return
		}
	}
	h.NoContent(w)
}
