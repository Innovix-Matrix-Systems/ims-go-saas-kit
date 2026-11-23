package dev

import (
	"net/http"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/mail"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// Playground: local-only helpers for exercising telemetry, synthetic jobs and
// a few harmless real jobs without leaving the dashboard.

type playgroundInfo struct {
	Enabled     bool     `json:"enabled"`
	QueueDriver string   `json:"queue_driver"`
	JobKind     string   `json:"job_kind"`
	Queues      []string `json:"queues"`
	Jobs        []string `json:"jobs"`
}

type playgroundJobRequest struct {
	Kind    string `json:"kind" validate:"required,oneof=dev.test user.export mail.send"`
	Queue   string `json:"queue" validate:"omitempty,oneof=default critical heavy"`
	Count   int    `json:"count" validate:"omitempty,min=1,max=10"`
	SleepMS int    `json:"sleep_ms" validate:"omitempty,min=0,max=10000"`
	Fail    bool   `json:"fail"`
	Tag     string `json:"tag" validate:"omitempty,max=60"`
	To      string `json:"to" validate:"omitempty,email,max=255"`
	Subject string `json:"subject" validate:"omitempty,max=255"`
	Text    string `json:"text" validate:"omitempty,max=2000"`
}

type playgroundJobResult struct {
	Enqueued        int     `json:"enqueued"`
	Kind            string  `json:"kind"`
	Queue           string  `json:"queue"`
	Driver          string  `json:"driver"`
	FailOnPurpose   bool    `json:"fail_on_purpose,omitempty"`
	ProcessingJobID *int64  `json:"processing_job_id,omitempty"`
	JobID           *string `json:"job_id,omitempty"`
}

func (h *Handler) requirePlayground(w http.ResponseWriter, r *http.Request) bool {
	if h.playgroundEnabled {
		return true
	}
	h.Error(w, r, apperr.New(apperr.ResourceNotFound,
		"The developer Playground is only available in local or development environments."))
	return false
}

func (h *Handler) playgroundInfoHandler(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlayground(w, r) {
		return
	}
	driver := ""
	if h.queue != nil {
		driver = h.queue.Kind()
	}
	h.Data(w, r, http.StatusOK, playgroundInfo{
		Enabled:     true,
		QueueDriver: driver,
		JobKind:     PlaygroundJobKind,
		Queues:      []string{string(queue.QueueDefault), string(queue.QueueCritical), string(queue.QueueHeavy)},
		Jobs:        []string{PlaygroundJobKind, identity.KindUserExport, mail.KindSend},
	})
}

func (h *Handler) enqueuePlaygroundJobs(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlayground(w, r) {
		return
	}
	actorID := authn.UserID(r)
	if h.queue == nil {
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "The queue engine is unavailable."))
		return
	}

	var request playgroundJobRequest
	if !h.Bind(w, r, &request) {
		return
	}

	started := time.Now()
	target := "playground:" + request.Kind
	var result playgroundJobResult
	var opErr error
	switch request.Kind {
	case PlaygroundJobKind:
		result, target, opErr = h.enqueueSyntheticJobs(r, request)
	case identity.KindUserExport:
		result, target, opErr = h.enqueueUserExport(r, actorID)
	case mail.KindSend:
		result, target, opErr = h.enqueueTestMail(r, request)
	default:
		opErr = apperr.Validation(map[string][]string{
			"kind": {"The selected job kind is not supported."},
		})
	}

	params := map[string]any{
		"kind":      request.Kind,
		"queue":     request.Queue,
		"count":     request.Count,
		"enqueued":  result.Enqueued,
		"driver":    h.queue.Kind(),
		"transport": "playground",
	}
	if request.Kind == PlaygroundJobKind {
		params["sleep_ms"] = request.SleepMS
		params["fail"] = request.Fail
		params["tag"] = request.Tag
	}
	if request.Kind == mail.KindSend {
		params["to"] = request.To
		params["subject"] = request.Subject
	}
	h.audit.Record(r.Context(), actorID, "playground.jobs", target, params, started, opErr)
	if opErr != nil {
		h.Error(w, r, opErr)
		return
	}
	result.Driver = h.queue.Kind()
	h.Data(w, r, http.StatusAccepted, result)
}

func (h *Handler) enqueueSyntheticJobs(r *http.Request, request playgroundJobRequest) (playgroundJobResult, string, error) {
	if request.Queue == "" {
		request.Queue = string(queue.QueueDefault)
	}
	if request.Count == 0 {
		request.Count = 1
	}
	if request.Tag == "" {
		request.Tag = "playground"
	}
	name := queue.Name(request.Queue)
	options := queue.EnqueueOptions{Queue: &name}
	enqueued := 0
	for index := 0; index < request.Count; index++ {
		payload := PlaygroundJobPayload{
			SleepMS: request.SleepMS,
			Fail:    request.Fail,
			Tag:     request.Tag,
		}
		if err := h.queue.Enqueue(r.Context(), PlaygroundJobKind, payload, options); err != nil {
			return playgroundJobResult{
				Enqueued:      enqueued,
				Kind:          PlaygroundJobKind,
				Queue:         request.Queue,
				FailOnPurpose: request.Fail,
			}, "queue:" + request.Queue, err
		}
		enqueued++
	}
	return playgroundJobResult{
		Enqueued:      enqueued,
		Kind:          PlaygroundJobKind,
		Queue:         request.Queue,
		FailOnPurpose: request.Fail,
	}, "queue:" + request.Queue, nil
}

func (h *Handler) enqueueUserExport(r *http.Request, actorID int64) (playgroundJobResult, string, error) {
	if h.exchange == nil {
		return playgroundJobResult{}, "job:" + identity.KindUserExport,
			apperr.New(apperr.ServiceUnavailable, "User exports are not available.")
	}
	job, err := h.exchange.StartExport(r.Context(), tenancy.From(r), actorID, identity.ExportRequest{})
	if err != nil {
		return playgroundJobResult{}, "job:" + identity.KindUserExport, err
	}
	result := playgroundJobResult{
		Enqueued:        1,
		Kind:            identity.KindUserExport,
		Queue:           string(queue.QueueHeavy),
		ProcessingJobID: &job.ID,
		JobID:           job.QueueJobID,
	}
	return result, formatTarget("processing", job.ID), nil
}

func (h *Handler) enqueueTestMail(r *http.Request, request playgroundJobRequest) (playgroundJobResult, string, error) {
	fields := map[string][]string{}
	if strings.TrimSpace(request.To) == "" {
		fields["to"] = append(fields["to"], "The to field is required for mail.send.")
	}
	if strings.TrimSpace(request.Subject) == "" {
		fields["subject"] = append(fields["subject"], "The subject field is required for mail.send.")
	}
	if len(fields) > 0 {
		return playgroundJobResult{}, "queue:" + string(queue.QueueDefault),
			apperr.Validation(fields)
	}
	if err := mail.Enqueue(r.Context(), h.queue, mail.Message{
		To:      request.To,
		Subject: request.Subject,
		Text:    request.Text,
	}); err != nil {
		return playgroundJobResult{}, "queue:" + string(queue.QueueDefault), err
	}
	return playgroundJobResult{
		Enqueued: 1,
		Kind:     mail.KindSend,
		Queue:    string(queue.QueueDefault),
	}, "queue:" + string(queue.QueueDefault), nil
}
