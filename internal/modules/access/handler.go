package access

import (
	"context"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// RoleHandler serves role endpoints.
type RoleHandler struct {
	service *RoleService
	checker *authz.Checker
	tenants TenantLookup
	httpx.Kit
}

// NewRoleHandler creates the role handler.
func NewRoleHandler(service *RoleService, checker *authz.Checker, tenants TenantLookup, kit httpx.Kit) *RoleHandler {
	return &RoleHandler{service: service, checker: checker, tenants: tenants, Kit: kit}
}

// tenantSlims loads the slim tenants for a set of roles in one query and keys
// them by tenant id (nil tenants are skipped).
func (h *RoleHandler) tenantSlims(ctx context.Context, roles []Role) map[int64]*TenantSlim {
	out := map[int64]*TenantSlim{}
	if h.tenants == nil {
		return out
	}
	ids := make([]int64, 0, len(roles))
	seen := map[int64]bool{}
	for _, role := range roles {
		if role.TenantID != nil && !seen[*role.TenantID] {
			seen[*role.TenantID] = true
			ids = append(ids, *role.TenantID)
		}
	}
	if len(ids) == 0 {
		return out
	}
	refs, err := h.tenants.RefsByIDs(ctx, ids)
	if err != nil {
		return out
	}
	for id, ref := range refs {
		out[id] = NewTenantSlim(ref)
	}
	return out
}

// responseFor maps a single role, embedding its tenant when it has one.
func (h *RoleHandler) responseFor(ctx context.Context, role Role) RoleResponse {
	byTenant := h.tenantSlims(ctx, []Role{role})
	if role.TenantID == nil {
		return NewRoleResponse(role, nil)
	}
	return NewRoleResponse(role, byTenant[*role.TenantID])
}

func (h *RoleHandler) index(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)
	roles, err := h.service.List(r.Context(), scope.EffectiveTenantID())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	byTenant := h.tenantSlims(r.Context(), roles)
	data := make([]RoleResponse, 0, len(roles))
	for _, role := range roles {
		var tenant *TenantSlim
		if role.TenantID != nil {
			tenant = byTenant[*role.TenantID]
		}
		data = append(data, NewRoleResponse(role, tenant))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *RoleHandler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	scope := tenancy.From(r)
	role, err := h.service.Get(r.Context(), scope.EffectiveTenantID(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if role.Hidden {
		h.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "Role with ID '%d' was not found.", id))
		return
	}
	permissions, err := h.service.Permissions(r.Context(), role.ID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	response := h.responseFor(r.Context(), *role)
	response.Permissions = make([]PermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		response.Permissions = append(response.Permissions, NewPermissionResponse(permission))
	}
	h.Data(w, r, http.StatusOK, response)
}

// grantable lists the permissions the caller may put on a role.
func (h *RoleHandler) grantable(w http.ResponseWriter, r *http.Request) {
	permissions, err := h.service.Grantable(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]PermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		data = append(data, NewPermissionResponse(permission))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *RoleHandler) store(w http.ResponseWriter, r *http.Request) {
	var req RoleInsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	role, err := h.service.Create(r.Context(), scope.EffectiveTenantID(), authn.UserID(r), trimString(req.Name), req.Description)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req RoleInsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	role, err := h.service.Update(r.Context(), scope.EffectiveTenantID(), id, authn.UserID(r), trimString(req.Name), req.Description)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	scope := tenancy.From(r)
	if err := h.service.Delete(r.Context(), scope.EffectiveTenantID(), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *RoleHandler) assignPermissions(w http.ResponseWriter, r *http.Request) {
	var req RolePermissionAssignRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	if err := h.service.AssignPermissions(r.Context(), scope.EffectiveTenantID(), authn.UserID(r), req.ID, req.Permissions); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Permissions assigned successfully.")
}

func (h *RoleHandler) storeInternal(w http.ResponseWriter, r *http.Request) {
	var req RoleInternalRequest
	if !h.Bind(w, r, &req) {
		return
	}
	role, err := h.service.CreateInternal(r.Context(), req.TenantID, authn.UserID(r), trimString(req.Name), req.Description, req.IsSystem)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) updateInternal(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req RoleInsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	role, err := h.service.UpdateInternal(r.Context(), id, authn.UserID(r), trimString(req.Name), req.Description)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *role))
}

func (h *RoleHandler) assignPermissionsInternal(w http.ResponseWriter, r *http.Request) {
	var req RolePermissionAssignRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.service.AssignPermissionsInternal(r.Context(), req.ID, req.Permissions); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Permissions assigned successfully.")
}

// PermissionHandler serves permission endpoints (internal surface).
type PermissionHandler struct {
	service *PermissionService
	checker *authz.Checker
	httpx.Kit
}

// NewPermissionHandler creates the permission handler.
func NewPermissionHandler(service *PermissionService, checker *authz.Checker, kit httpx.Kit) *PermissionHandler {
	return &PermissionHandler{service: service, checker: checker, Kit: kit}
}

func (h *PermissionHandler) index(w http.ResponseWriter, r *http.Request) {
	permissions, err := h.service.List(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]PermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		data = append(data, NewPermissionResponse(permission))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *PermissionHandler) store(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustBeSuperAdmin(r.Context(), identity.UserID); err != nil {
		h.Error(w, r, err)
		return
	}
	var req PermissionInsertRequest
	if !h.Bind(w, r, &req) {
		return
	}
	permission, err := h.service.Create(r.Context(), trimString(req.Name), req.Group, req.Description)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewPermissionResponse(*permission))
}

func (h *PermissionHandler) destroy(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if err := h.checker.MustBeSuperAdmin(r.Context(), identity.UserID); err != nil {
		h.Error(w, r, err)
		return
	}
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *PermissionHandler) forUser(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	permissions, err := h.service.ForUser(r.Context(), identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]PermissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		data = append(data, NewPermissionResponse(permission))
	}
	h.Data(w, r, http.StatusOK, data)
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *PermissionHandler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *RoleHandler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
