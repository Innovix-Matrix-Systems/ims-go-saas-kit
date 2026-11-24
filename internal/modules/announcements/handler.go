package announcements

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Request is what staff send to create or update an announcement.
type Request struct {
	Title       string     `json:"title" validate:"required,max=255"`
	Body        *string    `json:"body" validate:"omitempty,max=5000"`
	Kind        string     `json:"kind" validate:"required,oneof=info promotion warning critical"`
	CTALabel    *string    `json:"cta_label" validate:"omitempty,max=60"`
	CTAURL      *string    `json:"cta_url" validate:"omitempty,max=2048"`
	TenantIDs   []int64    `json:"tenant_ids" validate:"omitempty,max=1000,dive,gt=0"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Dismissible *bool      `json:"dismissible"`
	Status      string     `json:"status" validate:"omitempty,oneof=draft published archived"`
	Channels    []string   `json:"channels" validate:"omitempty,max=4,dive,oneof=database mail sms push"`
}

func (r Request) input() Input {
	dismissible := true
	if r.Dismissible != nil {
		dismissible = *r.Dismissible
	}
	return Input{
		Title: r.Title, Body: r.Body, Kind: r.Kind, CTALabel: r.CTALabel, CTAURL: r.CTAURL, TenantIDs: r.TenantIDs,
		StartsAt: r.StartsAt, EndsAt: r.EndsAt, Dismissible: dismissible, Status: r.Status, Channels: r.Channels,
	}
}

// EventRequest is a member's view, click or dismissal.
type EventRequest struct {
	Type string `json:"type" validate:"required,oneof=view click dismiss"`
}

// Response is an announcement.
type Response struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Body        *string    `json:"body"`
	Kind        string     `json:"kind"`
	CTALabel    *string    `json:"cta_label"`
	CTAURL      *string    `json:"cta_url"`
	Audience    string     `json:"audience"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Dismissible bool       `json:"dismissible"`
	Status      string     `json:"status"`
	State       string     `json:"state"`
	Channels    []string   `json:"channels"`
	DeliveredAt *time.Time `json:"delivered_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func newResponse(a Announcement, now time.Time) Response {
	return Response{
		ID: a.ID, Title: a.Title, Body: a.Body, Kind: a.Kind, CTALabel: a.CTALabel, CTAURL: a.CTAURL, Audience: a.Audience,
		StartsAt: a.StartsAt, EndsAt: a.EndsAt, Dismissible: a.Dismissible, Status: a.Status, State: a.State(now),
		Channels: a.Channels, DeliveredAt: a.DeliveredAt, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

// ListedResponse is an announcement in the staff list.
type ListedResponse struct {
	Response
	TenantCount int64  `json:"tenant_count"`
	Totals      Totals `json:"totals"`
}

// DetailResponse is an announcement with its audience and counts.
type DetailResponse struct {
	Response
	Tenants  []TenantRef    `json:"tenants"`
	Totals   Totals         `json:"totals"`
	ByTenant []TenantTotals `json:"by_tenant"`
}

// LiveResponse is what members see: the banner's content.
type LiveResponse struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Body        *string    `json:"body"`
	Kind        string     `json:"kind"`
	CTALabel    *string    `json:"cta_label"`
	CTAURL      *string    `json:"cta_url"`
	Dismissible bool       `json:"dismissible"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
}

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

// RegisterRoutes mounts what members use (tenant-resolved): the live
// announcements for their organization and their reactions to them.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/announcements/live", h.live)
	r.Post("/announcements/{id}/events", h.track)
}

// RegisterInternalRoutes mounts the staff management (InternalOnly).
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.With(h.can(authz.PermAnnouncementView)).Get("/internal/announcements", h.index)
	r.With(h.can(authz.PermAnnouncementView)).Get("/internal/announcements/{id}", h.show)
	r.With(h.can(authz.PermAnnouncementManage)).Post("/internal/announcements", h.store)
	r.With(h.can(authz.PermAnnouncementManage)).Patch("/internal/announcements/{id}", h.update)
	r.With(h.can(authz.PermAnnouncementManage)).Delete("/internal/announcements/{id}", h.destroy)
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
		Response: newResponse(detail.Announcement, h.service.now()), Tenants: detail.Tenants, Totals: detail.Totals, ByTenant: detail.ByTenant,
	})
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
		data = append(data, LiveResponse{
			ID: a.ID, Title: a.Title, Body: a.Body, Kind: a.Kind, CTALabel: a.CTALabel, CTAURL: a.CTAURL,
			Dismissible: a.Dismissible, StartsAt: a.StartsAt, EndsAt: a.EndsAt,
		})
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
