package settings

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
)

// RegisterRoutes mounts the tenant-resolved settings routes (any authenticated
// user reads; tenant admins write their own override).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/settings", h.index)
	r.With(h.can(authz.PermSettingUpdate)).Patch("/settings/tenant/{key}", h.updateTenant)
}

// RegisterInternalRoutes mounts the internal (cross-tenant) settings routes.
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.With(h.can(authz.PermSettingInternalView)).Get("/internal/settings", h.internalIndex)
	r.With(h.can(authz.PermSettingInternalUpdate)).Patch("/internal/settings/{key}", h.internalUpdate)
	r.With(h.can(authz.PermSettingInternalUpdate)).Delete("/internal/settings/{key}", h.internalDelete)
	r.With(h.can(authz.PermSettingInternalUpdate)).Put("/internal/settings/{key}/group", h.assignGroup)

	r.With(h.can(authz.PermSettingsGroupViewAll)).Get("/internal/settings-groups", h.groupsIndex)
	r.With(h.can(authz.PermSettingsGroupCreate)).Post("/internal/settings-groups", h.groupsStore)
	r.With(h.can(authz.PermSettingsGroupView)).Get("/internal/settings-groups/{id}", h.groupsShow)
	r.With(h.can(authz.PermSettingsGroupUpdate)).Patch("/internal/settings-groups/{id}", h.groupsUpdate)
	r.With(h.can(authz.PermSettingsGroupDelete)).Delete("/internal/settings-groups/{id}", h.groupsDestroy)
}
