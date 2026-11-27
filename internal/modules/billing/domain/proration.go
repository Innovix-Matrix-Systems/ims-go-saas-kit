package domain

import (
	"encoding/json"
	"math"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

// Why a payment is made (payments.kind) and what a plan change is.
const (
	// ChangeNew: no paid plan in force; the plan starts when paid.
	ChangeNew = "new"
	// ChangeRenew: the same plan (or the plan chosen for the renewal) for the
	// next period; it starts when the current one ends.
	ChangeRenew = "renew"
	// ChangeUpgrade: a plan that costs more per day; it starts when paid and
	// the unused time of the current plan is credited.
	ChangeUpgrade = "upgrade"
	// ChangeDowngrade: a plan that costs less per day; it starts when the
	// current period ends (nothing is refunded).
	ChangeDowngrade = "downgrade"
	// ChangeStaff: staff put the organization on a plan.
	ChangeStaff = "staff"
)

// When a change takes effect.
const (
	EffectiveNow       = "now"
	EffectivePeriodEnd = "period_end"
)

// Invoice line types.
const (
	LinePlan          = "plan"
	LineUnusedTime    = "unused_time"
	LineAccountCredit = "account_credit"
)

// MethodCredit is the payment method of a checkout the account credit
// covered entirely (amount 0).
const MethodCredit = "credit"

// InvoiceLine is one line of an invoice: the plan bought, the credit for the
// unused time of the plan it replaces, or account credit used.
type InvoiceLine struct {
	Type         string     `json:"type"`
	PackageID    int64      `json:"package_id,omitempty"`
	PackageName  string     `json:"package_name,omitempty"`
	BillingCycle string     `json:"billing_cycle,omitempty"`
	PeriodStart  *time.Time `json:"period_start,omitempty"`
	PeriodEnd    *time.Time `json:"period_end,omitempty"`
	// Days is the unused days credited (unused_time lines).
	Days   int   `json:"days,omitempty"`
	Amount int64 `json:"amount"`
}

// DecodeLines reads stored invoice lines (nil for none or bad JSON).
func DecodeLines(raw []byte) []InvoiceLine {
	if len(raw) == 0 {
		return nil
	}
	var lines []InvoiceLine
	if err := json.Unmarshal(raw, &lines); err != nil || len(lines) == 0 {
		return nil
	}
	return lines
}

// EncodeLines stores invoice lines ("[]" for none).
func EncodeLines(lines []InvoiceLine) []byte {
	if len(lines) == 0 {
		return []byte("[]")
	}
	raw, err := json.Marshal(lines)
	if err != nil {
		return []byte("[]")
	}
	return raw
}

// QuoteInput is everything a plan change is priced from.
type QuoteInput struct {
	Now time.Time
	// Current is the subscription in force (active, trial, or a scheduled
	// one whose turn has come); nil when there is none.
	Current *Subscription
	// CurrentPackage is Current's plan.
	CurrentPackage *Package
	// CurrentPaid is the value the organization paid for Current's period
	// (0 for trials, free plans and staff comps).
	CurrentPaid int64
	// Scheduled is a period already arranged to follow Current (a paid
	// renewal or a free plan), nil when there is none.
	Scheduled *Subscription
	// Target is the plan asked for.
	Target Package
	// Balance is the account credit the organization can use.
	Balance int64
}

// Quote is what a plan change costs and when it takes effect.
type Quote struct {
	Kind      string
	Effective string
	Target    Package
	// Price is the target plan's price (the value bought, before credits).
	Price int64
	// Credit is the value of the current plan's unused time (upgrades).
	Credit int64
	// UnusedDays and PeriodDays are what Credit was computed from.
	UnusedDays int
	PeriodDays int
	// BalanceApplied is the account credit used.
	BalanceApplied int64
	// AmountDue is what is paid now.
	AmountDue int64
	// Surplus is unused-time credit beyond the new price: it becomes account
	// credit.
	Surplus int64
	// DueAtRenewal is what a downgrade will cost when the organization renews
	// onto it (nothing is paid now).
	DueAtRenewal int64
	// PeriodStart and PeriodEnd are the new period (for EffectiveNow they
	// move to the moment of payment).
	PeriodStart time.Time
	PeriodEnd   time.Time
	// RequiresPayment is false when nothing is paid now (a downgrade waits
	// for the renewal; a free plan needs no payment).
	RequiresPayment bool
	// ReplacesSubscriptionID is the subscription an upgrade or a new plan
	// replaces.
	ReplacesSubscriptionID *int64
	Lines                  []InvoiceLine
}

// CycleDays is the nominal length of a billing cycle, used to compare plans
// of different cycles per day.
func CycleDays(cycle string) int64 {
	switch cycle {
	case CycleYearly:
		return 365
	case CycleOneTime:
		return 36500
	default:
		return 30
	}
}

// PeriodEnd is when a period of the cycle started at from ends.
func PeriodEnd(cycle string, from time.Time) time.Time {
	switch cycle {
	case CycleYearly:
		return from.AddDate(1, 0, 0)
	case CycleOneTime:
		return from.AddDate(100, 0, 0)
	default:
		return from.AddDate(0, 1, 0)
	}
}

// QuoteChange prices a move to in.Target (docs/guides/billing.md#changing-plans):
// upgrades start now and credit the unused time of the current period;
// downgrades and renewals start when the current period ends; account credit
// is used before any money. Amounts are integers in minor units.
func QuoteChange(in QuoteInput) (Quote, error) {
	target := in.Target
	// A free plan never needs a payment.
	quote := Quote{Target: target, Price: target.Price, RequiresPayment: target.Price > 0}
	current := in.Current
	if current == nil || in.CurrentPackage == nil || current.ExpiresAt == nil || !current.ExpiresAt.After(in.Now) {
		quote.Kind = ChangeNew
		quote.Effective = EffectiveNow
		quote.PeriodStart = in.Now
		quote.PeriodEnd = PeriodEnd(target.BillingCycle, in.Now)
		quote.finish(in.Balance)
		return quote, nil
	}
	currentPkg := in.CurrentPackage
	periodEnd := *current.ExpiresAt

	// The same plan at the same cycle (or the plan and cycle already chosen
	// for the renewal) renews. CurrentPackage is the plan at the current
	// subscription's cycle (Subscription.PlanFor).
	renewing := (target.ID == current.PackageID && target.BillingCycle == currentPkg.BillingCycle) ||
		(current.NextPackageID != nil && *current.NextPackageID == target.ID &&
			(current.NextBillingCycle == nil || *current.NextBillingCycle == target.BillingCycle))
	if renewing {
		if target.ID == current.PackageID && target.BillingCycle == CycleOneTime && current.Status != StatusTrial {
			return Quote{}, apperr.New(apperr.InvalidState, "This plan is already yours for good.")
		}
		if in.Scheduled != nil {
			return Quote{}, apperr.New(apperr.InvalidState, "Your next period is already arranged.")
		}
		quote.Kind = ChangeRenew
		quote.Effective = EffectivePeriodEnd
		quote.PeriodStart = periodEnd
		quote.PeriodEnd = PeriodEnd(target.BillingCycle, periodEnd)
		quote.finish(in.Balance)
		return quote, nil
	}

	// A trial or a free plan has no paid time to keep: the new plan starts
	// now, as for an organization without a plan.
	if current.Status == StatusTrial || currentPkg.Price == 0 {
		quote.Kind = ChangeNew
		quote.Effective = EffectiveNow
		quote.PeriodStart = in.Now
		quote.PeriodEnd = PeriodEnd(target.BillingCycle, in.Now)
		quote.ReplacesSubscriptionID = &current.ID
		quote.finish(in.Balance)
		return quote, nil
	}

	// The same plan on a shorter cycle (yearly → monthly) is a downgrade even
	// though the shorter cycle costs more per day: it waits for the period
	// end, as a cancellation would.
	sameOnShorter := target.ID == current.PackageID && CycleDays(target.BillingCycle) < CycleDays(currentPkg.BillingCycle)
	if !sameOnShorter && isHigher(target, *currentPkg) {
		quote.Kind = ChangeUpgrade
		quote.Effective = EffectiveNow
		quote.PeriodStart = in.Now
		quote.PeriodEnd = PeriodEnd(target.BillingCycle, in.Now)
		quote.ReplacesSubscriptionID = &current.ID
		quote.PeriodDays, quote.UnusedDays = periodDays(current, currentPkg.BillingCycle, in.Now)
		if quote.PeriodDays > 0 && in.CurrentPaid > 0 {
			quote.Credit = roundDiv(in.CurrentPaid*int64(quote.UnusedDays), int64(quote.PeriodDays))
		}
		quote.finish(in.Balance)
		return quote, nil
	}

	// Downgrade: the current plan runs to its end; nothing is refunded.
	if in.Scheduled != nil {
		return Quote{}, apperr.New(apperr.InvalidState, "Your next period is already arranged.")
	}
	quote.Kind = ChangeDowngrade
	quote.Effective = EffectivePeriodEnd
	quote.PeriodStart = periodEnd
	quote.PeriodEnd = PeriodEnd(target.BillingCycle, periodEnd)
	quote.RequiresPayment = false
	quote.DueAtRenewal = target.Price
	quote.Lines = []InvoiceLine{planLine(target, quote.PeriodStart, quote.PeriodEnd)}
	return quote, nil
}

// finish writes the lines and the amounts: price − unused-time credit −
// account credit, never below zero (the rest of the credit is Surplus).
func (q *Quote) finish(balance int64) {
	lines := []InvoiceLine{planLine(q.Target, q.PeriodStart, q.PeriodEnd)}
	due := q.Price
	if q.Credit > 0 {
		lines = append(lines, InvoiceLine{Type: LineUnusedTime, Days: q.UnusedDays, Amount: -q.Credit})
		due -= q.Credit
		if due < 0 {
			q.Surplus = -due
			due = 0
		}
	}
	if q.RequiresPayment && due > 0 && balance > 0 {
		q.BalanceApplied = min(balance, due)
		due -= q.BalanceApplied
		lines = append(lines, InvoiceLine{Type: LineAccountCredit, Amount: -q.BalanceApplied})
	}
	if !q.RequiresPayment {
		due = 0
	}
	q.AmountDue = due
	q.Lines = lines
}

// NameUnusedTime puts the current plan's name on the unused-time line.
func (q *Quote) NameUnusedTime(pkg Package) {
	for i := range q.Lines {
		if q.Lines[i].Type == LineUnusedTime {
			q.Lines[i].PackageID = pkg.ID
			q.Lines[i].PackageName = pkg.Name
			q.Lines[i].BillingCycle = pkg.BillingCycle
		}
	}
}

func planLine(pkg Package, start, end time.Time) InvoiceLine {
	return InvoiceLine{
		Type: LinePlan, PackageID: pkg.ID, PackageName: pkg.Name, BillingCycle: pkg.BillingCycle,
		PeriodStart: &start, PeriodEnd: &end, Amount: pkg.Price,
	}
}

// isHigher reports whether moving from current to target takes effect now:
// a longer billing cycle (monthly → yearly, as Stripe and Claude do), or a
// plan that costs at least as much per day (a same-priced other plan counts:
// the customer wants it now). Anything else waits for the period end.
func isHigher(target, current Package) bool {
	if CycleDays(target.BillingCycle) > CycleDays(current.BillingCycle) && target.Price > 0 {
		return true
	}
	return target.Price*CycleDays(current.BillingCycle) >= current.Price*CycleDays(target.BillingCycle)
}

// periodDays returns the length of the current period and the days left in
// it, in whole days; days left are rounded up (in the customer's favour) and
// never exceed the period.
func periodDays(current *Subscription, cycle string, now time.Time) (period, unused int) {
	end := *current.ExpiresAt
	start := end.AddDate(0, 0, -int(CycleDays(cycle)))
	if current.StartsAt != nil && current.StartsAt.Before(end) {
		start = *current.StartsAt
	}
	period = int(math.Round(end.Sub(start).Hours() / 24))
	if period < 1 {
		period = 1
	}
	unused = int(math.Ceil(end.Sub(now).Hours() / 24))
	unused = max(0, min(unused, period))
	return period, unused
}

// roundDiv divides rounding half up (both operands non-negative).
func roundDiv(a, b int64) int64 {
	return (2*a + b) / (2 * b)
}
