package dashboard

import (
	"testing"
	"time"
)

func TestWindowSeries(t *testing.T) {
	dhaka, err := time.LoadLocation("Asia/Dhaka")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{now: func() time.Time { return time.Date(2026, 3, 10, 20, 0, 0, 0, time.UTC) }}
	// 20:00 UTC is already 11 March in Dhaka.
	w := s.NewWindow(7, dhaka)
	if got := w.From.Format(time.DateOnly); got != "2026-03-05" {
		t.Fatalf("from = %s", got)
	}
	if got := w.Previous.Format(time.DateOnly); got != "2026-02-26" {
		t.Fatalf("previous = %s", got)
	}
	series := w.series(map[string]int64{"2026-02-27": 4, "2026-03-04": 1, "2026-03-05": 2, "2026-03-11": 3})
	if len(series.Points) != 7 || series.Points[0].Date != "2026-03-05" || series.Points[6].Date != "2026-03-11" {
		t.Fatalf("points = %+v", series.Points)
	}
	if series.Total != 5 || series.PreviousTotal != 5 {
		t.Fatalf("total %d previous %d, want 5 and 5", series.Total, series.PreviousTotal)
	}
	if p := w.period(); p.From != "2026-03-05" || p.To != "2026-03-11" || p.TimeZone != "Asia/Dhaka" {
		t.Fatalf("period = %+v", p)
	}
}
