package settings

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Handler serves the grouped settings REST surface.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the settings handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

// index returns the caller's effective settings, grouped for the UI.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)

	canWriteTenant, err := h.checker.Can(r.Context(), identity.UserID, authz.PermSettingUpdate)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.service.EffectiveSettings(r.Context(), scope)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	groups := groupSettings(items, strings.TrimSpace(r.URL.Query().Get("group")), canWriteTenant)
	h.Data(w, r, http.StatusOK, groups)
}

// updateTenant upserts the caller's tenant override.
func (h *Handler) updateTenant(w http.ResponseWriter, r *http.Request) {
	var req TenantUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	effective, err := h.service.SetTenant(r.Context(), scope, chi.URLParam(r, "key"), req.Value)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newSettingItem(*effective, true, true))
}

// internalIndex returns every catalog key's effective value for a target.
func (h *Handler) internalIndex(w http.ResponseWriter, r *http.Request) {
	target, err := targetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.service.EffectiveForTarget(r.Context(), target)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, toSettingItems(items, true))
}

// internalUpdate upserts a setting for the target named in the body.
func (h *Handler) internalUpdate(w http.ResponseWriter, r *http.Request) {
	var req UpdateSettingRequest
	if !h.Bind(w, r, &req) {
		return
	}
	effective, err := h.service.SetTarget(r.Context(), chi.URLParam(r, "key"), targetFromRequest(req.TargetType, req.TargetID), req.Value)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newSettingItem(*effective, true, true))
}

// internalDelete removes the override at the target named in the query.
func (h *Handler) internalDelete(w http.ResponseWriter, r *http.Request) {
	target, err := targetFromQuery(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if err := h.service.DeleteTarget(r.Context(), chi.URLParam(r, "key"), target); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// groupsIndex lists settings groups (paginated, or lightweight when
// ?is_light_weight is truthy).
func (h *Handler) groupsIndex(w http.ResponseWriter, r *http.Request) {
	orderBy, orderDir := groupOrderParams(r)

	if isTruthy(r.URL.Query().Get("is_light_weight")) {
		groups, err := h.service.ListGroupsLight(r.Context(), orderBy, orderDir)
		if err != nil {
			h.Error(w, r, err)
			return
		}
		data := make([]GroupLightResponse, 0, len(groups))
		for _, group := range groups {
			data = append(data, newGroupLightResponse(group))
		}
		h.Data(w, r, http.StatusOK, data)
		return
	}

	page, perPage, offset := httpx.PageParams(r)
	filter := GroupListFilter{
		Search:   strings.TrimSpace(r.URL.Query().Get("search")),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}
	groups, total, err := h.service.ListGroups(r.Context(), filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]GroupResponse, 0, len(groups))
	for _, group := range groups {
		data = append(data, newGroupResponse(group))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

// groupsShow returns one settings group.
func (h *Handler) groupsShow(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	group, err := h.service.GetGroup(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newGroupResponse(*group))
}

// groupsStore creates a settings group.
func (h *Handler) groupsStore(w http.ResponseWriter, r *http.Request) {
	var req GroupInsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	group, err := h.service.CreateGroup(r.Context(), authn.UserID(r), req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, newGroupResponse(*group))
}

// groupsUpdate updates a settings group.
func (h *Handler) groupsUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req GroupInsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	group, err := h.service.UpdateGroup(r.Context(), authn.UserID(r), id, req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newGroupResponse(*group))
}

// groupsDestroy deletes a settings group.
func (h *Handler) groupsDestroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteGroup(r.Context(), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

// assignGroup assigns a catalog key's global row to a settings group.
func (h *Handler) assignGroup(w http.ResponseWriter, r *http.Request) {
	var req AssignGroupRequest
	if !h.Bind(w, r, &req) {
		return
	}
	effective, err := h.service.AssignKeyGroup(r.Context(), chi.URLParam(r, "key"), req.GroupID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, newSettingItem(*effective, true, true))
}

func groupOrderParams(r *http.Request) (orderBy, orderDir string) {
	orderBy = r.URL.Query().Get("order_by")
	switch orderBy {
	case "name", "created_at", "sort_order":
	default:
		orderBy = "sort_order"
	}
	orderDir = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("order_direction")))
	if orderDir != "asc" && orderDir != "desc" {
		orderDir = "asc"
	}
	return orderBy, orderDir
}

func isTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func targetFromRequest(targetType string, targetID *int64) Target {
	target := Target{Type: targetType}
	if targetType == "" || targetType == "global" {
		target.Type = TargetGlobal
	}
	// Preserve a provided id even for the global target so the service rejects
	// `global` + `target_id` (the value must not be silently ignored).
	if targetID != nil {
		target.ID = *targetID
	}
	return target
}

// targetFromQuery parses the settings target query parameters (global default).
func targetFromQuery(r *http.Request) (Target, error) {
	rawType := strings.TrimSpace(r.URL.Query().Get("target_type"))
	targetType := TargetGlobal
	switch rawType {
	case "", "global":
	case string(TargetTenant):
		targetType = TargetTenant
	default:
		return Target{}, apperr.Validation(map[string][]string{
			"target_type": {"The selected target_type is not supported for settings."},
		})
	}

	var id int64
	if rawID := strings.TrimSpace(r.URL.Query().Get("target_id")); rawID != "" {
		parsed, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || parsed < 0 {
			return Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be a positive integer."},
			})
		}
		id = parsed
	}

	if targetType == TargetGlobal {
		if id != 0 {
			return Target{}, apperr.Validation(map[string][]string{
				"target_id": {"The target_id must be omitted for the global target."},
			})
		}
		return Target{Type: TargetGlobal}, nil
	}
	if id <= 0 {
		return Target{}, apperr.Validation(map[string][]string{
			"target_id": {"The target_id is required for the tenant target."},
		})
	}
	return Target{Type: TargetTenant, ID: id}, nil
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
