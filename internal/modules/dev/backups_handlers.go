package dev

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/backup"
)

var backupStatuses = map[string]bool{
	backup.StatusPending:   true,
	backup.StatusRunning:   true,
	backup.StatusCompleted: true,
	backup.StatusPartial:   true,
	backup.StatusFailed:    true,
}

type backupStatsResponse struct {
	TotalRuns      int64      `json:"total_runs"`
	TotalSize      int64      `json:"total_size"`
	SuccessfulRuns int64      `json:"successful_runs"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	Status         string     `json:"status"`
	Summary        string     `json:"summary"`
	AgeHours       float64    `json:"age_hours"`
}

func (h *Handler) listBackups(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	beforeID, err := parseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !backupStatuses[status] {
		h.Error(w, r, apperr.Validation(map[string][]string{
			"status": {"The status is invalid."},
		}))
		return
	}
	runs, err := h.backups.List(r.Context(), backup.ListFilter{
		Status:   status,
		BeforeID: beforeID,
		Limit:    int32(limit),
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(runs) == limit && len(runs) > 0 {
		nextCursor = strconv.FormatInt(runs[len(runs)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, runs, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) backupStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.backups.Stats(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	monitor, err := h.backups.Monitor(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, backupStatsResponse{
		TotalRuns:      stats.TotalRuns,
		TotalSize:      stats.TotalSize,
		SuccessfulRuns: stats.SuccessfulRuns,
		LastSuccessAt:  stats.LastSuccessAt,
		Status:         monitor.Status,
		Summary:        monitor.Summary,
		AgeHours:       monitor.AgeHours,
	})
}

func (h *Handler) backupConfig(w http.ResponseWriter, r *http.Request) {
	h.Data(w, r, http.StatusOK, h.backups.Config(r.Context()))
}

func (h *Handler) createBackup(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	started := time.Now()
	run, opErr := h.backups.Enqueue(r.Context(), backup.TriggerDashboard, actorID)
	params := map[string]any{"trigger": backup.TriggerDashboard}
	if run != nil {
		params["run_id"] = run.ID
		params["filename"] = run.Filename
		params["status"] = run.Status
	}
	h.audit.Record(r.Context(), actorID, "backups.create", "backups", params, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusAccepted, run)
}

func (h *Handler) getBackup(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	run, err := h.backups.Get(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, run)
}

func (h *Handler) downloadBackup(w http.ResponseWriter, r *http.Request) {
	// A backup is a full copy of every tenant's data: viewing the dashboard
	// (dev.view) is not enough to take one home (H14); the route needs
	// dev.manage.
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	download, err := h.backups.Open(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	defer download.Reader.Close()

	w.Header().Set("Content-Type", "application/gzip")
	if value := mime.FormatMediaType("attachment", map[string]string{"filename": download.FileName}); value != "" {
		w.Header().Set("Content-Disposition", value)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	if download.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(download.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, download.Reader)
}

func (h *Handler) deleteBackup(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	run, getErr := h.backups.Get(r.Context(), id)
	started := time.Now()
	var opErr error
	if getErr != nil {
		opErr = getErr
	} else {
		opErr = h.backups.Delete(r.Context(), id)
	}
	params := map[string]any{"run_id": id}
	if run != nil {
		params["filename"] = run.Filename
		params["size_bytes"] = run.SizeBytes
	}
	h.audit.Record(r.Context(), actorID, "backups.delete", formatTarget("backup", id), params, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.NoContent(w)
}

type cleanBackupsRequest struct {
	DryRun bool `json:"dry_run"`
}

func (h *Handler) cleanBackups(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	var request cleanBackupsRequest
	if r.ContentLength != 0 && !h.Bind(w, r, &request) {
		return
	}
	started := time.Now()
	result, opErr := h.backups.Clean(r.Context(), request.DryRun)
	h.audit.Record(r.Context(), actorID, "backups.clean", "backups", map[string]any{
		"dry_run":    result.DryRun,
		"candidates": result.Candidates,
		"deleted":    result.Deleted,
		"failed":     result.Failed,
	}, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	h.Data(w, r, http.StatusOK, result)
}
