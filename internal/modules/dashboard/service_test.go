package dashboard

import (
	"context"
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

// revenueRepo serves the revenue rows; the rest of Repository is unused.
type revenueRepo struct {
	Repository
	rows []RevenueDay
}

func (r revenueRepo) Revenue(context.Context, time.Time, string) ([]RevenueDay, error) {
	return r.rows, nil
}

func (revenueRepo) PendingPayments(context.Context) ([]PendingPayments, error) { return nil, nil }

type fixedDefault string

func (f fixedDefault) DefaultCode(context.Context) (string, error) { return string(f), nil }

func TestRevenueLeadsWithTheDefaultCurrency(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	rows := []RevenueDay{
		{Currency: "BDT", Day: "2026-11-24", Amount: 80000, Count: 2},
		{Currency: "EUR", Day: "2026-11-24", Amount: 1000, Count: 1},
		{Currency: "USD", Day: "2026-11-25", Amount: 350000, Count: 3},
	}
	codes := func(out *PlatformRevenue) []string {
		var got []string
		for _, currency := range out.Currencies {
			got = append(got, currency.Currency)
		}
		return got
	}
	s := &Service{repo: revenueRepo{rows: rows}, now: func() time.Time { return now }}
	w := s.NewWindow(30, time.UTC)

	out, err := s.revenue(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	// Without a default: the most payments first.
	if got := codes(out); len(got) != 3 || got[0] != "USD" || got[1] != "BDT" || got[2] != "EUR" {
		t.Fatalf("order = %v, want USD BDT EUR", got)
	}

	// The default currency leads even with fewer payments; no amount is converted.
	s.WithCurrencies(fixedDefault("EUR"))
	out, err = s.revenue(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	if got := codes(out); got[0] != "EUR" || got[1] != "USD" || got[2] != "BDT" {
		t.Fatalf("order = %v, want EUR USD BDT", got)
	}
	if out.Currencies[0].Amount.Total != 1000 || out.Currencies[1].Amount.Total != 350000 {
		t.Fatalf("totals = %d, %d", out.Currencies[0].Amount.Total, out.Currencies[1].Amount.Total)
	}
}
