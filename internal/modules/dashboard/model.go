package dashboard

import "time"

// Period is the span a dashboard covers, in the viewer's time zone.
type Period struct {
	Days     int    `json:"days"`
	From     string `json:"from"`
	To       string `json:"to"`
	TimeZone string `json:"time_zone"`
}

// Point is one day's value.
type Point struct {
	Date  string `json:"date"`
	Value int64  `json:"value"`
}

// Series is a value per day over the period, its total, and the total of the
// same number of days before it (for the change).
type Series struct {
	Points        []Point `json:"points"`
	Total         int64   `json:"total"`
	PreviousTotal int64   `json:"previous_total"`
}

// TenantDashboard is an organization's home page. Sections the caller may
// not see are null.
type TenantDashboard struct {
	Period   Period          `json:"period"`
	Members  *TenantMembers  `json:"members"`
	Activity *TenantActivity `json:"activity"`
	Files    *FilesSummary   `json:"files"`
	Branches *int64          `json:"branches"`
	Roles    *int64          `json:"roles"`
}

// TenantMembers counts the organization's members.
type TenantMembers struct {
	Total              int64  `json:"total"`
	Active             int64  `json:"active"`
	PendingInvitations int64  `json:"pending_invitations"`
	SignedInRecently   int64  `json:"signed_in_recently"`
	Added              Series `json:"added"`
}

// TenantActivity is the organization's activity log at a glance.
type TenantActivity struct {
	Events     Series         `json:"events"`
	TopActions []RankedAction `json:"top_actions"`
	TopActors  []RankedActor  `json:"top_actors"`
}

// RankedAction is an action and how often it happened in the period.
type RankedAction struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

// RankedActor is a member and how many changes they made in the period.
type RankedActor struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// FilesSummary is the organization's stored files, in all and per type
// (largest first).
type FilesSummary struct {
	Files  int64           `json:"files"`
	Bytes  int64           `json:"bytes"`
	ByType []FileTypeShare `json:"by_type"`
}

// FileTypeShare is one file type's count and size.
type FileTypeShare struct {
	Type  string `json:"type"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

// PlatformDashboard is the staff home page. Sections the caller may not see
// are null.
type PlatformDashboard struct {
	Period        Period                 `json:"period"`
	Organizations *PlatformOrganizations `json:"organizations"`
	Users         *PlatformUsers         `json:"users"`
	Activity      *Series                `json:"activity"`
	Revenue       *PlatformRevenue       `json:"revenue"`
	Plans         []PlanShare            `json:"plans"`
}

// PlatformOrganizations counts organizations.
type PlatformOrganizations struct {
	Total     int64                `json:"total"`
	Active    int64                `json:"active"`
	Suspended int64                `json:"suspended"`
	Added     Series               `json:"added"`
	Recent    []RecentOrganization `json:"recent"`
}

// RecentOrganization is one of the newest organizations.
type RecentOrganization struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Active    bool      `json:"active"`
	Members   int64     `json:"members"`
	CreatedAt time.Time `json:"created_at"`
}

// PlatformUsers counts organization members across the platform.
type PlatformUsers struct {
	Total            int64  `json:"total"`
	SignedInRecently int64  `json:"signed_in_recently"`
	Added            Series `json:"added"`
}

// PlatformRevenue is completed payments per currency and those awaiting
// approval. Amounts are in the currency's smallest unit, like payments.
type PlatformRevenue struct {
	Currencies []CurrencyRevenue `json:"currencies"`
	Pending    []PendingPayments `json:"pending"`
}

// CurrencyRevenue is one currency's completed payments.
type CurrencyRevenue struct {
	Currency string `json:"currency"`
	Payments int64  `json:"payments"`
	Amount   Series `json:"amount"`
}

// PendingPayments is what waits for approval in one currency.
type PendingPayments struct {
	Currency string `json:"currency"`
	Count    int64  `json:"count"`
	Amount   int64  `json:"amount"`
}

// PlanShare is how many organizations are on a plan.
type PlanShare struct {
	PackageID     int64  `json:"package_id"`
	Name          string `json:"name"`
	Organizations int64  `json:"organizations"`
}
