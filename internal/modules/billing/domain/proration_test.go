package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

var (
	basic      = Package{ID: 1, Name: "Basic", Price: 1000, BillingCycle: CycleMonthly}
	pro        = Package{ID: 2, Name: "Pro", Price: 3000, BillingCycle: CycleMonthly}
	proYearly  = Package{ID: 3, Name: "Pro yearly", Price: 30000, BillingCycle: CycleYearly}
	free       = Package{ID: 4, Name: "Free", Price: 0, BillingCycle: CycleMonthly}
	basicYear  = Package{ID: 5, Name: "Basic yearly", Price: 12000, BillingCycle: CycleYearly}
	lifetime   = Package{ID: 6, Name: "Lifetime", Price: 50000, BillingCycle: CycleOneTime}
	team       = Package{ID: 7, Name: "Team", Price: 3000, BillingCycle: CycleMonthly}
	periodFrom = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
)

func day(n int) time.Time { return periodFrom.AddDate(0, 0, n-1) }

func sub(id int64, pkg Package, status string, start time.Time) *Subscription {
	end := PeriodEnd(pkg.BillingCycle, start)
	return &Subscription{ID: id, TenantID: 9, PackageID: pkg.ID, Status: status, StartsAt: &start, ExpiresAt: &end}
}

func mustQuote(t *testing.T, in QuoteInput) Quote {
	t.Helper()
	quote, err := QuoteChange(in)
	if err != nil {
		t.Fatalf("QuoteChange: %v", err)
	}
	return quote
}

// linesTotal checks the invoice lines add up to what is paid now.
func linesTotal(t *testing.T, quote Quote) {
	t.Helper()
	if !quote.RequiresPayment {
		return
	}
	var total int64
	for _, line := range quote.Lines {
		total += line.Amount
	}
	if total+quote.Surplus != quote.AmountDue {
		t.Fatalf("lines add up to %d (+%d surplus), amount due is %d: %+v", total, quote.Surplus, quote.AmountDue, quote.Lines)
	}
}

func TestQuoteNewPlanWithoutCurrent(t *testing.T) {
	now := day(5)
	quote := mustQuote(t, QuoteInput{Now: now, Target: pro, Balance: 200})
	if quote.Kind != ChangeNew || quote.Effective != EffectiveNow || !quote.RequiresPayment {
		t.Fatalf("kind %s effective %s", quote.Kind, quote.Effective)
	}
	if quote.BalanceApplied != 200 || quote.AmountDue != 2800 {
		t.Fatalf("balance %d due %d, want 200 and 2800", quote.BalanceApplied, quote.AmountDue)
	}
	if !quote.PeriodStart.Equal(now) || !quote.PeriodEnd.Equal(now.AddDate(0, 1, 0)) {
		t.Fatalf("period %v – %v", quote.PeriodStart, quote.PeriodEnd)
	}
	linesTotal(t, quote)
}

func TestQuoteUpgradeCreditsUnusedTime(t *testing.T) {
	current := sub(10, basic, StatusActive, periodFrom) // 1 Nov – 1 Dec: 30 days
	quote := mustQuote(t, QuoteInput{Now: day(16), Current: current, CurrentPackage: &basic, CurrentPaid: 1000, Target: pro})
	if quote.Kind != ChangeUpgrade || quote.Effective != EffectiveNow {
		t.Fatalf("kind %s effective %s", quote.Kind, quote.Effective)
	}
	if quote.PeriodDays != 30 || quote.UnusedDays != 15 || quote.Credit != 500 || quote.AmountDue != 2500 {
		t.Fatalf("period %d unused %d credit %d due %d, want 30/15/500/2500",
			quote.PeriodDays, quote.UnusedDays, quote.Credit, quote.AmountDue)
	}
	if quote.ReplacesSubscriptionID == nil || *quote.ReplacesSubscriptionID != 10 {
		t.Fatalf("replaces %v", quote.ReplacesSubscriptionID)
	}
	quote.NameUnusedTime(basic)
	if len(quote.Lines) != 2 || quote.Lines[1].Type != LineUnusedTime || quote.Lines[1].PackageName != "Basic" || quote.Lines[1].Days != 15 {
		t.Fatalf("lines %+v", quote.Lines)
	}
	linesTotal(t, quote)
}

func TestQuoteUpgradeUsesWhatWasPaidNotTheListPrice(t *testing.T) {
	current := sub(10, basic, StatusActive, periodFrom)
	// A deal: Basic for 600 instead of 1000.
	quote := mustQuote(t, QuoteInput{Now: day(16), Current: current, CurrentPackage: &basic, CurrentPaid: 600, Target: pro})
	if quote.Credit != 300 || quote.AmountDue != 2700 {
		t.Fatalf("credit %d due %d, want 300 and 2700", quote.Credit, quote.AmountDue)
	}
}

func TestQuoteUpgradeAppliesAccountCreditAfterTheUnusedTime(t *testing.T) {
	current := sub(10, basic, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(16), Current: current, CurrentPackage: &basic, CurrentPaid: 1000, Target: pro, Balance: 300})
	if quote.Credit != 500 || quote.BalanceApplied != 300 || quote.AmountDue != 2200 {
		t.Fatalf("credit %d balance %d due %d", quote.Credit, quote.BalanceApplied, quote.AmountDue)
	}
	if len(quote.Lines) != 3 || quote.Lines[2].Type != LineAccountCredit || quote.Lines[2].Amount != -300 {
		t.Fatalf("lines %+v", quote.Lines)
	}
	linesTotal(t, quote)
}

func TestQuoteBalanceLargerThanTheAmountDue(t *testing.T) {
	quote := mustQuote(t, QuoteInput{Now: day(1), Target: basic, Balance: 5000})
	if quote.BalanceApplied != 1000 || quote.AmountDue != 0 || !quote.RequiresPayment {
		t.Fatalf("balance %d due %d requires %v", quote.BalanceApplied, quote.AmountDue, quote.RequiresPayment)
	}
	linesTotal(t, quote)
}

func TestQuoteMonthlyToYearly(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(11), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Target: proYearly})
	if quote.Kind != ChangeUpgrade || quote.Credit != 2000 || quote.AmountDue != 28000 {
		t.Fatalf("kind %s credit %d due %d, want upgrade/2000/28000", quote.Kind, quote.Credit, quote.AmountDue)
	}
	if !quote.PeriodEnd.Equal(day(11).AddDate(1, 0, 0)) {
		t.Fatalf("period end %v", quote.PeriodEnd)
	}
}

func TestQuoteCreditLargerThanTheNewPriceBecomesSurplus(t *testing.T) {
	current := sub(10, basicYear, StatusActive, periodFrom) // a year of Basic, 12000
	quote := mustQuote(t, QuoteInput{Now: day(31), Current: current, CurrentPackage: &basicYear, CurrentPaid: 12000, Target: pro, Balance: 100})
	if quote.Kind != ChangeUpgrade {
		t.Fatalf("kind %s, want upgrade (Pro costs more per day)", quote.Kind)
	}
	// 365-day period, 335 days left: 12000 × 335 / 365 = 11013.7 → 11014.
	if quote.UnusedDays != 335 || quote.Credit != 11014 {
		t.Fatalf("unused %d credit %d", quote.UnusedDays, quote.Credit)
	}
	if quote.AmountDue != 0 || quote.Surplus != 8014 || quote.BalanceApplied != 0 {
		t.Fatalf("due %d surplus %d balance %d, want 0/8014/0", quote.AmountDue, quote.Surplus, quote.BalanceApplied)
	}
	linesTotal(t, quote)
}

func TestQuoteDowngradeWaitsForThePeriodEnd(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(5), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Target: basic, Balance: 999})
	if quote.Kind != ChangeDowngrade || quote.Effective != EffectivePeriodEnd || quote.RequiresPayment {
		t.Fatalf("kind %s effective %s requires %v", quote.Kind, quote.Effective, quote.RequiresPayment)
	}
	if quote.AmountDue != 0 || quote.Credit != 0 || quote.BalanceApplied != 0 || quote.DueAtRenewal != 1000 {
		t.Fatalf("due %d credit %d balance %d at renewal %d", quote.AmountDue, quote.Credit, quote.BalanceApplied, quote.DueAtRenewal)
	}
	if !quote.PeriodStart.Equal(*current.ExpiresAt) {
		t.Fatalf("starts %v, want %v", quote.PeriodStart, *current.ExpiresAt)
	}
}

func TestQuoteSwitchToFreeWaitsForThePeriodEnd(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(5), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Target: free})
	if quote.Kind != ChangeDowngrade || quote.Effective != EffectivePeriodEnd || quote.DueAtRenewal != 0 {
		t.Fatalf("kind %s effective %s at renewal %d", quote.Kind, quote.Effective, quote.DueAtRenewal)
	}
}

func TestQuoteYearlyToMonthlyCostsMorePerDay(t *testing.T) {
	current := sub(10, proYearly, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(40), Current: current, CurrentPackage: &proYearly, CurrentPaid: 30000, Target: pro})
	// 30000/365 = 82/day; 3000/30 = 100/day: monthly costs more per day.
	if quote.Kind != ChangeUpgrade {
		t.Fatalf("kind %s", quote.Kind)
	}
	if quote.Surplus == 0 || quote.AmountDue != 0 {
		t.Fatalf("surplus %d due %d", quote.Surplus, quote.AmountDue)
	}
}

func TestQuoteSamePricedOtherPlanIsImmediate(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(16), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Target: team})
	if quote.Kind != ChangeUpgrade || quote.Credit != 1500 || quote.AmountDue != 1500 {
		t.Fatalf("kind %s credit %d due %d", quote.Kind, quote.Credit, quote.AmountDue)
	}
}

func TestQuoteRenewalStacksAfterThePeriod(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(25), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Target: pro, Balance: 500})
	if quote.Kind != ChangeRenew || quote.Effective != EffectivePeriodEnd || !quote.RequiresPayment {
		t.Fatalf("kind %s effective %s requires %v", quote.Kind, quote.Effective, quote.RequiresPayment)
	}
	if quote.Credit != 0 || quote.BalanceApplied != 500 || quote.AmountDue != 2500 {
		t.Fatalf("credit %d balance %d due %d", quote.Credit, quote.BalanceApplied, quote.AmountDue)
	}
	if !quote.PeriodStart.Equal(*current.ExpiresAt) || !quote.PeriodEnd.Equal(current.ExpiresAt.AddDate(0, 1, 0)) {
		t.Fatalf("period %v – %v", quote.PeriodStart, quote.PeriodEnd)
	}
	linesTotal(t, quote)
}

func TestQuoteRenewalOntoTheChosenDowngrade(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	next := basic.ID
	current.NextPackageID = &next
	quote := mustQuote(t, QuoteInput{Now: day(25), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Target: basic})
	if quote.Kind != ChangeRenew || quote.AmountDue != 1000 || !quote.PeriodStart.Equal(*current.ExpiresAt) {
		t.Fatalf("kind %s due %d start %v", quote.Kind, quote.AmountDue, quote.PeriodStart)
	}
}

func TestQuoteRefusesASecondArrangedPeriod(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	scheduled := sub(11, pro, StatusScheduled, *current.ExpiresAt)
	for _, target := range []Package{pro, basic} {
		_, err := QuoteChange(QuoteInput{Now: day(25), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Scheduled: scheduled, Target: target})
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Code != apperr.InvalidState {
			t.Fatalf("%s: err %v, want INVALID_STATE", target.Name, err)
		}
	}
	// An upgrade is still possible (the arranged period is withdrawn).
	if _, err := QuoteChange(QuoteInput{Now: day(25), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Scheduled: scheduled, Target: proYearly}); err != nil {
		t.Fatalf("upgrade with a scheduled period: %v", err)
	}
}

func TestQuoteTrialOfTheSamePlanStartsWhenTheTrialEnds(t *testing.T) {
	trial := sub(10, pro, StatusTrial, periodFrom)
	ends := periodFrom.AddDate(0, 0, 14)
	trial.ExpiresAt = &ends
	quote := mustQuote(t, QuoteInput{Now: day(3), Current: trial, CurrentPackage: &pro, Target: pro})
	if quote.Kind != ChangeRenew || !quote.PeriodStart.Equal(ends) || quote.AmountDue != 3000 {
		t.Fatalf("kind %s start %v due %d", quote.Kind, quote.PeriodStart, quote.AmountDue)
	}
}

func TestQuoteTrialOfAnotherPlanEndsNowWithoutCredit(t *testing.T) {
	trial := sub(10, pro, StatusTrial, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(3), Current: trial, CurrentPackage: &pro, Target: basic})
	if quote.Kind != ChangeNew || quote.Effective != EffectiveNow || quote.Credit != 0 || quote.AmountDue != 1000 {
		t.Fatalf("kind %s effective %s credit %d due %d", quote.Kind, quote.Effective, quote.Credit, quote.AmountDue)
	}
	if quote.ReplacesSubscriptionID == nil || *quote.ReplacesSubscriptionID != 10 {
		t.Fatalf("replaces %v", quote.ReplacesSubscriptionID)
	}
}

func TestQuoteFromAFreePlanIsImmediate(t *testing.T) {
	current := sub(10, free, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(3), Current: current, CurrentPackage: &free, Target: pro})
	if quote.Kind != ChangeNew || quote.Effective != EffectiveNow || quote.AmountDue != 3000 {
		t.Fatalf("kind %s effective %s due %d", quote.Kind, quote.Effective, quote.AmountDue)
	}
}

func TestQuoteFromAStaffCompGivesNoCredit(t *testing.T) {
	current := sub(10, basic, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(16), Current: current, CurrentPackage: &basic, CurrentPaid: 0, Target: pro})
	if quote.Kind != ChangeUpgrade || quote.Credit != 0 || quote.AmountDue != 3000 {
		t.Fatalf("kind %s credit %d due %d", quote.Kind, quote.Credit, quote.AmountDue)
	}
	if len(quote.Lines) != 1 {
		t.Fatalf("lines %+v", quote.Lines)
	}
}

func TestQuoteAfterThePlanExpiredIsNew(t *testing.T) {
	current := sub(10, pro, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(40), Current: current, CurrentPackage: &pro, CurrentPaid: 3000, Target: pro})
	if quote.Kind != ChangeNew || quote.Effective != EffectiveNow || !quote.PeriodStart.Equal(day(40)) {
		t.Fatalf("kind %s effective %s start %v", quote.Kind, quote.Effective, quote.PeriodStart)
	}
}

func TestQuoteRefusesRebuyingAOneTimePlan(t *testing.T) {
	current := sub(10, lifetime, StatusActive, periodFrom)
	_, err := QuoteChange(QuoteInput{Now: day(3), Current: current, CurrentPackage: &lifetime, CurrentPaid: 50000, Target: lifetime})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.InvalidState {
		t.Fatalf("err %v, want INVALID_STATE", err)
	}
}

func TestQuoteRoundsUnusedDaysUpAndMoneyHalfUp(t *testing.T) {
	current := sub(10, basic, StatusActive, periodFrom)
	// 12 hours before the end: one (rounded up) day left. 1000/30 = 33.3 → 33.
	quote := mustQuote(t, QuoteInput{Now: current.ExpiresAt.Add(-12 * time.Hour), Current: current, CurrentPackage: &basic, CurrentPaid: 1000, Target: pro})
	if quote.UnusedDays != 1 || quote.Credit != 33 {
		t.Fatalf("unused %d credit %d", quote.UnusedDays, quote.Credit)
	}
	// 1000 × 2 / 30 = 66.7 → 67 (half up).
	quote = mustQuote(t, QuoteInput{Now: current.ExpiresAt.Add(-36 * time.Hour), Current: current, CurrentPackage: &basic, CurrentPaid: 1000, Target: pro})
	if quote.UnusedDays != 2 || quote.Credit != 67 {
		t.Fatalf("unused %d credit %d", quote.UnusedDays, quote.Credit)
	}
}

func TestQuoteShortMonth(t *testing.T) {
	start := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC) // February 2027: 28 days
	current := sub(10, basic, StatusActive, start)
	quote := mustQuote(t, QuoteInput{Now: start.AddDate(0, 0, 14), Current: current, CurrentPackage: &basic, CurrentPaid: 1000, Target: pro})
	if quote.PeriodDays != 28 || quote.UnusedDays != 14 || quote.Credit != 500 {
		t.Fatalf("period %d unused %d credit %d", quote.PeriodDays, quote.UnusedDays, quote.Credit)
	}
}

func TestQuoteUnusedDaysNeverExceedThePeriod(t *testing.T) {
	// A period that has not started yet (a scheduled plan counted early).
	current := sub(10, basic, StatusActive, day(10))
	quote := mustQuote(t, QuoteInput{Now: day(5), Current: current, CurrentPackage: &basic, CurrentPaid: 1000, Target: pro})
	if quote.UnusedDays != quote.PeriodDays || quote.Credit != 1000 {
		t.Fatalf("unused %d period %d credit %d", quote.UnusedDays, quote.PeriodDays, quote.Credit)
	}
}

func TestLinesRoundTrip(t *testing.T) {
	if DecodeLines(nil) != nil || DecodeLines([]byte("[]")) != nil || DecodeLines([]byte("{bad")) != nil {
		t.Fatal("empty or bad lines should decode to nil")
	}
	if string(EncodeLines(nil)) != "[]" {
		t.Fatal("no lines should encode to []")
	}
	quote := mustQuote(t, QuoteInput{Now: day(1), Target: pro, Balance: 100})
	back := DecodeLines(EncodeLines(quote.Lines))
	if len(back) != 2 || back[0].PackageName != "Pro" || back[1].Amount != -100 || back[0].PeriodStart == nil {
		t.Fatalf("round trip %+v", back)
	}
}

func TestIsHigherComparesPerDay(t *testing.T) {
	cases := []struct {
		target, current Package
		want            bool
	}{
		{pro, basic, true},
		{basic, pro, false},
		{proYearly, pro, true},    // a longer cycle: now
		{pro, proYearly, true},    // 100/day vs 82/day
		{basic, proYearly, false}, // 33/day vs 82/day
		{basicYear, pro, true},    // a longer cycle, even at 33/day: a longer commitment, now
		{free, proYearly, false},
		{team, pro, true}, // same price: immediate
		{free, basic, false},
		{basic, free, true},
	}
	for _, c := range cases {
		if got := isHigher(c.target, c.current); got != c.want {
			t.Errorf("isHigher(%s, %s) = %v, want %v", c.target.Name, c.current.Name, got, c.want)
		}
	}
}

func TestQuoteYearlyToACheaperMonthlyPlanWaits(t *testing.T) {
	current := sub(10, proYearly, StatusActive, periodFrom)
	quote := mustQuote(t, QuoteInput{Now: day(40), Current: current, CurrentPackage: &proYearly, CurrentPaid: 30000, Target: basic})
	if quote.Kind != ChangeDowngrade || quote.Effective != EffectivePeriodEnd || quote.AmountDue != 0 {
		t.Fatalf("kind %s effective %s due %d", quote.Kind, quote.Effective, quote.AmountDue)
	}
}

func TestQuoteAFreePlanNeverNeedsAPayment(t *testing.T) {
	quote := mustQuote(t, QuoteInput{Now: day(1), Target: free, Balance: 500})
	if quote.RequiresPayment || quote.AmountDue != 0 || quote.BalanceApplied != 0 || quote.Effective != EffectiveNow {
		t.Fatalf("requires %v due %d balance %d effective %s", quote.RequiresPayment, quote.AmountDue, quote.BalanceApplied, quote.Effective)
	}
}
