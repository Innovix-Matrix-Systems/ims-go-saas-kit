package domain

import "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"

// Monthly and yearly on one plan (docs/guides/billing.md#monthly-and-yearly).
//
// A plan has a base price and cycle (Price, BillingCycle). A monthly plan can
// also be offered yearly (YearlyPrice). A subscription records the cycle the
// organization chose. Every price calculation works on the plan as offered at
// that cycle (ForCycle), so proration, checkout, renewal and invoices need no
// notion of a second price.

// Cycles lists the billing cycles the plan is offered at, base cycle first.
func (p Package) Cycles() []string {
	if p.BillingCycle == CycleMonthly && p.YearlyPrice != nil {
		return []string{CycleMonthly, CycleYearly}
	}
	return []string{p.BillingCycle}
}

// Offers reports whether the plan is offered at cycle ("" is the base cycle).
func (p Package) Offers(cycle string) bool {
	if cycle == "" {
		return true
	}
	for _, offered := range p.Cycles() {
		if offered == cycle {
			return true
		}
	}
	return false
}

// ForCycle is the plan as offered at cycle: the same plan with that cycle's
// price and BillingCycle. "" is the base cycle. A cycle the plan is not
// offered at is a validation error.
func (p Package) ForCycle(cycle string) (Package, error) {
	if cycle == "" || cycle == p.BillingCycle {
		return p, nil
	}
	if cycle == CycleYearly && p.BillingCycle == CycleMonthly && p.YearlyPrice != nil {
		yearly := p
		yearly.Price = *p.YearlyPrice
		yearly.BillingCycle = CycleYearly
		return yearly, nil
	}
	return Package{}, apperr.Newf(apperr.ValidationFailed, "%s is not offered %s.", p.Name, cycleWord(cycle))
}

// Cycle is the billing cycle the subscription runs on: the one chosen, or
// the plan's own.
func (s Subscription) Cycle(pkg Package) string {
	if s.BillingCycle != nil && *s.BillingCycle != "" {
		return *s.BillingCycle
	}
	return pkg.BillingCycle
}

// PlanFor is the plan the subscription runs on, at the subscription's cycle.
// A cycle the plan no longer offers (staff removed the yearly price) keeps
// the plan's base price, so the renewal still has a price.
func (s Subscription) PlanFor(pkg Package) Package {
	offered, err := pkg.ForCycle(s.Cycle(pkg))
	if err != nil {
		return pkg
	}
	return offered
}

func cycleWord(cycle string) string {
	switch cycle {
	case CycleMonthly:
		return "monthly"
	case CycleYearly:
		return "yearly"
	case CycleOneTime:
		return "as a one-time purchase"
	default:
		return "on the " + cycle + " cycle"
	}
}
