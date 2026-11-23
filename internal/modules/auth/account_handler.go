package auth

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
)

// InvitationPreviewRequest names a pending invitation link.
type InvitationPreviewRequest struct {
	Email string `json:"email" validate:"required,email,max=255"`
	Token string `json:"token" validate:"required,max=200"`
}

// InvitationPreviewResponse is what the invitee sees before choosing a password.
type InvitationPreviewResponse struct {
	Name             string `json:"name"`
	Email            string `json:"email"`
	OrganizationName string `json:"organization_name"`
	InviterName      string `json:"inviter_name"`
}

// ChangeEmailRequest asks to move the account to another address.
type ChangeEmailRequest struct {
	Password string `json:"password" validate:"required,max=72"`
	Email    string `json:"email" validate:"required,email,max=255"`
}

// previewInvitation describes a pending invitation without using the link up.
func (h *Handler) previewInvitation(w http.ResponseWriter, r *http.Request) {
	var req InvitationPreviewRequest
	if !h.Bind(w, r, &req) {
		return
	}
	userID, err := h.recovery.PeekInvitation(r.Context(), req.Email, req.Token)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	preview, err := h.users.GetInvitationPreview(r.Context(), userID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, InvitationPreviewResponse{
		Name: preview.Name, Email: preview.Email,
		OrganizationName: preview.OrganizationName, InviterName: preview.InviterName,
	})
}

// requestEmailChange mails a confirmation link to the new address.
func (h *Handler) requestEmailChange(w http.ResponseWriter, r *http.Request) {
	var req ChangeEmailRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.recovery.RequestEmailChange(r.Context(), authn.UserID(r), req.Password, req.Email); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusAccepted, "We sent a confirmation link to your new address. Your e-mail changes when you open it.")
}

// confirmEmailChange switches the account to the new address (public: the
// link arrives in the new mailbox, possibly on another device).
func (h *Handler) confirmEmailChange(w http.ResponseWriter, r *http.Request) {
	var req VerifyEmailRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.recovery.ConfirmEmailChange(r.Context(), req.Token); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Your e-mail address has been changed. Sign in with the new address.")
}
