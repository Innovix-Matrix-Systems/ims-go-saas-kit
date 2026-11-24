package dev

import (
	"net/http"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Developers panel: dedicated internal accounts that hold the hidden Developer
// role (rules live in DeveloperService).

// DeveloperStats is the dashboard header payload.
type DeveloperStats struct {
	Developers       int64  `json:"developers"`
	RoleExists       bool   `json:"role_exists"`
	RoleID           *int64 `json:"role_id"`
	RoleName         string `json:"role_name"`
	RolePermissions  int64  `json:"role_permissions"`
	TotalPermissions int64  `json:"total_permissions"`
	AllPermissions   int64  `json:"all_permissions"`
}

func (h *Handler) listDevelopers(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.developers.List(r.Context(), DeveloperFilter{
		Search: strings.TrimSpace(r.URL.Query().Get("search")),
		Limit:  int32(perPage),
		Offset: int32(offset),
	}, authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, items, meta, links)
}

func (h *Handler) developerStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.developers.Stats(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

type createDeveloperRequest struct {
	Name     string `json:"name" validate:"required,min=2,max=255"`
	Email    string `json:"email" validate:"required,email,max=255"`
	Password string `json:"password" validate:"required,min=8,max=72"`
	IsActive *bool  `json:"is_active"`
}

func (h *Handler) createDeveloper(w http.ResponseWriter, r *http.Request) {
	var request createDeveloperRequest
	if !h.Bind(w, r, &request) {
		return
	}
	isActive := request.IsActive == nil || *request.IsActive
	developer, err := h.developers.Create(r.Context(), tenancy.From(r), authn.UserID(r), NewDeveloperInput{
		Name: request.Name, Email: request.Email, Password: request.Password, IsActive: isActive,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, developer)
}

func (h *Handler) ensureDeveloperRole(w http.ResponseWriter, r *http.Request) {
	stats, err := h.developers.EnsureRole(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) grantDeveloperAllPermissions(w http.ResponseWriter, r *http.Request) {
	stats, err := h.developers.GrantAll(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) removeDeveloper(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.PathID(w, r, "user_id")
	if !ok {
		return
	}
	if err := h.developers.Remove(r.Context(), tenancy.From(r), authn.UserID(r), userID); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"removed": true})
}

func developerFromUser(user *identity.User, actorID int64) Developer {
	return Developer{
		UserID:      user.ID,
		Name:        user.Name,
		Email:       user.Email,
		IsActive:    user.IsActive,
		IsSelf:      user.ID == actorID,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
	}
}
