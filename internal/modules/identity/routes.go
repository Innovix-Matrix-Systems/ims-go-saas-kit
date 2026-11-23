package identity

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
)

// RegisterRoutesResolve mounts tenant-scoped user routes.
func (h *Handler) RegisterRoutesResolve(r chi.Router) {
	r.With(h.can(authz.PermUserViewAll)).Get("/user", h.index)
	r.Get("/user/profile", h.profile)
	// Account changes are refused while someone acts as the user (phase 21).
	own := r.With(authn.DenyImpersonated(h.Responder))
	own.Post("/user/profile/update", h.profileUpdate)
	own.Post("/user/profile/update-avatar", h.updateAvatar)
	own.Post("/user/profile/change-password", h.profileChangePassword)
	r.Get("/user/{id}", h.show)
	r.With(h.can(authz.PermUserCreate)).Post("/user", h.store)
	r.With(h.can(authz.PermUserUpdate)).Patch("/user/{id}", h.update)
	r.With(h.can(authz.PermUserDelete)).Delete("/user/{id}", h.destroy)
	r.With(h.can(authz.PermUserRoleAssign)).Post("/user/assign-role", h.assignRole)
	r.With(h.can(authz.PermUserUpdate)).Post("/user/change-password", h.changePassword)
	r.With(h.can(authz.PermUserTwoFactorReset)).Delete("/user/{id}/two-factor", h.resetTwoFactor)
	r.With(h.can(authz.PermUserUpdate)).Post("/user/{id}/invitation", h.resendInvitation)
	own.With(h.can(authz.PermUserBranchSwitch)).Patch("/user/switch-branch/{id}", h.switchBranch)
	r.Post("/user/import-background", h.importUsersBackground)
	r.Post("/user/export-background", h.exportUsersBackground)
	r.With(h.can(authz.PermUserImport)).Post("/user/import-template", h.importTemplate)
}

// RegisterRoutesExempt mounts internal (cross-tenant) user routes.
func (h *Handler) RegisterRoutesExempt(r chi.Router) {
	r.With(h.can(authz.PermUserInternalCreate)).Post("/internal/user", h.storeInternal)
	r.With(h.can(authz.PermUserInternalUpdate)).Patch("/internal/user/change-password", h.changePasswordInternal)
	r.With(h.can(authz.PermUserInternalUpdate)).Patch("/internal/user/{id}", h.updateInternal)
	r.With(h.can(authz.PermUserInternalUpdate)).Post("/internal/user/{id}/invitation", h.resendInvitation)
	r.With(h.can(authz.PermUserInternalUpdate)).Post("/internal/user/{id}/update-avatar", h.updateUserAvatarInternal)
	r.With(h.can(authz.PermUserInternalDelete)).Delete("/internal/user/{id}", h.destroyInternal)
	r.With(h.can(authz.PermUserInternalRoleAssign)).Post("/internal/user/assign-role", h.assignRoleInternal)
	r.Post("/internal/user/profile/update-avatar", h.updateProfileAvatarInternal)
	r.Patch("/internal/user/profile/update", h.profileUpdate)
	r.Patch("/internal/user/profile/change-password", h.profileChangePassword)
	r.With(h.can(authz.PermUserInternalExport)).Post("/internal/user/export", h.exportUsersInternal)
	r.Post("/internal/user/export-background", h.exportUsersBackground)
	r.With(h.can(authz.PermUserInternalImport)).Post("/internal/user/import", h.importUsersInternal)
	r.Post("/internal/user/import-background", h.importUsersBackground)
}
