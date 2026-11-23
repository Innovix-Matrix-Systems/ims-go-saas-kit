package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
)

func testRunner(t *testing.T, registry *Registry) (*Runner, *memory.Store) {
	t.Helper()
	store := memory.New("sched-")
	t.Cleanup(store.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner, err := NewRunner(registry, logger, store, "UTC")
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner, store
}

func TestRegistryValidation(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(Task{ID: "", Spec: "* * * * *", Run: noopRun}); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := registry.Register(Task{ID: "x", Spec: "not-a-cron", Run: noopRun}); err == nil {
		t.Fatal("invalid spec accepted")
	}
	if err := registry.Register(Task{ID: "x", Spec: "* * * * *", Run: nil}); err == nil {
		t.Fatal("missing handler accepted")
	}
	if err := registry.Register(Task{ID: "x", Spec: "* * * * *", Run: noopRun}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(Task{ID: "x", Spec: "* * * * *", Run: noopRun}); err == nil {
		t.Fatal("duplicate id accepted")
	}
}

func TestRunNowUnknownTask(t *testing.T) {
	runner, _ := testRunner(t, NewRegistry())
	if err := runner.RunNow(context.Background(), "missing"); err == nil {
		t.Fatal("RunNow(unknown) = nil error")
	}
}

func TestOverlapSkip(t *testing.T) {
	registry := NewRegistry()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var runs int32

	if err := registry.Register(Task{
		ID:      "slow",
		Spec:    "* * * * *",
		Overlap: OverlapSkip,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&runs, 1)
			started <- struct{}{}
			<-release
			return nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	runner, _ := testRunner(t, registry)
	go runner.Execute(context.Background(), mustTask(t, registry, "slow"))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run did not start")
	}

	// Second execution while the first is running must be skipped.
	runner.Execute(context.Background(), mustTask(t, registry, "slow"))
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("runs = %d, want 1 (overlap skipped)", got)
	}

	close(release)
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("runs after release = %d, want 1", got)
	}
}

func TestHeartbeatAndHealth(t *testing.T) {
	runner, store := testRunner(t, NewRegistry())
	ctx := context.Background()

	check := HealthCheck{Store: store}
	if result := check.Run(ctx); result.Status != "warning" {
		t.Fatalf("missing heartbeat status = %q, want warning", result.Status)
	}

	runner.Heartbeat(ctx)
	if result := check.Run(ctx); result.Status != "ok" {
		t.Fatalf("fresh heartbeat status = %q, want ok", result.Status)
	}

	// Stale heartbeat.
	_ = store.Set(ctx, HeartbeatKey, time.Now().Add(-10*time.Minute).Unix(), 0)
	if result := check.Run(ctx); result.Status != "warning" {
		t.Fatalf("stale heartbeat status = %q, want warning", result.Status)
	}
}

func TestRunNowPropagatesError(t *testing.T) {
	registry := NewRegistry()
	wantErr := errors.New("task failed")
	if err := registry.Register(Task{
		ID:   "failing",
		Spec: "* * * * *",
		Run: func(ctx context.Context) error {
			return wantErr
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	runner, _ := testRunner(t, registry)
	if err := runner.RunNow(context.Background(), "failing"); !errors.Is(err, wantErr) {
		t.Fatalf("RunNow() error = %v, want %v", err, wantErr)
	}
}

func mustTask(t *testing.T, registry *Registry, id string) Task {
	t.Helper()
	task, ok := registry.Get(id)
	if !ok {
		t.Fatalf("task %q not found", id)
	}
	return task
}

func noopRun(ctx context.Context) error { return nil }

func TestMaintenanceSkipsTasksUnlessOptedIn(t *testing.T) {
	registry := NewRegistry()
	var normal, opted int32
	for _, task := range []Task{
		{ID: "normal", Spec: "* * * * *", Run: func(context.Context) error { atomic.AddInt32(&normal, 1); return nil }},
		{ID: "opted", Spec: "* * * * *", EvenInMaintenance: true, Run: func(context.Context) error { atomic.AddInt32(&opted, 1); return nil }},
	} {
		if err := registry.Register(task); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	runner, _ := testRunner(t, registry)
	down := true
	runner.WithMaintenance(func(context.Context) bool { return down })

	ctx := context.Background()
	runner.Execute(ctx, mustTask(t, registry, "normal"))
	runner.Execute(ctx, mustTask(t, registry, "opted"))
	if normal != 0 || opted != 1 {
		t.Fatalf("while down: normal = %d, opted = %d; want 0, 1", normal, opted)
	}
	// schedule:run is an explicit request: never skipped.
	if err := runner.RunNow(ctx, "normal"); err != nil || normal != 1 {
		t.Fatalf("RunNow while down: err = %v, normal = %d; want nil, 1", err, normal)
	}

	down = false
	runner.Execute(ctx, mustTask(t, registry, "normal"))
	if normal != 2 {
		t.Fatalf("after up: normal = %d, want 2", normal)
	}
}

// memRunLog is an in-memory RunLog for the runner tests.
type memRunLog struct {
	mu   sync.Mutex
	runs []Run
}

func (l *memRunLog) Start(_ context.Context, task, trigger string, at time.Time) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runs = append(l.runs, Run{ID: int64(len(l.runs) + 1), Task: task, Trigger: trigger, Status: RunRunning, StartedAt: at})
	return int64(len(l.runs)), nil
}

func (l *memRunLog) Finish(_ context.Context, id int64, status string, finished time.Time, durationMS float64, runErr string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	run := &l.runs[id-1]
	run.Status, run.FinishedAt, run.DurationMS, run.Error = status, &finished, &durationMS, runErr
	return nil
}

func (l *memRunLog) Recent(_ context.Context, tasks []string, perTask int) (map[string][]Run, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	wanted := map[string]bool{}
	for _, task := range tasks {
		wanted[task] = true
	}
	out := map[string][]Run{}
	for i := len(l.runs) - 1; i >= 0; i-- {
		if run := l.runs[i]; wanted[run.Task] && len(out[run.Task]) < perTask {
			out[run.Task] = append(out[run.Task], run)
		}
	}
	return out, nil
}

func (l *memRunLog) List(context.Context, RunFilter) ([]Run, error) { return l.runs, nil }

func (l *memRunLog) Interrupt(_ context.Context, staleBefore time.Time) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var count int64
	for i := range l.runs {
		lost := l.runs[i].Trigger == TriggerSchedule || l.runs[i].StartedAt.Before(staleBefore)
		if l.runs[i].Status == RunRunning && lost {
			l.runs[i].Status = RunInterrupted
			count++
		}
	}
	return count, nil
}

func (l *memRunLog) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

// TestStatusRecordsRuns: every run leaves its outcome in the run log
// (scheduled or manual, ok or failed), each task reports its next run and
// recent runs, and a run a crashed scheduler left open is closed on start.
func TestStatusRecordsRuns(t *testing.T) {
	registry := NewRegistry()
	boom := errors.New("boom")
	if err := registry.Register(Task{ID: "good", Spec: "0 3 * * *", Description: "Nightly", Run: noopRun}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Task{ID: "bad", Spec: "*/5 * * * *", Run: func(context.Context) error { return boom }}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Task{ID: "beat", Spec: "* * * * *", NoHistory: true, Run: noopRun}); err != nil {
		t.Fatal(err)
	}
	runner, _ := testRunner(t, registry)
	log := &memRunLog{}
	runner.WithRunLog(log)
	ctx := context.Background()

	status, err := runner.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Recording || len(status.Tasks) != 3 || status.Tasks[0].LastRun != nil || status.HeartbeatAt != nil {
		t.Fatalf("before any run = %+v, want three tasks, recording, no runs, no heartbeat", status)
	}
	for _, task := range status.Tasks {
		if task.NextRun == nil || !task.NextRun.After(time.Now()) {
			t.Fatalf("%s next run = %v, want a time ahead", task.ID, task.NextRun)
		}
	}

	if err := runner.RunNow(ctx, "good"); err != nil {
		t.Fatalf("RunNow(good) error = %v", err)
	}
	bad, _ := registry.Get("bad")
	runner.Execute(ctx, bad)
	runner.Execute(ctx, bad)
	if err := runner.RunNow(ctx, "beat"); err != nil {
		t.Fatalf("RunNow(beat) error = %v", err)
	}
	runner.Heartbeat(ctx)

	status, err = runner.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	byID := map[string]TaskStatus{}
	for _, task := range status.Tasks {
		byID[task.ID] = task
	}
	if last := byID["good"].LastRun; last == nil || last.Status != RunOK || last.Trigger != TriggerManual || last.FinishedAt == nil {
		t.Fatalf("good last run = %+v, want a finished manual ok run", last)
	}
	if last := byID["bad"].LastRun; last == nil || last.Status != RunFailed || last.Trigger != TriggerSchedule || last.Error != "boom" {
		t.Fatalf("bad last run = %+v, want a failed scheduled run with its error", last)
	}
	if len(byID["bad"].Recent) != 2 {
		t.Fatalf("bad recent runs = %d, want 2", len(byID["bad"].Recent))
	}
	if beat := byID["beat"]; beat.Recorded || len(beat.Recent) != 0 || len(log.runs) != 3 {
		t.Fatalf("no-history task = %+v (log %d runs), want nothing recorded", beat, len(log.runs))
	}
	if status.HeartbeatAt == nil {
		t.Fatal("heartbeat not reported")
	}

	// A scheduled run still open when the scheduler starts was lost with the
	// previous process.
	if _, err := log.Start(ctx, "good", TriggerSchedule, time.Now()); err != nil {
		t.Fatal(err)
	}
	startCtx, stop := context.WithCancel(ctx)
	stop()
	if err := runner.Start(startCtx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if last := log.runs[len(log.runs)-1]; last.Status != RunInterrupted {
		t.Fatalf("open run after start = %s, want interrupted", last.Status)
	}
}

// TestPanickingTaskFailsTheRun: a panic in a task is a failed run (with
// the panic as its error), never a crash of the process running it.
func TestPanickingTaskFailsTheRun(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Task{ID: "panics", Spec: "* * * * *", Run: func(context.Context) error { panic("boom") }}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	runner, _ := testRunner(t, registry)
	log := &memRunLog{}
	runner.WithRunLog(log)

	err := runner.RunNow(context.Background(), "panics")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("RunNow() error = %v, want the panic", err)
	}
	if got := log.runs[0]; got.Status != RunFailed || !strings.Contains(got.Error, "boom") {
		t.Fatalf("run = %+v, want failed with the panic", got)
	}
}

// TestRunningSeesOpenRuns: a task is running while this process runs it or
// while its newest recorded run is open within the task's timeout (another
// process); a skipped overlap reports ErrTaskRunning.
func TestRunningSeesOpenRuns(t *testing.T) {
	registry := NewRegistry()
	release := make(chan struct{})
	started := make(chan struct{})
	for _, task := range []Task{
		{ID: "slow", Spec: "* * * * *", Run: func(context.Context) error { close(started); <-release; return nil }},
		{ID: "elsewhere", Spec: "* * * * *", Timeout: time.Minute, Run: func(context.Context) error { return nil }},
	} {
		if err := registry.Register(task); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	runner, _ := testRunner(t, registry)
	log := &memRunLog{}
	runner.WithRunLog(log)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- runner.RunNow(ctx, "slow") }()
	<-started
	if running, err := runner.Running(ctx, "slow"); err != nil || !running {
		t.Fatalf("Running(slow) = %v, %v; want true", running, err)
	}
	if err := runner.RunNow(ctx, "slow"); !errors.Is(err, ErrTaskRunning) {
		t.Fatalf("overlapping RunNow() error = %v, want ErrTaskRunning", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("RunNow(slow) error = %v", err)
	}

	// An open run recorded by another process counts until the timeout.
	_, _ = log.Start(ctx, "elsewhere", TriggerSchedule, time.Now())
	if running, _ := runner.Running(ctx, "elsewhere"); !running {
		t.Fatal("Running(elsewhere) = false with a fresh open run")
	}
	log.runs[len(log.runs)-1].StartedAt = time.Now().Add(-2 * time.Hour)
	if running, _ := runner.Running(ctx, "elsewhere"); running {
		t.Fatal("Running(elsewhere) = true with a run open past its timeout")
	}
}
