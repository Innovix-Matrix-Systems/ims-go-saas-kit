package oauth

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Handler serves the module's HTTP side: the OAuth endpoints
// (token_handler.go), the sign-in and consent pages (authorize.go), the
// caller's connections and registered-client management.
type Handler struct {
	service *Service
	pages   *Pages
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the handler.
func NewHandler(service *Service, pages *Pages, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, pages: pages, checker: checker, Kit: kit}
}

func (h *Handler) connections(w http.ResponseWriter, r *http.Request) {
	grants, err := h.service.Connections(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]ConnectionResponse, 0, len(grants))
	for _, g := range grants {
		data = append(data, NewConnectionResponse(g))
	}
	h.Data(w, r, http.StatusOK, data)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Disconnect(r.Context(), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	clients, total, err := h.service.ListClients(r.Context(), r.URL.Query().Get("search"), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]ClientResponse, 0, len(clients))
	for _, c := range clients {
		data = append(data, NewClientResponse(c, ""))
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req ClientRequest
	if !h.Bind(w, r, &req) {
		return
	}
	created, err := h.service.CreateClient(r.Context(), authn.UserID(r), req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewClientResponse(created.ClientRecord, created.Secret))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	client, err := h.service.GetClient(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewClientResponse(*client, ""))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req ClientRequest
	if !h.Bind(w, r, &req) {
		return
	}
	client, err := h.service.UpdateClient(r.Context(), authn.UserID(r), id, req.toInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewClientResponse(*client, ""))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteClient(r.Context(), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) rotate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	rotated, err := h.service.RotateSecret(r.Context(), authn.UserID(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewClientResponse(rotated.ClientRecord, rotated.Secret))
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

// Gate answers 404 when the MCP server is disabled (MCP_ENABLED off), like
// /dev. It guards /mcp, /oauth/* and the OAuth well-known documents.
func Gate(enabled bool, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			responder.Error(w, r, apperr.New(apperr.ResourceNotFound, "The requested resource was not found."))
		})
	}
}
