// Package scheduler implements the central scheduled-task registry and its
// cron runner. One scheduler process per deployment; River periodic jobs are
// deliberately not used.
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// OverlapPolicy decides what happens when a task is still running at its next
// fire time.
type OverlapPolicy int

const (
	// OverlapSkip skips the new run (default).
	OverlapSkip OverlapPolicy = iota
	// OverlapAllow runs anyway.
	OverlapAllow
)

// Task is one scheduled task definition.
type Task struct {
	ID          string
	Spec        string
	Description string
	Timeout     time.Duration
	Overlap     OverlapPolicy
	// EvenInMaintenance runs the task while the app is in maintenance mode
	// (Laravel's evenInMaintenanceMode); every other task is skipped then.
	EvenInMaintenance bool
	// NoHistory keeps the task's runs out of the run history (a liveness
	// beat every minute would bury the tasks that do real work).
	NoHistory bool
	Run       func(ctx context.Context) error
}

// Registry holds scheduled tasks; duplicate ids and invalid specs fail fast.
type Registry struct {
	tasks map[string]Task
	order []string
}

// NewRegistry creates an empty task registry.
func NewRegistry() *Registry {
	return &Registry{tasks: map[string]Task{}}
}

// Register adds a scheduled task.
func (r *Registry) Register(task Task) error {
	if task.ID == "" {
		return fmt.Errorf("scheduler: task id must not be empty")
	}
	if _, exists := r.tasks[task.ID]; exists {
		return fmt.Errorf("scheduler: duplicate task id %q", task.ID)
	}
	if task.Run == nil {
		return fmt.Errorf("scheduler: task %q must define a handler", task.ID)
	}
	if _, err := cron.ParseStandard(task.Spec); err != nil {
		return fmt.Errorf("scheduler: task %q: invalid cron spec %q: %w", task.ID, task.Spec, err)
	}
	r.tasks[task.ID] = task
	r.order = append(r.order, task.ID)
	return nil
}

// Tasks returns tasks in registration order.
func (r *Registry) Tasks() []Task {
	out := make([]Task, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.tasks[id])
	}
	return out
}

// Get looks up a task by id.
func (r *Registry) Get(id string) (Task, bool) {
	task, ok := r.tasks[id]
	return task, ok
}
