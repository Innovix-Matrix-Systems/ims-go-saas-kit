package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/tracing"
)

// ErrTaskRunning is returned when a run is skipped because the task is
// still running (OverlapSkip).
var ErrTaskRunning = errors.New("scheduler: the task is still running")

// HeartbeatKey stores the scheduler's last execution timestamp in the cache.
const HeartbeatKey = "schedule:heartbeat"

// Runner executes the registry on a cron schedule with overlap protection,
// per-task timeouts and structured logging.
type Runner struct {
	registry *Registry
	logger   *slog.Logger
	location *time.Location
	store    cache.Store

	// paused reports maintenance mode; nil means never paused.
	paused func(ctx context.Context) bool
	// runs records every run (schedule_runs); nil keeps no history.
	runs RunLog

	mu      sync.Mutex
	running map[string]bool
}

// WithMaintenance makes scheduled runs skip every task not marked
// EvenInMaintenance while down reports true (maintenance mode). RunNow
// (`schedule:run`) is an explicit request and is never skipped.
func (r *Runner) WithMaintenance(down func(ctx context.Context) bool) *Runner {
	r.paused = down
	return r
}

// NewRunner creates a scheduler runner for the given timezone.
func NewRunner(registry *Registry, logger *slog.Logger, store cache.Store, timezone string) (*Runner, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, err
	}
	return &Runner{
		registry: registry,
		logger:   logger,
		location: location,
		store:    store,
		running:  map[string]bool{},
	}, nil
}

// Start runs the scheduler until the context is cancelled, then waits for
// in-flight tasks to finish.
func (r *Runner) Start(ctx context.Context) error {
	runner := cron.New(cron.WithLocation(r.location))
	for _, task := range r.registry.Tasks() {
		task := task
		if _, err := runner.AddFunc(task.Spec, func() { r.Execute(ctx, task) }); err != nil {
			return err
		}
	}

	if r.runs != nil {
		if count, err := r.runs.Interrupt(ctx, time.Now().Add(-staleManualRun)); err != nil {
			r.logger.Warn("could not close runs left open by the last scheduler", "error", err)
		} else if count > 0 {
			r.logger.Warn("closed runs left open by the last scheduler", "runs", count)
		}
	}

	r.logger.Info("scheduler started",
		"tasks", len(r.registry.Tasks()),
		"timezone", r.location.String(),
	)
	runner.Start()
	// Record liveness immediately so health is green before the first tick
	// (the every-minute heartbeat task would otherwise leave a ~60s gap).
	r.Heartbeat(ctx)

	<-ctx.Done()
	stopped := runner.Stop()
	select {
	case <-stopped.Done():
	case <-time.After(30 * time.Second):
		r.logger.Warn("scheduler shutdown timed out waiting for running tasks")
	}
	r.logger.Info("scheduler stopped")
	return nil
}

// RunNow executes a task immediately (`schedule:run`, the dashboard's Run
// now).
func (r *Runner) RunNow(ctx context.Context, id string) error {
	task, ok := r.registry.Get(id)
	if !ok {
		return ErrUnknownTask(id)
	}
	return r.execute(ctx, task, TriggerManual)
}

// Task returns a registered task (ok false when id is unknown).
func (r *Runner) Task(id string) (Task, bool) { return r.registry.Get(id) }

// Execute runs a scheduled firing of task: skipped in maintenance mode
// unless the task opts in, then through the overlap policy (exported for
// tests).
func (r *Runner) Execute(ctx context.Context, task Task) {
	if !task.EvenInMaintenance && r.paused != nil && r.paused(ctx) {
		r.logger.Info("scheduled task skipped (maintenance mode)", "task", task.ID)
		return
	}
	_ = r.execute(ctx, task, TriggerSchedule)
}

// Running reports whether task id is running now: in this process, or (for
// a task with history) as an open run another process recorded.
func (r *Runner) Running(ctx context.Context, id string) (bool, error) {
	r.mu.Lock()
	local := r.running[id]
	r.mu.Unlock()
	if local {
		return true, nil
	}
	task, ok := r.registry.Get(id)
	if !ok || r.runs == nil || task.NoHistory {
		return false, nil
	}
	recent, err := r.runs.Recent(ctx, []string{id}, 1)
	if err != nil {
		return false, err
	}
	runs := recent[id]
	if len(runs) == 0 || runs[0].Status != RunRunning {
		return false, nil
	}
	// A run open past its task's timeout was lost with its process.
	return task.Timeout <= 0 || time.Since(runs[0].StartedAt) < task.Timeout+time.Minute, nil
}

func (r *Runner) execute(ctx context.Context, task Task, trigger string) error {
	if task.Overlap == OverlapSkip {
		r.mu.Lock()
		if r.running[task.ID] {
			r.mu.Unlock()
			r.logger.Warn("scheduled task skipped (still running)", "task", task.ID)
			return ErrTaskRunning
		}
		r.running[task.ID] = true
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			delete(r.running, task.ID)
			r.mu.Unlock()
		}()
	}

	runCtx := ctx
	if task.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, task.Timeout)
		defer cancel()
	}

	start := time.Now()
	runCtx, span := tracing.Start(runCtx, "task "+task.ID)
	defer span.End()
	r.logger.InfoContext(runCtx, "scheduled task started", "task", task.ID)
	runID := r.start(runCtx, task, trigger, start)
	err := r.safeRun(runCtx, task)
	finished := time.Now()
	durationMS := float64(finished.Sub(start).Microseconds()) / 1000.0

	if err != nil {
		r.finish(runCtx, runID, RunFailed, finished, durationMS, err)
		_ = tracing.Fail(runCtx, err)
		r.logger.ErrorContext(runCtx, "scheduled task failed", "task", task.ID, "duration_ms", durationMS, "error", err)
		return err
	}
	r.finish(runCtx, runID, RunOK, finished, durationMS, nil)
	r.logger.InfoContext(runCtx, "scheduled task finished", "task", task.ID, "duration_ms", durationMS)
	return nil
}

// Heartbeat records scheduler liveness for health checks.
func (r *Runner) Heartbeat(ctx context.Context) {
	_ = r.store.Set(ctx, HeartbeatKey, time.Now().Unix(), 0)
}

// safeRun runs the task, turning a panic into a failed run: a panicking
// task must not take the scheduler (or the API, for Run now) down with it.
func (r *Runner) safeRun(ctx context.Context, task Task) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			tracing.RecordPanic(ctx, recovered, stack)
			r.logger.ErrorContext(ctx, "scheduled task panicked", "task", task.ID, "panic", recovered, "stack", string(stack))
			err = fmt.Errorf("task panicked: %v", recovered)
		}
	}()
	return task.Run(ctx)
}
