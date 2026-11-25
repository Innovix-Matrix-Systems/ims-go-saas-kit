package scheduler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// NewRunLog returns the run log backed by the schedule_runs table.
func NewRunLog(queries *dbgen.Queries) RunLog { return &sqlRunLog{queries: queries} }

type sqlRunLog struct{ queries *dbgen.Queries }

func (l *sqlRunLog) Start(ctx context.Context, task, trigger string, at time.Time) (int64, error) {
	id, err := l.queries.StartScheduleRun(ctx, dbgen.StartScheduleRunParams{
		Task:        task,
		TriggeredBy: trigger,
		StartedAt:   pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		return 0, database.MapError("schedule_runs.start", err)
	}
	return id, nil
}

func (l *sqlRunLog) Finish(ctx context.Context, id int64, status string, finished time.Time, durationMS float64, runErr string) error {
	var message *string
	if runErr != "" {
		message = &runErr
	}
	err := l.queries.FinishScheduleRun(ctx, dbgen.FinishScheduleRunParams{
		ID:         id,
		Status:     status,
		FinishedAt: pgtype.Timestamptz{Time: finished, Valid: true},
		DurationMs: &durationMS,
		Error:      message,
	})
	if err != nil {
		return database.MapError("schedule_runs.finish", err)
	}
	return nil
}

func (l *sqlRunLog) Recent(ctx context.Context, tasks []string, perTask int) (map[string][]Run, error) {
	rows, err := l.queries.ListRecentScheduleRuns(ctx, dbgen.ListRecentScheduleRunsParams{Tasks: tasks, PerTask: int64(perTask)})
	if err != nil {
		return nil, database.MapError("schedule_runs.recent", err)
	}
	out := map[string][]Run{}
	for _, row := range rows {
		out[row.Task] = append(out[row.Task], mapRun(row))
	}
	return out, nil
}

func (l *sqlRunLog) List(ctx context.Context, filter RunFilter) ([]Run, error) {
	rows, err := l.queries.ListScheduleRuns(ctx, dbgen.ListScheduleRunsParams{
		BeforeID: filter.BeforeID,
		Task:     filter.Task,
		Status:   filter.Status,
		RowLimit: filter.Limit,
	})
	if err != nil {
		return nil, database.MapError("schedule_runs.list", err)
	}
	out := make([]Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapRun(row))
	}
	return out, nil
}

func (l *sqlRunLog) Interrupt(ctx context.Context, staleBefore time.Time) (int64, error) {
	count, err := l.queries.InterruptScheduleRuns(ctx, pgtype.Timestamptz{Time: staleBefore, Valid: true})
	if err != nil {
		return 0, database.MapError("schedule_runs.interrupt", err)
	}
	return count, nil
}

func (l *sqlRunLog) Prune(ctx context.Context, before time.Time) (int64, error) {
	count, err := l.queries.PruneScheduleRuns(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, database.MapError("schedule_runs.prune", err)
	}
	return count, nil
}

func mapRun(row dbgen.ScheduleRun) Run {
	run := Run{
		ID:         row.ID,
		Task:       row.Task,
		Trigger:    row.TriggeredBy,
		Status:     row.Status,
		StartedAt:  row.StartedAt.Time,
		DurationMS: row.DurationMs,
	}
	if row.FinishedAt.Valid {
		finished := row.FinishedAt.Time
		run.FinishedAt = &finished
	}
	if row.Error != nil {
		run.Error = *row.Error
	}
	return run
}
