package identity

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// The caller's own profile and avatar uploads.

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	user, err := h.service.GetRecord(r.Context(), identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

// ProfilePermissionsResponse is what the effective user may do.
type ProfilePermissionsResponse struct {
	// SuperAdmin bypasses every permission check.
	SuperAdmin  bool     `json:"super_admin"`
	Permissions []string `json:"permissions"`
	// ActingAs is true when staff are acting as this user (X-Acting-User).
	ActingAs bool `json:"acting_as"`
}

func (h *Handler) profilePermissions(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	super, names, err := h.checker.Grants(r.Context(), identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if names == nil {
		names = []string{}
	}
	h.Data(w, r, http.StatusOK, ProfilePermissionsResponse{SuperAdmin: super, Permissions: names, ActingAs: identity.Impersonated()})
}

func (h *Handler) profileUpdate(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	var req ProfileUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	current, err := h.service.GetRecord(r.Context(), identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	user, err := h.service.ProfileUpdate(r.Context(), identity.UserID, UpdateParams{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Name:      name,
		Phone:     req.Phone,
		Locale:    req.Locale,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) profileChangePassword(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	var req ProfileChangePasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.service.ProfileChangePassword(r.Context(), identity.UserID, identity.TokenID, req.CurrentPassword, req.Password); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Password changed successfully.")
}

// updateAvatar replaces a user's profile photo (multipart: id + avatar file).
func (h *Handler) updateAvatar(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if h.photo == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "Avatar uploads are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, avatarMaxBytes+avatarMultipartSlack)
	if err != nil {
		h.Error(w, r, avatarUploadError(err))
		return
	}
	targetID, err := avatarTargetID(form.Value["id"])
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if targetID != identity.UserID {
		if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserUpdate); err != nil {
			h.Error(w, r, err)
			return
		}
	}
	scope := tenancy.From(r)
	target, err := h.service.Get(r.Context(), scope, targetID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	header, err := httpx.FormFile(form, "avatar")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if err := h.photo.UpdateAvatar(r.Context(), identity.UserID, targetID, target.TenantIDValue(), header); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *target))
}

// avatarUploadError turns a transport-level size rejection into the same 422
// field error the size rule produces, so clients never see a bare 413 for an
// avatar that is merely too big.
func avatarUploadError(err error) error {
	if apperr.Is(err, apperr.RequestEntityTooLarge) {
		return apperr.Validation(map[string][]string{
			"avatar": {fmt.Sprintf("The avatar field must not be greater than %d kilobytes.", avatarMaxBytes>>10)},
		})
	}
	return err
}

func avatarTargetID(values []string) (int64, error) {
	if len(values) == 0 || values[0] == "" {
		return 0, apperr.Validation(map[string][]string{"id": {"The id field is required."}})
	}
	id, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.Validation(map[string][]string{"id": {"The id field must be a positive integer."}})
	}
	return id, nil
}

// updateUserAvatarInternal replaces any user's avatar (path id) across tenants.
func (h *Handler) updateUserAvatarInternal(w http.ResponseWriter, r *http.Request) {
	if h.photo == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "Avatar uploads are not available."))
		return
	}
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	scope := tenancy.From(r)
	target, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	form, err := httpx.Multipart(w, r, avatarMaxBytes+avatarMultipartSlack)
	if err != nil {
		h.Error(w, r, avatarUploadError(err))
		return
	}
	header, err := httpx.FormFile(form, "avatar")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if err := h.photo.UpdateAvatar(r.Context(), authn.UserID(r), id, target.TenantIDValue(), header); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *target))
}

// updateProfileAvatarInternal replaces the caller's own avatar.
func (h *Handler) updateProfileAvatarInternal(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	if h.photo == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "Avatar uploads are not available."))
		return
	}
	form, err := httpx.Multipart(w, r, avatarMaxBytes+avatarMultipartSlack)
	if err != nil {
		h.Error(w, r, avatarUploadError(err))
		return
	}
	header, err := httpx.FormFile(form, "avatar")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if err := h.photo.UpdateAvatar(r.Context(), identity.UserID, identity.UserID, identity.TenantID, header); err != nil {
		h.Error(w, r, err)
		return
	}
	target, err := h.service.GetRecord(r.Context(), identity.UserID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *target))
}
