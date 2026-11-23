package notifications

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Handler serves notification HTTP endpoints.
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit
}

// NewHandler creates the notification handler.
func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)

	filter, err := feedFilter(r)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	page, err := h.service.Feed(r.Context(), scope, identity.UserID, filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}

	data := make([]FeedItemResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, NewFeedResponse(item))
	}
	meta := FeedMeta{
		Path:        r.URL.Path,
		PerPage:     page.PerPage,
		NextCursor:  page.NextCursor,
		PrevCursor:  page.PrevCursor,
		UnreadCount: page.UnreadCount,
	}
	links := FeedLinks{
		Prev: httpx.CursorURL(r, page.PrevCursor),
		Next: httpx.CursorURL(r, page.NextCursor),
	}
	h.Cursor(w, r, http.StatusOK, data, meta, links)
}

func feedFilter(r *http.Request) (FeedFilter, error) {
	query := r.URL.Query()
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return FeedFilter{}, apperr.Validation(map[string][]string{
				"limit": {"The limit must be a positive integer."},
			})
		}
		limit = parsed
	}
	if limit > 100 {
		limit = 100
	}
	filter := FeedFilter{Limit: int32(limit), Cursor: DecodeCursor(query.Get("cursor"))}
	if raw := query.Get("unread_only"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return FeedFilter{}, apperr.Validation(map[string][]string{
				"unread_only": {"The unread only field must be true or false."},
			})
		}
		filter.UnreadOnly = parsed
	}
	if priority := query.Get("priority"); priority != "" {
		if _, ok := Priorities[priority]; !ok {
			return FeedFilter{}, apperr.Validation(map[string][]string{
				"priority": {"The selected priority is invalid."},
			})
		}
		filter.Priority = priority
	}
	return filter, nil
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.MarkRead(r.Context(), scope, identity.UserID, id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) markAllRead(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	scope := tenancy.From(r)
	if err := h.service.MarkAllRead(r.Context(), scope, identity.UserID); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	scope := tenancy.From(r)
	page, perPage, offset := httpx.PageParams(r)
	orderBy, orderDir := httpx.OrderParams(r, "created_at", "created_at", "title", "priority")
	filter := ListFilter{
		Search:   r.URL.Query().Get("search"),
		Priority: r.URL.Query().Get("priority"),
		OrderBy:  orderBy,
		OrderDir: orderDir,
		Limit:    int32(perPage),
		Offset:   int32(offset),
	}
	if filter.Priority != "" {
		if _, ok := Priorities[filter.Priority]; !ok {
			h.Error(w, r, apperr.Validation(map[string][]string{
				"priority": {"The selected priority is invalid."},
			}))
			return
		}
	}

	items, total, err := h.service.List(r.Context(), scope, filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]Response, 0, len(items))
	for _, item := range items {
		targets, err := h.service.Targets(r.Context(), item.ID)
		if err != nil {
			h.Error(w, r, err)
			return
		}
		data = append(data, NewResponse(item, targets))
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
	notification, err := h.service.Get(r.Context(), scope, id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	targets, err := h.service.Targets(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*notification, targets))
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) {
		return
	}
	scope := tenancy.From(r)
	notification, targets, err := h.service.Create(r.Context(), scope, authn.UserID(r), req.toCreateInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusCreated, NewResponse(*notification, targets))
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
	notification, targets, err := h.service.Update(r.Context(), scope, authn.UserID(r), id, req.toUpdateInput())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*notification, targets))
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	scope := tenancy.From(r)
	if err := h.service.Delete(r.Context(), scope, id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func (req InsertUpdateRequest) toCreateInput() CreateInput {
	return CreateInput{
		TenantID:  req.TenantID,
		EventType: req.EventType,
		Priority:  req.Priority,
		Title:     req.Title,
		Body:      req.Body,
		Data:      dataJSON(req.Data),
		GroupKey:  req.GroupKey,
		ExpiresAt: req.ExpiresAt,
		Targets:   targetsInput(req.Targets),
		Channels:  req.Channels,
	}
}

func (req InsertUpdateRequest) toUpdateInput() UpdateInput {
	return UpdateInput{
		EventType: req.EventType,
		Priority:  req.Priority,
		Title:     req.Title,
		Body:      req.Body,
		Data:      dataJSON(req.Data),
		GroupKey:  req.GroupKey,
		ExpiresAt: req.ExpiresAt,
		Targets:   targetsInput(req.Targets),
	}
}

func targetsInput(requests []TargetRequest) []TargetInput {
	targets := make([]TargetInput, 0, len(requests))
	for _, request := range requests {
		targets = append(targets, TargetInput{Type: request.Type, ID: request.ID})
	}
	return targets
}

func dataJSON(value map[string]any) []byte {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}

// preferences lists the caller's notification preferences: every event type
// × channel it may use, with the caller's choice (mandatory ones read-only).
func (h *Handler) preferences(w http.ResponseWriter, r *http.Request) {
	preferences, err := h.service.Preferences(r.Context(), authn.UserID(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, preferences)
}

func (h *Handler) setPreferences(w http.ResponseWriter, r *http.Request) {
	var req PreferencesRequest
	if !h.Bind(w, r, &req) {
		return
	}
	inputs := make([]PreferenceInput, 0, len(req.Preferences))
	for _, preference := range req.Preferences {
		inputs = append(inputs, PreferenceInput{EventType: preference.EventType, Channel: preference.Channel, Enabled: preference.Enabled})
	}
	if err := h.service.SetPreferences(r.Context(), authn.UserID(r), inputs); err != nil {
		h.Error(w, r, err)
		return
	}
	h.preferences(w, r)
}

// unsubscribe is the one-click link of notification mails (public: the
// sealed token names the user and the event type). GET for a click, POST
// for mail clients' one-click unsubscribe (RFC 8058).
func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" && r.Method == http.MethodPost {
		var req UnsubscribeRequest
		if !h.Bind(w, r, &req) {
			return
		}
		token = req.Token
	}
	if err := h.service.Unsubscribe(r.Context(), token); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "You will no longer receive these notifications by e-mail, SMS or push. The in-app feed keeps them.")
}
