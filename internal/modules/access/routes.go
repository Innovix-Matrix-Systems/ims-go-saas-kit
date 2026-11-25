package access

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterRoutesResolve mounts tenant-scoped role routes.
func (h *RoleHandler) RegisterRoutesResolve(r chi.Router) {
	r.With(h.can(authz.PermRoleViewAll)).Get("/role", h.index)
	r.With(h.can(authz.PermRoleCreate)).Post("/role", h.store)
	r.With(h.can(authz.PermRoleUpdate)).Get("/role/grantable-permissions", h.grantable)
	r.With(h.can(authz.PermRoleView)).Get("/role/{id}", h.show)
	r.With(h.can(authz.PermRoleUpdate)).Patch("/role/{id}", h.update)
	r.With(h.can(authz.PermRoleDelete)).Delete("/role/{id}", h.destroy)
	r.With(h.can(authz.PermRoleUpdate)).Post("/role/assign-permission", h.assignPermissions)
}

// RegisterRoutesExempt mounts internal role routes.
func (h *RoleHandler) RegisterRoutesExempt(r chi.Router) {
	r.With(h.can(authz.PermRoleInternalCreate)).Post("/internal/roles", h.storeInternal)
	r.With(h.can(authz.PermRoleInternalUpdate)).Patch("/internal/roles/{id}", h.updateInternal)
	r.With(h.can(authz.PermRoleInternalUpdate)).Post("/internal/role/assign-permission", h.assignPermissionsInternal)
}

// RegisterRoutes mounts permission routes (tenant-exempt group).
func (h *PermissionHandler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermPermissionView)).Get("/permission", h.index)
	r.Post("/permission", h.store)
	r.Delete("/permission/{id}", h.destroy)
	r.Get("/permission/user", h.forUser)
}
