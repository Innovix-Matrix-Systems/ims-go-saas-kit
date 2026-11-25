package dev

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river/rivertype"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
)

func (h *Handler) requireQueueAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.queueAdmin != nil {
		return true
	}
	h.Error(w, r, apperr.New(apperr.ServiceUnavailable,
		"Queue administration is unavailable (QUEUE_DRIVER=sync)."))
	return false
}

func (h *Handler) respondQueueError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, rivertype.ErrNotFound):
		h.Error(w, r, apperr.New(apperr.ResourceNotFound, "The requested job was not found."))
	case errors.Is(err, rivertype.ErrJobRunning):
		h.Error(w, r, apperr.New(apperr.Conflict, "Running jobs cannot be deleted."))
	case errors.Is(err, queue.ErrInvalidCursor):
		h.Error(w, r, apperr.Validation(map[string][]string{"cursor": {"The cursor is invalid or expired."}}))
	default:
		h.Error(w, r, apperr.Wrap(apperr.InternalServerError, "queue admin", err))
	}
}

func (h *Handler) listQueues(w http.ResponseWriter, r *http.Request) {
	if !h.requireQueueAdmin(w, r) {
		return
	}
	queues, err := h.queueAdmin.ListQueues(r.Context())
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, queues)
}

func (h *Handler) pauseQueue(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	if !h.requireQueueAdmin(w, r) {
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		h.Error(w, r, apperr.New(apperr.InvalidInput, "The queue name is required."))
		return
	}
	started := time.Now()
	err := h.queueAdmin.PauseQueue(r.Context(), name)
	h.audit.Record(r.Context(), actorID, "queue.pause", name, map[string]any{"queue": name}, started, err)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Queue paused.")
}

func (h *Handler) resumeQueue(w http.ResponseWriter, r *http.Request) {
	actorID := authn.UserID(r)
	if !h.requireQueueAdmin(w, r) {
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		h.Error(w, r, apperr.New(apperr.InvalidInput, "The queue name is required."))
		return
	}
	started := time.Now()
	err := h.queueAdmin.ResumeQueue(r.Context(), name)
	h.audit.Record(r.Context(), actorID, "queue.resume", name, map[string]any{"queue": name}, started, err)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Queue resumed.")
}

func (h *Handler) listJobs(w http.ResponseWriter, r *http.Request) {
	if !h.requireQueueAdmin(w, r) {
		return
	}
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	state := strings.ToLower(strings.TrimSpace(query.Get("state")))
	if state != "" && !validAdminState(state) {
		h.Error(w, r, apperr.Validation(map[string][]string{
			"state": {"The selected state is invalid."},
		}))
		return
	}
	result, err := h.queueAdmin.ListJobs(r.Context(), queue.AdminJobListParams{
		Queue:  strings.TrimSpace(query.Get("queue")),
		State:  state,
		Kind:   strings.TrimSpace(query.Get("kind")),
		Limit:  limit,
		Cursor: strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	meta := map[string]any{"next_cursor": result.NextCursor}
	h.Cursor(w, r, http.StatusOK, result.Jobs, meta, nil)
}

func (h *Handler) getJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireQueueAdmin(w, r) {
		return
	}
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	job, err := h.queueAdmin.GetJob(r.Context(), id)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, job)
}

func (h *Handler) retryJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, "queue.job.retry", h.queueAdmin.RetryJob)
}

func (h *Handler) cancelJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, "queue.job.cancel", h.queueAdmin.CancelJob)
}

func (h *Handler) deleteJob(w http.ResponseWriter, r *http.Request) {
	h.mutateJob(w, r, "queue.job.delete", h.queueAdmin.DeleteJob)
}

func (h *Handler) mutateJob(w http.ResponseWriter, r *http.Request, action string, op func(ctx context.Context, id int64) (*queue.AdminJob, error)) {
	actorID := authn.UserID(r)
	if !h.requireQueueAdmin(w, r) {
		return
	}
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	started := time.Now()
	job, err := op(r.Context(), id)
	h.audit.Record(r.Context(), actorID, action, "job:"+strconv.FormatInt(id, 10), map[string]any{"job_id": id}, started, err)
	if err != nil {
		h.respondQueueError(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, job)
}

func validAdminState(state string) bool {
	for _, candidate := range queue.AdminStates {
		if candidate == state {
			return true
		}
	}
	return false
}
