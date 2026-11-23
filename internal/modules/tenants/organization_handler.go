package tenants

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// OrganizationUpdateRequest is PATCH /api/v1/organization.
type OrganizationUpdateRequest struct {
	Name  string  `json:"name" validate:"required,min=2,max=255"`
	Phone *string `json:"phone" validate:"omitempty,max=50"`
	Email *string `json:"email" validate:"omitempty,email,max=255"`
}

// TransferOwnershipRequest is POST /api/v1/organization/transfer-ownership.
type TransferOwnershipRequest struct {
	UserID   int64  `json:"user_id" validate:"required,gt=0"`
	Password string `json:"password" validate:"required,max=72"`
}

// OwnerResponse is the organization's owner.
type OwnerResponse struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// BrandingResponse is how the organization presents itself: its e-mail
// branding settings, which clients also use for its mark.
type BrandingResponse struct {
	LogoURL *string `json:"logo_url"`
	Color   *string `json:"color"`
}

// OrganizationResponse is the caller's organization.
type OrganizationResponse struct {
	ID        int64            `json:"id"`
	Name      string           `json:"name"`
	Code      string           `json:"code"`
	Phone     *string          `json:"phone"`
	Email     *string          `json:"email"`
	IsActive  bool             `json:"is_active"`
	Owner     *OwnerResponse   `json:"owner"`
	IsOwner   bool             `json:"is_owner"`
	Branding  BrandingResponse `json:"branding"`
	CreatedAt time.Time        `json:"created_at"`
}

// SettingReader reads a tenant's effective settings (the settings service).
type SettingReader interface {
	Get(ctx context.Context, scope tenancy.Scope, name string) (any, error)
}

// WithSettings adds the branding to organization responses.
func (h *Handler) WithSettings(settings SettingReader) *Handler {
	h.settings = settings
	return h
}

// branding reads the organization's logo and colour; unset or unreadable
// values are null.
func (h *Handler) branding(ctx context.Context, tenantID int64) BrandingResponse {
	out := BrandingResponse{}
	if h.settings == nil {
		return out
	}
	scope := tenancy.Scope{TenantID: tenantID}
	text := func(name string) *string {
		value, err := h.settings.Get(ctx, scope, name)
		if err != nil {
			return nil
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			trimmed := strings.TrimSpace(s)
			return &trimmed
		}
		return nil
	}
	out.LogoURL = text("branding.logo_url")
	out.Color = text("branding.color")
	return out
}

func newOrganizationResponse(tenant Tenant, owner *Owner, callerID int64) OrganizationResponse {
	out := OrganizationResponse{
		ID: tenant.ID, Name: tenant.Name, Code: tenant.Code, Phone: tenant.Phone, Email: tenant.Email,
		IsActive: tenant.IsActive, CreatedAt: tenant.CreatedAt,
	}
	if owner != nil {
		out.Owner = &OwnerResponse{ID: owner.ID, Name: owner.Name, Email: owner.Email}
		out.IsOwner = owner.ID == callerID
	}
	return out
}

// RegisterOrganizationRoutes mounts the caller's own organization
// (tenant-resolved group). Every member reads it; admins edit it; only the
// owner transfers it (checked in the service), never while acting as them.
func (h *Handler) RegisterOrganizationRoutes(r chi.Router) {
	r.Get("/organization", h.showOrganization)
	r.With(h.can(authz.PermOrganizationUpdate)).Patch("/organization", h.updateOrganization)
	r.With(authn.DenyImpersonated(h.Responder)).Post("/organization/transfer-ownership", h.transferOwnership)
}

func (h *Handler) showOrganization(w http.ResponseWriter, r *http.Request) {
	tenant, owner, err := h.service.Organization(r.Context(), tenancy.From(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondOrganization(w, r, *tenant, owner)
}

func (h *Handler) updateOrganization(w http.ResponseWriter, r *http.Request) {
	var req OrganizationUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	tenant, owner, err := h.service.UpdateOrganization(r.Context(), tenancy.From(r), authn.UserID(r), OrganizationUpdate{
		Name: req.Name, Phone: req.Phone, Email: req.Email,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondOrganization(w, r, *tenant, owner)
}

func (h *Handler) transferOwnership(w http.ResponseWriter, r *http.Request) {
	var req TransferOwnershipRequest
	if !h.Bind(w, r, &req) {
		return
	}
	tenant, owner, err := h.service.TransferOwnership(r.Context(), tenancy.From(r), authn.UserID(r), req.UserID, req.Password)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondOrganization(w, r, *tenant, owner)
}

func (h *Handler) respondOrganization(w http.ResponseWriter, r *http.Request, tenant Tenant, owner *Owner) {
	out := newOrganizationResponse(tenant, owner, authn.UserID(r))
	out.Branding = h.branding(r.Context(), tenant.ID)
	h.Data(w, r, http.StatusOK, out)
}
