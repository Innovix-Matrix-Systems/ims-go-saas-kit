package backup

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestSelectForDeletionKeepsWindows(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	run := func(id int64, age time.Duration) RetentionRun {
		return RetentionRun{ID: id, CreatedAt: now.Add(-age), SizeBytes: 100}
	}
	runs := []RetentionRun{
		run(100, time.Hour),                 // newest: always kept
		run(99, 2*24*time.Hour),             // keep-all window
		run(98, 3*24*time.Hour),             // keep-all window
		run(97, 8*24*time.Hour),             // daily window: first of its day
		run(96, 8*24*time.Hour+2*time.Hour), // same day: deleted
		run(95, 32*24*time.Hour),            // weekly window: first of its week
		run(94, 36*24*time.Hour),            // same ISO week: deleted
		run(93, 100*24*time.Hour),           // monthly window: first of its month
		run(92, 110*24*time.Hour),           // same month: deleted
		run(91, 500*24*time.Hour),           // yearly window: first of its year
		run(90, 900*24*time.Hour),           // older than 2 years: deleted
	}
	policy := RetentionPolicy{
		KeepAllDays:       7,
		KeepDailyDays:     16,
		KeepWeeklyWeeks:   8,
		KeepMonthlyMonths: 4,
		KeepYearlyYears:   2,
	}
	deleted := SelectForDeletion(runs, now, policy)
	sort.Slice(deleted, func(i, j int) bool { return deleted[i] < deleted[j] })
	want := []int64{90, 92, 94, 96}
	if !reflect.DeepEqual(deleted, want) {
		t.Fatalf("SelectForDeletion() = %v, want %v", deleted, want)
	}
}

func TestSelectForDeletionNeverDeletesNewest(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	runs := []RetentionRun{
		{ID: 1, CreatedAt: now.Add(-5 * 365 * 24 * time.Hour), SizeBytes: 10},
	}
	deleted := SelectForDeletion(runs, now, RetentionPolicy{})
	if len(deleted) != 0 {
		t.Fatalf("SelectForDeletion() = %v, want none (newest must survive)", deleted)
	}
}

func TestSelectForDeletionMaxTotalSize(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	megabyte := int64(1024 * 1024)
	runs := []RetentionRun{
		{ID: 3, CreatedAt: now.Add(-time.Hour), SizeBytes: 2 * megabyte},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), SizeBytes: 2 * megabyte},
		{ID: 1, CreatedAt: now.Add(-3 * time.Hour), SizeBytes: 2 * megabyte},
	}
	deleted := SelectForDeletion(runs, now, RetentionPolicy{KeepAllDays: 7, MaxTotalMB: 4})
	if !reflect.DeepEqual(deleted, []int64{1}) {
		t.Fatalf("SelectForDeletion(max size) = %v, want [1]", deleted)
	}
}

func TestSelectForDeletionDisabledWindows(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	runs := []RetentionRun{
		{ID: 2, CreatedAt: now.Add(-time.Hour), SizeBytes: 10},
		{ID: 1, CreatedAt: now.Add(-10 * 24 * time.Hour), SizeBytes: 10},
	}
	deleted := SelectForDeletion(runs, now, RetentionPolicy{KeepAllDays: 7})
	if !reflect.DeepEqual(deleted, []int64{1}) {
		t.Fatalf("SelectForDeletion(disabled windows) = %v, want [1]", deleted)
	}
}
