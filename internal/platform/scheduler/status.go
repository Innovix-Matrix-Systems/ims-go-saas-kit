package scheduler

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
)

// Every run of a task (scheduled or run by hand) is recorded in the run log
// (schedule_runs), so the dashboard can show what each task did: the
// scheduler process writes, any process reads.

// Run statuses.
const (
	RunRunning     = "running"
	RunOK          = "ok"
	RunFailed      = "failed"
	RunInterrupted = "interrupted"
)

// Run triggers.
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// recentPerTask is how many runs each task shows in the Status summary.
const recentPerTask = 10

// staleManualRun is how long a manual run may stay open before the
// scheduler's start closes it as interrupted (no task runs this long).
const staleManualRun = 24 * time.Hour

// Run is one recorded run of a task.
type Run struct {
	ID         int64      `json:"id"`
	Task       string     `json:"task"`
	Trigger    string     `json:"trigger"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMS *float64   `json:"duration_ms,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// RunFilter narrows the run history (newest first, keyset by id).
type RunFilter struct {
	Task     string
	Status   string
	BeforeID int64
	Limit    int32
}

// RunLog stores runs. NewRunLog is the schedule_runs implementation.
type RunLog interface {
	Start(ctx context.Context, task, trigger string, at time.Time) (int64, error)
	Finish(ctx context.Context, id int64, status string, finished time.Time, durationMS float64, runErr string) error
	// Recent returns the newest perTask runs of each of tasks, newest first.
	Recent(ctx context.Context, tasks []string, perTask int) (map[string][]Run, error)
	List(ctx context.Context, filter RunFilter) ([]Run, error)
	// Interrupt closes scheduled runs left 'running' by a scheduler that died,
	// and manual runs still open since before staleBefore.
	Interrupt(ctx context.Context, staleBefore time.Time) (int64, error)
	Prune(ctx context.Context, before time.Time) (int64, error)
}

// TaskStatus is a registered task with its next run and recent runs.
type TaskStatus struct {
	ID                string `json:"id"`
	Spec              string `json:"spec"`
	Description       string `json:"description"`
	TimeoutSeconds    int64  `json:"timeout_seconds"`
	SkipsOverlap      bool   `json:"skips_overlap"`
	EvenInMaintenance bool   `json:"even_in_maintenance"`
	// Recorded is false for a task that keeps no history (NoHistory).
	Recorded bool       `json:"recorded"`
	NextRun  *time.Time `json:"next_run,omitempty"`
	LastRun  *Run       `json:"last_run,omitempty"`
	// Recent is up to the last ten runs, newest first.
	Recent []Run `json:"recent"`
}

// Status is the scheduler as the dashboard shows it.
type Status struct {
	Timezone string `json:"timezone"`
	// HeartbeatAt is when the `ims schedule` process last checked in (nil:
	// it has never run against this cache).
	HeartbeatAt *time.Time `json:"heartbeat_at,omitempty"`
	// Recording is false when no run log is configured (runs are not kept).
	Recording bool         `json:"recording"`
	Tasks     []TaskStatus `json:"tasks"`
}

// WithRunLog records every run in log.
func (r *Runner) WithRunLog(log RunLog) *Runner {
	r.runs = log
	return r
}

// Status reports every task with its next run (in the scheduler's
// timezone) and its recent runs.
func (r *Runner) Status(ctx context.Context) (Status, error) {
	now := time.Now().In(r.location)
	out := Status{Timezone: r.location.String(), Recording: r.runs != nil, Tasks: []TaskStatus{}}

	var heartbeat int64
	found, err := r.store.Get(ctx, HeartbeatKey, &heartbeat)
	if err != nil {
		return Status{}, err
	}
	if found {
		at := time.Unix(heartbeat, 0)
		out.HeartbeatAt = &at
	}

	tasks := r.registry.Tasks()
	recent := map[string][]Run{}
	if r.runs != nil {
		ids := make([]string, 0, len(tasks))
		for _, task := range tasks {
			if !task.NoHistory {
				ids = append(ids, task.ID)
			}
		}
		if recent, err = r.runs.Recent(ctx, ids, recentPerTask); err != nil {
			return Status{}, err
		}
	}
	for _, task := range tasks {
		status := TaskStatus{
			ID:                task.ID,
			Spec:              task.Spec,
			Description:       task.Description,
			TimeoutSeconds:    int64(task.Timeout / time.Second),
			SkipsOverlap:      task.Overlap == OverlapSkip,
			EvenInMaintenance: task.EvenInMaintenance,
			Recorded:          r.runs != nil && !task.NoHistory,
			Recent:            recent[task.ID],
		}
		if status.Recent == nil {
			status.Recent = []Run{}
		}
		if len(status.Recent) > 0 {
			status.LastRun = &status.Recent[0]
		}
		if schedule, err := cron.ParseStandard(task.Spec); err == nil {
			next := schedule.Next(now)
			status.NextRun = &next
		}
		out.Tasks = append(out.Tasks, status)
	}
	return out, nil
}

// History lists recorded runs, newest first.
func (r *Runner) History(ctx context.Context, filter RunFilter) ([]Run, error) {
	if r.runs == nil {
		return []Run{}, nil
	}
	return r.runs.List(ctx, filter)
}

// PruneHistory deletes runs started more than days ago.
func (r *Runner) PruneHistory(ctx context.Context, days int) (int64, error) {
	if r.runs == nil || days <= 0 {
		return 0, nil
	}
	return r.runs.Prune(ctx, time.Now().AddDate(0, 0, -days))
}

// start records a run beginning; a failed write never stops the task.
func (r *Runner) start(ctx context.Context, task Task, trigger string, at time.Time) int64 {
	if r.runs == nil || task.NoHistory {
		return 0
	}
	id, err := r.runs.Start(context.WithoutCancel(ctx), task.ID, trigger, at)
	if err != nil {
		r.logger.WarnContext(ctx, "scheduled task run not recorded", "task", task.ID, "error", err)
		return 0
	}
	return id
}

// finish records how a run ended.
func (r *Runner) finish(ctx context.Context, id int64, status string, finished time.Time, durationMS float64, runErr error) {
	if r.runs == nil || id == 0 {
		return
	}
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	if err := r.runs.Finish(context.WithoutCancel(ctx), id, status, finished, durationMS, message); err != nil {
		r.logger.WarnContext(ctx, "scheduled task run not recorded", "run", id, "error", err)
	}
}
