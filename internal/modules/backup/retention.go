package backup

import (
	"fmt"
	"sort"
	"time"
)

type RetentionPolicy struct {
	KeepAllDays       int
	KeepDailyDays     int
	KeepWeeklyWeeks   int
	KeepMonthlyMonths int
	KeepYearlyYears   int
	MaxTotalMB        int64
}

type RetentionRun struct {
	ID        int64
	CreatedAt time.Time
	SizeBytes int64
}

func SelectForDeletion(runs []RetentionRun, now time.Time, policy RetentionPolicy) []int64 {
	if len(runs) == 0 {
		return []int64{}
	}
	sorted := append([]RetentionRun(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].ID > sorted[j].ID
		}
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})

	newestID := sorted[0].ID
	keep := make(map[int64]bool, len(sorted))
	kept := make([]RetentionRun, 0, len(sorted))

	allCutoff := now.AddDate(0, 0, -policy.KeepAllDays)
	dailyCutoff := now.AddDate(0, 0, -policy.KeepDailyDays)
	weeklyCutoff := now.AddDate(0, 0, -policy.KeepWeeklyWeeks*7)
	monthlyCutoff := now.AddDate(0, -policy.KeepMonthlyMonths, 0)
	yearlyCutoff := now.AddDate(-policy.KeepYearlyYears, 0, 0)

	seenDays := map[string]bool{}
	seenWeeks := map[string]bool{}
	seenMonths := map[string]bool{}
	seenYears := map[string]bool{}

	for _, run := range sorted {
		if run.ID == newestID || !run.CreatedAt.Before(allCutoff) {
			keep[run.ID] = true
			kept = append(kept, run)
			continue
		}
		switch {
		case policy.KeepDailyDays > 0 && !run.CreatedAt.Before(dailyCutoff):
			key := run.CreatedAt.UTC().Format("2006-01-02")
			if !seenDays[key] {
				seenDays[key] = true
				keep[run.ID] = true
				kept = append(kept, run)
			}
		case policy.KeepWeeklyWeeks > 0 && !run.CreatedAt.Before(weeklyCutoff):
			year, week := run.CreatedAt.UTC().ISOWeek()
			key := fmt.Sprintf("%04d-%02d", year, week)
			if !seenWeeks[key] {
				seenWeeks[key] = true
				keep[run.ID] = true
				kept = append(kept, run)
			}
		case policy.KeepMonthlyMonths > 0 && !run.CreatedAt.Before(monthlyCutoff):
			key := run.CreatedAt.UTC().Format("2006-01")
			if !seenMonths[key] {
				seenMonths[key] = true
				keep[run.ID] = true
				kept = append(kept, run)
			}
		case policy.KeepYearlyYears > 0 && !run.CreatedAt.Before(yearlyCutoff):
			key := run.CreatedAt.UTC().Format("2006")
			if !seenYears[key] {
				seenYears[key] = true
				keep[run.ID] = true
				kept = append(kept, run)
			}
		}
	}

	if policy.MaxTotalMB > 0 {
		limit := policy.MaxTotalMB * 1024 * 1024
		var total int64
		for _, run := range kept {
			total += run.SizeBytes
		}
		oldestFirst := append([]RetentionRun(nil), kept...)
		sort.Slice(oldestFirst, func(i, j int) bool {
			if oldestFirst[i].CreatedAt.Equal(oldestFirst[j].CreatedAt) {
				return oldestFirst[i].ID < oldestFirst[j].ID
			}
			return oldestFirst[i].CreatedAt.Before(oldestFirst[j].CreatedAt)
		})
		for _, run := range oldestFirst {
			if total <= limit {
				break
			}
			if run.ID == newestID || !keep[run.ID] {
				continue
			}
			delete(keep, run.ID)
			total -= run.SizeBytes
		}
	}

	deletions := make([]int64, 0)
	for _, run := range sorted {
		if !keep[run.ID] {
			deletions = append(deletions, run.ID)
		}
	}
	return deletions
}
