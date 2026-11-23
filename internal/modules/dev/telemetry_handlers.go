package dev

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
)

// Telemetry handlers: request inspector, logs, exceptions and prune.

func (h *Handler) listRequests(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	status := int32(0)
	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 100 || value > 599 {
			h.Error(w, r, apperr.Validation(map[string][]string{
				"status": {"The status must be a valid HTTP status code."},
			}))
			return
		}
		status = int32(value)
	}
	tenantID, err := parseOptionalID(query.Get("tenant_id"), "tenant_id")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	userID, err := parseOptionalID(query.Get("user_id"), "user_id")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.telemetry.ListRequests(r.Context(), RequestFilter{
		BeforeID:  beforeID,
		Limit:     int32(limit),
		Method:    strings.ToUpper(strings.TrimSpace(query.Get("method"))),
		Status:    status,
		Search:    strings.TrimSpace(query.Get("search")),
		TenantID:  tenantID,
		UserID:    userID,
		RequestID: strings.TrimSpace(query.Get("request_id")),
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(items) == limit && len(items) > 0 {
		nextCursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, items, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) getRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	detail, err := h.telemetry.GetRequestDetail(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if detail == nil {
		h.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "Captured request with ID '%d' was not found.", id))
		return
	}
	h.Data(w, r, http.StatusOK, detail)
}

func (h *Handler) listLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.telemetry.ListLogs(r.Context(), beforeID, int32(limit),
		strings.ToLower(strings.TrimSpace(query.Get("level"))),
		strings.TrimSpace(query.Get("request_id")),
		strings.TrimSpace(query.Get("search")))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(items) == limit && len(items) > 0 {
		nextCursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, items, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) listExceptions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.telemetry.ListExceptions(r.Context(), beforeID, int32(limit),
		strings.TrimSpace(query.Get("request_id")))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(items) == limit && len(items) > 0 {
		nextCursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, items, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) getLog(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.telemetry.GetLog(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if item == nil {
		h.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "Captured log with ID '%d' was not found.", id))
		return
	}
	h.Data(w, r, http.StatusOK, item)
}

func (h *Handler) getException(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.telemetry.GetException(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if item == nil {
		h.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "Captured exception with ID '%d' was not found.", id))
		return
	}
	h.Data(w, r, http.StatusOK, item)
}

type pruneTelemetryRequest struct {
	RetentionDays int `json:"retention_days" validate:"omitempty,min=1,max=3650"`
}

func (h *Handler) pruneTelemetry(w http.ResponseWriter, r *http.Request) {
	var request pruneTelemetryRequest
	if !h.Bind(w, r, &request) {
		return
	}
	days, deleted, err := h.telemetry.Prune(r.Context(), authn.UserID(r), request.RetentionDays)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"retention_days": days, "deleted": deleted})
}
