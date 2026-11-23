package dev

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/scheduler"
)

// The Scheduler panel: every registered task with its cron expression, next
// run and recent runs, the run history (schedule_runs), and Run now (the
// same work as `ims schedule:run`).

// scheduleRunStatuses are the statuses the history filter accepts.
var scheduleRunStatuses = map[string]bool{
	scheduler.RunRunning: true, scheduler.RunOK: true, scheduler.RunFailed: true, scheduler.RunInterrupted: true,
}

// scheduledRunResponse is the answer to POST /dev/api/scheduler/{id}/run:
// the run goes on in the background and its outcome is the task's last run.
type scheduledRunResponse struct {
	Task      string    `json:"task"`
	StartedAt time.Time `json:"started_at"`
}

func (h *Handler) requireScheduler(w http.ResponseWriter, r *http.Request) bool {
	if h.scheduler != nil {
		return true
	}
	h.Error(w, r, apperr.New(apperr.ResourceNotFound, "The scheduler is not configured."))
	return false
}

func (h *Handler) schedulerStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireScheduler(w, r) {
		return
	}
	status, err := h.scheduler.Status(r.Context())
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, status)
}

func (h *Handler) runScheduledTask(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	id := chi.URLParam(r, "id")
	if !h.requireScheduler(w, r) {
		return
	}
	if _, ok := h.scheduler.Task(id); !ok {
		h.Error(w, r, apperr.Newf(apperr.ResourceNotFound, "No scheduled task is registered as %q.", id))
		return
	}
	// The overlap check in the runner only sees this process; the run log
	// also sees a run the `ims schedule` process has open.
	running, err := h.scheduler.Running(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	if running {
		h.Error(w, r, apperr.Newf(apperr.ResourceLocked, "%s is still running; wait for it to finish.", id))
		return
	}
	started := time.Now()
	// Detached from the request: a task can outlive it (its own timeout still
	// applies), and the audit record is written when it ends.
	ctx := context.WithoutCancel(r.Context())
	go func() {
		err := h.scheduler.RunNow(ctx, id)
		h.audit.Record(ctx, actorID, "schedule.run", id, map[string]any{"task": id}, started, err)
	}()
	h.Data(w, r, http.StatusAccepted, scheduledRunResponse{Task: id, StartedAt: started})
}

func (h *Handler) listScheduleRuns(w http.ResponseWriter, r *http.Request) {
	if !h.requireScheduler(w, r) {
		return
	}
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
	status := strings.TrimSpace(query.Get("status"))
	if status != "" && !scheduleRunStatuses[status] {
		h.Error(w, r, apperr.Validation(map[string][]string{"status": {"The status must be running, ok, failed or interrupted."}}))
		return
	}
	runs, err := h.scheduler.History(r.Context(), scheduler.RunFilter{
		Task:     strings.TrimSpace(query.Get("task")),
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
