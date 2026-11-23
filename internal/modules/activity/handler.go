package activity

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Handler serves the activity feed.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the activity handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

// RegisterRoutes mounts the tenant-resolved activity routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(authz.RequireAny(h.checker, h.Responder, authz.PermActivityView, authz.PermActivityViewAll)).
		Get("/activity", h.index)
}

// Actor is who performed an action (null for system actions).
type Actor struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Response is one activity entry.
type Response struct {
	ID       int64  `json:"id"`
	TenantID *int64 `json:"tenant_id"`
	Actor    *Actor `json:"actor"`
	// ByStaff is true when staff did this while acting as the actor.
	ByStaff bool `json:"by_staff"`
	// Impersonator is who acted; null unless the reader has activity.view.all.
	Impersonator *Actor          `json:"impersonator"`
	Action       string          `json:"action"`
	SubjectType  string          `json:"subject_type"`
	SubjectID    *int64          `json:"subject_id"`
	Changes      json.RawMessage `json:"changes"`
	IP           *string         `json:"ip"`
	RequestID    *string         `json:"request_id"`
	CreatedAt    time.Time       `json:"created_at"`
}

// FeedMeta is the cursor-page metadata.
type FeedMeta struct {
	Path       string  `json:"path"`
	PerPage    int     `json:"per_page"`
	NextCursor *string `json:"next_cursor"`
}

// FeedLinks holds the next-page link.
type FeedLinks struct {
	Next *string `json:"next"`
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := Filter{
		SubjectType: query.Get("subject_type"),
		Cursor:      query.Get("cursor"),
	}
	filter.SubjectID, _ = strconv.ParseInt(query.Get("subject_id"), 10, 64)
	filter.ActorID, _ = strconv.ParseInt(query.Get("actor_id"), 10, 64)
	filter.PerPage, _ = strconv.Atoi(query.Get("per_page"))
	filter.Tenant = query.Get("tenant_id")
	filter.ImpersonatorID, _ = strconv.ParseInt(query.Get("impersonator_id"), 10, 64)
	filter.Impersonated, _ = strconv.ParseBool(query.Get("impersonated"))

	reader, err := h.reader(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	page, err := h.service.Feed(r.Context(), tenancy.From(r), reader, filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, toResponse(item, reader))
	}
	h.Cursor(w, r, http.StatusOK, data,
		FeedMeta{Path: r.URL.Path, PerPage: page.PerPage, NextCursor: page.NextCursor},
		FeedLinks{Next: httpx.CursorURL(r, page.NextCursor)})
}

// reader decides whether the caller reads as a system reader. Someone acting
// as a user reads as that user, so never as a system reader.
func (h *Handler) reader(r *http.Request) (Reader, error) {
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok || identity.Impersonated() {
		return Reader{}, nil
	}
	system, err := h.checker.Can(r.Context(), identity.UserID, authz.PermActivityViewAll)
	if err != nil {
		return Reader{}, err
	}
	return Reader{System: system}, nil
}

func toResponse(item Item, reader Reader) Response {
	response := Response{
		ID: item.ID, TenantID: item.TenantID, Action: item.Action, SubjectType: item.SubjectType,
		SubjectID: item.SubjectID, Changes: item.Changes, IP: item.IP, RequestID: item.RequestID,
		CreatedAt: item.CreatedAt,
		ByStaff:   item.ImpersonatorID != nil || item.ImpersonatorName != nil,
	}
	if reader.System && response.ByStaff {
		impersonator := Actor{}
		if item.ImpersonatorID != nil {
			impersonator.ID = *item.ImpersonatorID
		}
		if item.ImpersonatorName != nil {
			impersonator.Name = *item.ImpersonatorName
		}
		response.Impersonator = &impersonator
	}
	if item.ActorID != nil {
		actor := Actor{ID: *item.ActorID}
		if item.ActorName != nil {
			actor.Name = *item.ActorName
		}
		response.Actor = &actor
	}
	return response
}
