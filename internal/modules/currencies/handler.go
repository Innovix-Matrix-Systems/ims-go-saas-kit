package currencies

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

// Handler serves currency HTTP endpoints.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the currency handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

// public lists the enabled currencies and the default for clients that
// write amounts (the app loads it at start). Browsers must ask again each
// time (the list is cached on the server): a staff change shows everywhere at
// once instead of after a browser's cache expires.
func (h *Handler) public(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Enabled(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	h.Data(w, r, http.StatusOK, NewPublicResponse(items))
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, item := range items {
		data = append(data, NewResponse(item))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	currency, err := h.service.Create(r.Context(), authn.UserID(r), req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*currency))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	currency, err := h.service.Update(r.Context(), authn.UserID(r), id, req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*currency))
}

func (h *Handler) makeDefault(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	currency, err := h.service.SetDefault(r.Context(), authn.UserID(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*currency))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
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
