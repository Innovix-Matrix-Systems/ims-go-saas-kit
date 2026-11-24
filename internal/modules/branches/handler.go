package branches

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Handler serves branch HTTP endpoints.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the branch handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)

	page, perPage, offset := httpx.PageParams(r)
	orderBy, orderDir := httpx.OrderParams(r, "id", "name", "created_at")
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}

	items, total, err := h.service.List(r.Context(), scope, filter)
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
	scope := tenancy.From(r)
	branch, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*branch))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	branch, err := h.service.Create(r.Context(), scope, authn.UserID(r), req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*branch))
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
	scope := tenancy.From(r)
	branch, err := h.service.Update(r.Context(), scope, authn.UserID(r), id, req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*branch))
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

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
