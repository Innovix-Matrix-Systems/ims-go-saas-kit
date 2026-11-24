package announcements

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Handler serves announcements.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	status := r.URL.Query().Get("status")
	if status != "" && status != StatusDraft && status != StatusPublished && status != StatusArchived {
		h.Error(w, r, apperr.Validation(map[string][]string{"status": {"The status must be draft, published or archived."}}))
		return
	}
	items, total, err := h.service.List(r.Context(), status, int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	now := h.service.now()
	data := make([]ListedResponse, 0, len(items))
	for _, item := range items {
		data = append(data, ListedResponse{Response: newResponse(item.Announcement, now), TenantCount: item.TenantCount, Totals: item.Totals})
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	h.respondDetail(w, r, id, http.StatusOK)
}

func (h *Handler) respondDetail(w http.ResponseWriter, r *http.Request, id int64, status int) {
	detail, err := h.service.Detail(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, status, DetailResponse{
		Response: newResponse(detail.Announcement, h.service.now()), Tenants: detail.Tenants, Totals: detail.Totals,
	})
}

// organizations is a page of the organizations whose members reacted, with
// their counts (?search= by name).
func (h *Handler) organizations(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	page, perPage, offset := httpx.PageParams(r)
	items, total, err := h.service.TenantTotalsPage(r.Context(), id, r.URL.Query().Get("search"), int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, items, meta, links)
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req Request
	if !h.Bind(w, r, &req) {
		return
	}
	created, err := h.service.Create(r.Context(), authn.UserID(r), req.input())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondDetail(w, r, created.ID, http.StatusCreated)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req Request
	if !h.Bind(w, r, &req) {
		return
	}
	if _, err := h.service.Update(r.Context(), authn.UserID(r), id, req.input()); err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondDetail(w, r, id, http.StatusOK)
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

// live is every member's: the announcements their organization should see.
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	tenantID := tenancy.From(r).EffectiveTenantID()
	if tenantID == 0 {
		h.Data(w, r, http.StatusOK, []LiveResponse{})
		return
	}
	items, err := h.service.Live(r.Context(), tenantID, authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]LiveResponse, 0, len(items))
	for _, a := range items {
		data = append(data, newLiveResponse(a))
	}
	h.Data(w, r, http.StatusOK, data)
}

// track records a member's view, click or dismissal.
func (h *Handler) track(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	var req EventRequest
	if !h.Bind(w, r, &req) {
		return
	}
	tenantID := tenancy.From(r).EffectiveTenantID()
	if tenantID == 0 {
		h.Error(w, r, apperr.New(apperr.ResourceNotFound, "Announcement not found."))
		return
	}
	if err := h.service.Track(r.Context(), tenantID, authn.UserID(r), id, req.Type); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}
