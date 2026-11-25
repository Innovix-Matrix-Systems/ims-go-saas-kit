package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// ErrInvalidCursor reports an opaque River job-list cursor that could not be
// decoded (stale, truncated or tampered with).
var ErrInvalidCursor = errors.New("invalid job cursor")

// AdminJobError is one failed attempt of a River job.
type AdminJobError struct {
	At      time.Time `json:"at"`
	Attempt int       `json:"attempt"`
	Error   string    `json:"error"`
}

// AdminJob is the dashboard representation of a River job. Kind is the logical
// application kind decoded from the ims.job envelope; RiverKind is the constant
// River kind.
type AdminJob struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	RiverKind   string          `json:"river_kind"`
	Queue       string          `json:"queue"`
	State       string          `json:"state"`
	Priority    int             `json:"priority"`
	Attempt     int             `json:"attempt"`
	MaxAttempts int             `json:"max_attempts"`
	Args        json.RawMessage `json:"args"`
	Errors      []AdminJobError `json:"errors"`
	Tags        []string        `json:"tags"`
	CreatedAt   time.Time       `json:"created_at"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	AttemptedAt *time.Time      `json:"attempted_at"`
	FinalizedAt *time.Time      `json:"finalized_at"`
}

// AdminJobListParams filters a cursor-paginated job list.
type AdminJobListParams struct {
	Queue  string
	State  string
	Kind   string
	Limit  int
	Cursor string
}

// AdminJobListResult is one page of jobs plus the opaque next cursor.
type AdminJobListResult struct {
	Jobs       []AdminJob
	NextCursor string
}

// AdminQueue is a queue with its current depth counters.
type AdminQueue struct {
	Name     string     `json:"name"`
	PausedAt *time.Time `json:"paused_at"`
	Total    int64      `json:"total"`
	Active   int64      `json:"active"`
	Failed   int64      `json:"failed"`
}

// AdminStates is the set of job states accepted by the queue admin surface.
var AdminStates = []string{
	string(rivertype.JobStateAvailable),
	string(rivertype.JobStateCancelled),
	string(rivertype.JobStateCompleted),
	string(rivertype.JobStateDiscarded),
	string(rivertype.JobStatePending),
	string(rivertype.JobStateRetryable),
	string(rivertype.JobStateRunning),
	string(rivertype.JobStateScheduled),
}

// Admin is the queue management surface used by the developer dashboard.
type Admin interface {
	ListJobs(ctx context.Context, params AdminJobListParams) (*AdminJobListResult, error)
	GetJob(ctx context.Context, id int64) (*AdminJob, error)
	RetryJob(ctx context.Context, id int64) (*AdminJob, error)
	CancelJob(ctx context.Context, id int64) (*AdminJob, error)
	DeleteJob(ctx context.Context, id int64) (*AdminJob, error)
	ListQueues(ctx context.Context) ([]AdminQueue, error)
	PauseQueue(ctx context.Context, name string) error
	ResumeQueue(ctx context.Context, name string) error
}

// Admin exposes the queue management surface. It returns nil for engines that
// cannot manage jobs (the sync driver).
func (e *RiverEngine) Admin() Admin {
	if e == nil || e.client == nil {
		return nil
	}
	return &riverAdmin{engine: e}
}

type riverAdmin struct {
	engine *RiverEngine
}

const maxAdminJobErrors = 10

func (a *riverAdmin) ListJobs(ctx context.Context, params AdminJobListParams) (*AdminJobListResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	list := river.NewJobListParams().
		First(limit).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc)
	if params.Queue != "" {
		list = list.Queues(params.Queue)
	}
	if params.State != "" {
		list = list.States(rivertype.JobState(params.State))
	}
	if params.Kind != "" {
		list = list.Where("args->>'kind' = @kind", river.NamedArgs{"kind": params.Kind})
	}
	if params.Cursor != "" {
		var cursor river.JobListCursor
		if err := cursor.UnmarshalText([]byte(params.Cursor)); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
		list = list.After(&cursor)
	}
	result, err := a.engine.client.JobList(ctx, list)
	if err != nil {
		return nil, err
	}
	out := &AdminJobListResult{Jobs: make([]AdminJob, 0, len(result.Jobs))}
	for _, job := range result.Jobs {
		out.Jobs = append(out.Jobs, mapAdminJob(job))
	}
	if result.LastCursor != nil && len(result.Jobs) == limit {
		encoded, err := result.LastCursor.MarshalText()
		if err != nil {
			return nil, err
		}
		out.NextCursor = string(encoded)
	}
	return out, nil
}

func (a *riverAdmin) GetJob(ctx context.Context, id int64) (*AdminJob, error) {
	job, err := a.engine.client.JobGet(ctx, id)
	if err != nil {
		return nil, err
	}
	mapped := mapAdminJob(job)
	return &mapped, nil
}

func (a *riverAdmin) RetryJob(ctx context.Context, id int64) (_ *AdminJob, err error) {
	ctx, end := tracing.Begin(ctx, "queue.retry_job", attribute.Int64("job.id", id))
	defer end(&err)
	job, err := a.engine.client.JobRetry(ctx, id)
	if err != nil {
		return nil, err
	}
	mapped := mapAdminJob(job)
	return &mapped, nil
}

func (a *riverAdmin) CancelJob(ctx context.Context, id int64) (_ *AdminJob, err error) {
	ctx, end := tracing.Begin(ctx, "queue.cancel_job", attribute.Int64("job.id", id))
	defer end(&err)
	job, err := a.engine.client.JobCancel(ctx, id)
	if err != nil {
		return nil, err
	}
	mapped := mapAdminJob(job)
	return &mapped, nil
}

func (a *riverAdmin) DeleteJob(ctx context.Context, id int64) (_ *AdminJob, err error) {
	ctx, end := tracing.Begin(ctx, "queue.delete_job", attribute.Int64("job.id", id))
	defer end(&err)
	job, err := a.engine.client.JobDelete(ctx, id)
	if err != nil {
		return nil, err
	}
	mapped := mapAdminJob(job)
	return &mapped, nil
}

func (a *riverAdmin) ListQueues(ctx context.Context) ([]AdminQueue, error) {
	result, err := a.engine.client.QueueList(ctx, river.NewQueueListParams().First(100))
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*AdminQueue, len(result.Queues))
	for _, item := range result.Queues {
		byName[item.Name] = &AdminQueue{Name: item.Name, PausedAt: item.PausedAt}
	}
	stats, err := Stats(ctx, a.engine.pool)
	if err != nil {
		return nil, err
	}
	for _, stat := range stats {
		queue, ok := byName[stat.Queue]
		if !ok {
			queue = &AdminQueue{Name: stat.Queue}
			byName[stat.Queue] = queue
		}
		queue.Total = stat.Total
		queue.Active = stat.Active
		queue.Failed = stat.Failed
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	queues := make([]AdminQueue, 0, len(names))
	for _, name := range names {
		queues = append(queues, *byName[name])
	}
	return queues, nil
}

func (a *riverAdmin) PauseQueue(ctx context.Context, name string) (err error) {
	ctx, end := tracing.Begin(ctx, "queue.pause", attribute.String("queue.name", name))
	defer end(&err)
	return a.engine.client.QueuePause(ctx, name, nil)
}

func (a *riverAdmin) ResumeQueue(ctx context.Context, name string) (err error) {
	ctx, end := tracing.Begin(ctx, "queue.resume", attribute.String("queue.name", name))
	defer end(&err)
	return a.engine.client.QueueResume(ctx, name, nil)
}

func mapAdminJob(job *rivertype.JobRow) AdminJob {
	kind := job.Kind
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(job.EncodedArgs, &envelope); err == nil && envelope.Kind != "" {
		kind = envelope.Kind
	}
	args := json.RawMessage(job.EncodedArgs)
	if len(args) == 0 {
		args = json.RawMessage("null")
	}
	if strings.HasPrefix(kind, "mail.") || strings.HasPrefix(kind, "delivery.") {
		// Mail and delivery jobs carry recipients, message bodies and
		// template data such as one-time codes (customer data): the
		// dashboard shows the envelope, never the payload (H14).
		var envelope jobArgs
		if err := json.Unmarshal(job.EncodedArgs, &envelope); err == nil {
			envelope.Payload = json.RawMessage(`"[redacted]"`)
			if redacted, err := json.Marshal(envelope); err == nil {
				args = redacted
			}
		}
	}
	errors := job.Errors
	if len(errors) > maxAdminJobErrors {
		errors = errors[len(errors)-maxAdminJobErrors:]
	}
	attemptErrors := make([]AdminJobError, 0, len(errors))
	for _, item := range errors {
		attemptErrors = append(attemptErrors, AdminJobError{
			At:      item.At,
			Attempt: item.Attempt,
			Error:   item.Error,
		})
	}
	return AdminJob{
		ID:          job.ID,
		Kind:        kind,
		RiverKind:   job.Kind,
		Queue:       job.Queue,
		State:       string(job.State),
		Priority:    job.Priority,
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
		Args:        args,
		Errors:      attemptErrors,
		Tags:        job.Tags,
		CreatedAt:   job.CreatedAt,
		ScheduledAt: job.ScheduledAt,
		AttemptedAt: job.AttemptedAt,
		FinalizedAt: job.FinalizedAt,
	}
}
