package dev

import (
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/health"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
)

func TestSummariseHealth(t *testing.T) {
	check := func(name string, status health.Status, advisory bool) health.CheckResult {
		return health.CheckResult{Name: name, Label: name, Status: status, Advisory: advisory}
	}
	cases := []struct {
		name     string
		checks   []health.CheckResult
		want     string
		problems int
	}{
		{"all ok", []health.CheckResult{check("Database", health.StatusOK, false)}, "ok", 0},
		{"warning", []health.CheckResult{check("Debug Mode", health.StatusWarning, false)}, "warning", 1},
		{"advisory failure only warns", []health.CheckResult{check("Backups", health.StatusFailed, true)}, "warning", 1},
		{"critical failure", []health.CheckResult{
			check("Backups", health.StatusFailed, true),
			check("Database", health.StatusCrashed, false),
			check("Cache", health.StatusWarning, false),
		}, "failed", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summariseHealth(health.StoredResults{FinishedAt: time.Now().Unix(), CheckResults: tc.checks})
			if got.Status != tc.want || len(got.Problems) != tc.problems || len(got.Checks) != len(tc.checks) {
				t.Fatalf("summariseHealth() = %s with %d problems of %d, want %s with %d of %d", got.Status, len(got.Problems), len(got.Checks), tc.want, tc.problems, len(tc.checks))
			}
			if tc.problems > 0 && got.Checks[0].Status == string(health.StatusOK) {
				t.Fatalf("checks = %+v, want problems first", got.Checks)
			}
		})
	}

	got := summariseHealth(health.StoredResults{CheckResults: []health.CheckResult{{
		Name: "Migrations", Status: health.StatusWarning, ShortSummary: "pending",
		Meta: map[string]any{"pending_count": 2},
	}}})
	if got.Migrations.Pending != 2 || got.Migrations.Status != "warning" {
		t.Fatalf("migrations = %+v, want 2 pending", got.Migrations)
	}
}

func TestSummariseQueues(t *testing.T) {
	paused := time.Now()
	got := summariseQueues([]queue.AdminQueue{
		{Name: "default", Total: 10, Active: 4, Failed: 1},
		{Name: "heavy", Total: 3, Active: 2, Failed: 0, PausedAt: &paused},
	})
	if !got.Available || got.Queues != 2 || got.Paused != 1 || got.Backlog != 6 || got.Failed != 1 {
		t.Fatalf("summariseQueues() = %+v", got)
	}
}
