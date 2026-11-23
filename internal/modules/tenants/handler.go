package tenants

import (
	"net/http"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// InsertUpdateRequest is the tenant create/update payload.
type InsertUpdateRequest struct {
	Name     string  `json:"name" validate:"required,min=2,max=255"`
	Slug     *string `json:"slug" validate:"omitempty,max=255"`
	Domain   *string `json:"domain" validate:"omitempty,max=255"`
	Phone    *string `json:"phone" validate:"omitempty,max=50"`
	Email    *string `json:"email" validate:"omitempty,email,max=255"`
	IsActive *bool   `json:"is_active"`
}

// Response is the public tenant representation.
type Response struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Slug      *string   `json:"slug"`
	Domain    *string   `json:"domain"`
	Phone     *string   `json:"phone"`
	Email     *string   `json:"email"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewResponse maps a tenant to its public representation.
func NewResponse(tenant Tenant) Response {
	return Response{
		ID:        tenant.ID,
		Name:      tenant.Name,
		Code:      tenant.Code,
		Slug:      tenant.Slug,
		Domain:    tenant.Domain,
		Phone:     tenant.Phone,
		Email:     tenant.Email,
		IsActive:  tenant.IsActive,
		CreatedAt: tenant.CreatedAt,
		UpdatedAt: tenant.UpdatedAt,
	}
}

// Handler serves tenant endpoints (tenant-exempt group).
type Handler struct {
	service  *Service
	checker  *authz.Checker
	settings SettingReader
	httpx.Kit
}

// NewHandler creates the tenant handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.service.List(r.Context(), ListFilter{
		Search: r.URL.Query().Get("search"),
		Limit:  int32(perPage),
		Offset: int32(offset),
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, item := range items {
		data = append(data, NewResponse(item))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	tenant, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*tenant))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	tenant, err := h.service.Create(r.Context(), authn.UserID(r), CreateInput{
		Name:     strings.TrimSpace(req.Name),
		Slug:     req.Slug,
		Domain:   req.Domain,
		Phone:    req.Phone,
		Email:    req.Email,
		IsActive: isActive,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*tenant))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	current, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	isActive := current.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	tenant, err := h.service.Update(r.Context(), authn.UserID(r), id, UpdateInput{
		Name:     strings.TrimSpace(req.Name),
		Slug:     req.Slug,
		Domain:   req.Domain,
		Phone:    req.Phone,
		Email:    req.Email,
		IsActive: isActive,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*tenant))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

// PublicBranch is the branch a public request resolved to.
type PublicBranch struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PublicResponse is what a sign-in page needs to brand itself.
type PublicResponse struct {
	Name   string       `json:"name"`
	Code   string       `json:"code"`
	Branch PublicBranch `json:"branch"`
}

func (h *Handler) publicTenant(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenancy.PublicTenantFrom(r.Context())
	if !ok {
		h.Error(w, r, apperr.New(apperr.TenantContextNotResolved, "The tenant could not be resolved."))
		return
	}
	h.Data(w, r, http.StatusOK, PublicResponse{
		Name: tenant.Name, Code: tenant.Code,
		Branch: PublicBranch{ID: tenant.BranchID, Name: tenant.BranchName},
	})
}
