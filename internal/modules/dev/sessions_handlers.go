package dev

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
)

// Sessions panel: token/device directory, force logout and idle pruning
// (rules live in SessionService).

func (h *Handler) sessionStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sessions.Stats(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) listSessionUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	users, err := h.sessions.Users(r.Context(), SessionUserFilter{
		Search:   strings.TrimSpace(query.Get("search")),
		BeforeID: beforeID,
		Limit:    int32(limit),
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(users) == limit && len(users) > 0 {
		nextCursor = strconv.FormatInt(users[len(users)-1].UserID, 10)
	}
	h.Cursor(w, r, http.StatusOK, users, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) listUserSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	tokens, err := h.sessions.Tokens(r.Context(), userID, currentTokenID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, tokens)
}

func (h *Handler) revokeSessionToken(w http.ResponseWriter, r *http.Request) {
	tokenID, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.sessions.RevokeToken(r.Context(), authn.UserID(r), tokenID); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"revoked": 1})
}

func (h *Handler) revokeUserSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	revoked, err := h.sessions.RevokeUser(r.Context(), authn.UserID(r), userID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"revoked": revoked})
}

func (h *Handler) revokeAllSessions(w http.ResponseWriter, r *http.Request) {
	keep := currentTokenID(r)
	revoked, err := h.sessions.RevokeAllExcept(r.Context(), authn.UserID(r), keep)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"revoked": revoked, "excluded_token_id": keep})
}

type pruneSessionsRequest struct {
	IdleDays int32 `json:"idle_days" validate:"omitempty,min=1,max=3650"`
	DryRun   bool  `json:"dry_run"`
}

func (h *Handler) pruneSessions(w http.ResponseWriter, r *http.Request) {
	var request pruneSessionsRequest
	if !h.Bind(w, r, &request) {
		return
	}
	result, err := h.sessions.Prune(r.Context(), authn.UserID(r), request.IdleDays, request.DryRun)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, result)
}

func currentTokenID(r *http.Request) int64 {
	if identity, ok := authn.IdentityFromContext(r.Context()); ok && identity != nil {
		return identity.TokenID
	}
	return 0
}

func formatTarget(kind string, id int64) string {
	return kind + ":" + strconv.FormatInt(id, 10)
}
