package identity

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// User CRUD, role assignment and password changes (tenant and internal).

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)

	page, perPage, offset := httpx.PageParams(r)
	orderBy, orderDir := httpx.OrderParams(r, "id", "name", "email", "created_at")
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		RoleName: r.URL.Query().Get("role_name"),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}
	if raw := r.URL.Query().Get("is_active"); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			filter.IsActive = &value
		}
	}
	// Staff accounts only (no tenant); only a global reader sees any.
	filter.PlatformOnly, _ = strconv.ParseBool(r.URL.Query().Get("platform"))
	// One organization's members, for a reader of every tenant (the service
	// ignores it for an organization's own members, who only see theirs).
	if raw := r.URL.Query().Get("tenant_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			h.Error(w, r, apperr.Validation(map[string][]string{"tenant_id": {"The tenant_id must be a tenant id."}}))
			return
		}
		filter.TenantID = id
	}
	switch raw := r.URL.Query().Get("invitation"); raw {
	case "", InvitationPending, InvitationAccepted:
		filter.Invitation = raw
	default:
		h.Error(w, r, apperr.Validation(map[string][]string{"invitation": {"The invitation filter must be pending or accepted."}}))
		return
	}

	users, total, err := h.service.List(r.Context(), scope, filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	photos := map[int64]string{}
	if h.photo != nil {
		ids := make([]int64, 0, len(users))
		for _, user := range users {
			ids = append(ids, user.ID)
		}
		if resolved, err := h.photo.PhotoURLs(r.Context(), ids); err == nil {
			photos = resolved
		}
	}
	ids := make([]int64, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	roles, err := h.checker.RoleRefsForUsers(r.Context(), ids)
	if err != nil {
		roles = map[int64][]authz.RoleRef{}
	}
	data := make([]Response, 0, len(users))
	for _, user := range users {
		data = append(data, NewResponse(user, roles[user.ID], stringPtr(photos[user.ID])))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if id != identity.UserID {
		if err := h.checker.MustCan(r.Context(), identity.UserID, authz.PermUserView); err != nil {
			h.Error(w, r, err)
			return
		}
	}
	scope := tenancy.From(r)
	user, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	user, err := h.service.Create(r.Context(), scope, authn.UserID(r), req.toCreateInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *user))
}

func (h *Handler) storeInternal(w http.ResponseWriter, r *http.Request) {
	var req InternalInsertRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	input := req.InsertUpdateRequest.toCreateInput()
	input.TenantID = req.TenantID
	user, err := h.service.Create(r.Context(), scope, authn.UserID(r), input)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, h.responseFor(r.Context(), *user))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	h.updateTarget(w, r, authz.PermUserRoleAssign)
}

func (h *Handler) updateInternal(w http.ResponseWriter, r *http.Request) {
	h.updateTarget(w, r, authz.PermUserInternalRoleAssign)
}

// updateTarget applies a user update. Changing roles through the update
// payload needs the role-assign permission too, not just update.
func (h *Handler) updateTarget(w http.ResponseWriter, r *http.Request, rolePerm string) {
	identity, _ := authn.IdentityFromContext(r.Context())
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if req.Roles != nil {
		if err := h.checker.MustCan(r.Context(), identity.UserID, rolePerm); err != nil {
			h.Error(w, r, err)
			return
		}
	}
	scope := tenancy.From(r)
	user, err := h.service.Update(r.Context(), scope, identity.UserID, id, UpdateParams{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Name:      strings.TrimSpace(req.Name),
		Phone:     req.Phone,
		IsActive:  req.IsActive,
		BranchID:  req.BranchID,
		Roles:     req.Roles,

		ExpectedUpdatedAt: req.UpdatedAt,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	scope := tenancy.From(r)
	if err := h.service.Delete(r.Context(), scope, authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) assignRole(w http.ResponseWriter, r *http.Request) {
	var req AssignRoleRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	user, err := h.service.AssignRoles(r.Context(), scope, authn.UserID(r), req.UserID, req.Roles)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	if err := h.service.ChangePassword(r.Context(), scope, authn.UserID(r), req.UserID, req.Password); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Password changed successfully.")
}

func (h *Handler) resetTwoFactor(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.ResetTwoFactor(r.Context(), tenancy.From(r), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// resendInvitation mails a new invitation (tenant and internal routes: the
// scope decides whose users are reachable).
func (h *Handler) resendInvitation(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.ResendInvitation(r.Context(), tenancy.From(r), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "The invitation has been sent.")
}

func (h *Handler) switchBranch(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	scope := tenancy.From(r)
	user, err := h.service.SwitchBranch(r.Context(), scope, authn.UserID(r), branchID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

// clearBranch goes back to every branch.
func (h *Handler) clearBranch(w http.ResponseWriter, r *http.Request) {
	user, err := h.service.ClearBranch(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}

// changePasswordInternal sets another user's password across tenants.
func (h *Handler) changePasswordInternal(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	if err := h.service.ChangePassword(r.Context(), scope, authn.UserID(r), req.UserID, req.Password); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Password changed successfully.")
}

// destroyInternal soft-deletes a user across tenants.
func (h *Handler) destroyInternal(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	scope := tenancy.From(r)
	if err := h.service.Delete(r.Context(), scope, authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// assignRoleInternal replaces a user's roles across tenants.
func (h *Handler) assignRoleInternal(w http.ResponseWriter, r *http.Request) {
	var req AssignRoleRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	user, err := h.service.AssignRoles(r.Context(), scope, authn.UserID(r), req.UserID, req.Roles)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, h.responseFor(r.Context(), *user))
}
