package auth

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
)

// Self-service sessions (phase 26.3): the caller sees where they are signed
// in and signs devices out. Session routes run without tenant resolution, so
// platform accounts use them too; X-Acting-User does not apply here, so a
// session is always the caller's own.

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		h.Error(w, r, apperr.New(apperr.Unauthorized, "Authentication is required."))
		return
	}
	sessions, err := h.sessions.List(r.Context(), caller)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, sessions)
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	caller, _ := authn.IdentityFromContext(r.Context())
	if err := h.sessions.Revoke(r.Context(), caller, id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	caller, _ := authn.IdentityFromContext(r.Context())
	if err := h.sessions.RevokeOthers(r.Context(), caller); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}
