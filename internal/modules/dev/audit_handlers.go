package dev

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

// Audit log panel: every sign-in and every action taken in the dashboard,
// filterable by action, person, result, date range and free text.

func (h *Handler) listActions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	filter, err := actionFilterFromQuery(query.Get)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	filter.Limit = int32(limit)
	actions, total, err := h.audit.Search(r.Context(), filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(actions) == limit {
		nextCursor = strconv.FormatInt(actions[len(actions)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, actions, map[string]any{"next_cursor": nextCursor, "total": total}, nil)
}

func (h *Handler) getAction(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	action, err := h.audit.Get(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, action)
}

func (h *Handler) actionFacets(w http.ResponseWriter, r *http.Request) {
	kinds, actors, err := h.audit.Facets(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, map[string]any{"actions": kinds, "actors": actors})
}

func (h *Handler) actionStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.audit.Stats(r.Context(), h.auditRetentionDays)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func actionFilterFromQuery(get func(string) string) (ActionFilter, error) {
	filter := ActionFilter{
		Search: strings.TrimSpace(get("search")),
		Action: strings.TrimSpace(get("action")),
		Status: strings.TrimSpace(get("status")),
	}
	if filter.Status != "" && filter.Status != actionStatusOK && filter.Status != "error" {
		return filter, apperr.Validation(map[string][]string{"status": {"The status must be ok or error."}})
	}
	var err error
	if filter.ActorID, err = parseOptionalID(get("actor_id"), "actor_id"); err != nil {
		return filter, err
	}
	if filter.BeforeID, err = parseCursor(get("cursor")); err != nil {
		return filter, err
	}
	if filter.From, err = parseOptionalTime(get("from"), "from", false); err != nil {
		return filter, err
	}
	to, err := parseOptionalTime(get("to"), "to", true)
	if err != nil {
		return filter, err
	}
	if to != nil {
		end := to.Add(time.Nanosecond) // inclusive end of day
		filter.To = &end
	}
	return filter, nil
}
