package webhooks

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Handler serves the webhook endpoints API.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the webhook handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	h.Data(w, r, http.StatusOK, Events())
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	endpoints, err := h.service.List(r.Context(), tenancy.From(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	out := make([]Response, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, NewResponse(endpoint))
	}
	h.Data(w, r, http.StatusOK, out)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	endpoint, secret, err := h.service.Create(r.Context(), tenancy.From(r), authn.UserID(r), CreateInput{
		URL: req.URL, Description: req.Description, Events: req.Events,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, SecretResponse{Response: NewResponse(*endpoint), Secret: secret})
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	endpoint, err := h.service.Get(r.Context(), tenancy.From(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*endpoint))
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
	endpoint, err := h.service.Update(r.Context(), tenancy.From(r), authn.UserID(r), id, UpdateInput{
		URL: req.URL, Description: req.Description, Events: req.Events, IsActive: req.IsActive,
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*endpoint))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), tenancy.From(r), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) rotateSecret(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	endpoint, secret, err := h.service.RotateSecret(r.Context(), tenancy.From(r), authn.UserID(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, SecretResponse{Response: NewResponse(*endpoint), Secret: secret})
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	delivery, err := h.service.Test(r.Context(), tenancy.From(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, delivery)
}

func (h *Handler) deliveries(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	query := r.URL.Query()
	filter := DeliveryFilter{Status: strings.TrimSpace(query.Get("status")), Limit: 25}
	if limit, err := strconv.Atoi(query.Get("limit")); err == nil {
		filter.Limit = int32(min(max(limit, 1), 100)) // #nosec G115 -- clamped to 1..100
	}
	if before, err := strconv.ParseInt(query.Get("cursor"), 10, 64); err == nil {
		filter.BeforeID = before
	}
	deliveries, err := h.service.Deliveries(r.Context(), tenancy.From(r), id, filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	next := ""
	if len(deliveries) == int(filter.Limit) {
		next = strconv.FormatInt(deliveries[len(deliveries)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, deliveries, map[string]any{"next_cursor": next}, nil)
}

func (h *Handler) redeliver(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	delivery, err := h.service.Redeliver(r.Context(), tenancy.From(r), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, delivery)
}
