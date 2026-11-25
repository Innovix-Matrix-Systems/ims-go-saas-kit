package dev

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// Inspector panel: routes, roles & permissions, config and packages.

func (h *Handler) inspectorRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.inspector.Routes()
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, routes)
}

func (h *Handler) inspectorAccess(w http.ResponseWriter, r *http.Request) {
	report, err := h.inspector.Access(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, report)
}

func (h *Handler) inspectorConfig(w http.ResponseWriter, r *http.Request) {
	h.Data(w, r, http.StatusOK, h.inspector.Config())
}

func (h *Handler) inspectorPackages(w http.ResponseWriter, r *http.Request) {
	h.Data(w, r, http.StatusOK, h.inspector.Packages())
}

// PoolSource is the database registry's pool state (database.Registry).
type PoolSource interface {
	PoolStats() []database.PoolStats
}

// DatabasePools is the GET /dev/api/inspector/database payload: every
// connection pool, read live (not the cached health report).
type DatabasePools struct {
	CheckedAt time.Time            `json:"checked_at"`
	BusyShare float64              `json:"busy_share"`
	Pools     []database.PoolStats `json:"pools"`
}

func (h *Handler) inspectorDatabase(w http.ResponseWriter, r *http.Request) {
	out := DatabasePools{CheckedAt: time.Now(), BusyShare: database.PoolBusyShare, Pools: []database.PoolStats{}}
	if h.pools != nil {
		out.Pools = h.pools.PoolStats()
	}
	h.Data(w, r, http.StatusOK, out)
}

// Commands panel: allowlisted maintenance commands and their run history.

func (h *Handler) listCommands(w http.ResponseWriter, r *http.Request) {
	commands, err := h.commands.List(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, commands)
}

func (h *Handler) listCommandRuns(w http.ResponseWriter, r *http.Request) {
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
	runs, err := h.commands.Runs(r.Context(), query.Get("command"), beforeID, int32(limit))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(runs) == limit {
		nextCursor = strconv.FormatInt(runs[len(runs)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, runs, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) getCommandRun(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	run, err := h.commands.Get(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, run)
}

type runCommandRequest struct {
	Options map[string]string `json:"options"`
}

func (h *Handler) runCommand(w http.ResponseWriter, r *http.Request) {
	var request runCommandRequest
	if r.ContentLength != 0 && !h.Bind(w, r, &request) {
		return
	}
	// Clients may escape the ':' in "permission:sync".
	name, err := url.PathUnescape(chi.URLParam(r, "name"))
	if err != nil || name == "" {
		h.Error(w, r, apperr.New(apperr.InvalidInput, "A command name is required."))
		return
	}
	run, err := h.commands.Run(r.Context(), authn.UserID(r), name, request.Options)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	status := http.StatusOK
	if run.Status == runStatusRunning {
		status = http.StatusAccepted
	}
	h.Data(w, r, status, run)
}
