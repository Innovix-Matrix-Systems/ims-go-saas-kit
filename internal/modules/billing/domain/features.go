// Package billing implements packages, feature flags/limits, subscriptions,
// payments, gateway drivers and the manual payment approval workflow.
package domain

// Feature is a billable capability or numeric limit.
type Feature string

// Feature catalog.
const (
	FeatureUsersMax           Feature = "users.max"
	FeatureBranchesMax        Feature = "branches.max"
	FeaturePatientsManage     Feature = "patients.manage"
	FeatureAppointmentsManage Feature = "appointments.manage"
	FeatureInvoicesManage     Feature = "invoices.manage"
	FeatureReportsBasic       Feature = "reports.basic"
	FeatureReportsAdvanced    Feature = "reports.advanced"
	FeatureReportsExport      Feature = "reports.export"
	FeatureSMSNotifications   Feature = "sms.notifications"
	FeatureAPIAccess          Feature = "api.access"
	FeatureAuditLog           Feature = "audit.log"
	FeatureCustomBranding     Feature = "custom.branding"
	FeatureStorageMB          Feature = "storage.mb"
)

// Features lists the catalog in a stable order.
var Features = []Feature{
	FeatureUsersMax,
	FeatureBranchesMax,
	FeaturePatientsManage,
	FeatureAppointmentsManage,
	FeatureInvoicesManage,
	FeatureReportsBasic,
	FeatureReportsAdvanced,
	FeatureReportsExport,
	FeatureSMSNotifications,
	FeatureAPIAccess,
	FeatureAuditLog,
	FeatureCustomBranding,
	FeatureStorageMB,
}

// IsLimit reports whether the feature carries a numeric limit.
func (f Feature) IsLimit() bool {
	switch f {
	case FeatureUsersMax, FeatureBranchesMax, FeatureStorageMB:
		return true
	default:
		return false
	}
}

// featureDescriptions explain each feature in admin screens.
var featureDescriptions = map[Feature]string{
	FeatureUsersMax:           "Maximum user accounts per tenant",
	FeatureBranchesMax:        "Maximum branch count per tenant",
	FeaturePatientsManage:     "Create and manage patient records",
	FeatureAppointmentsManage: "Schedule and manage appointments",
	FeatureInvoicesManage:     "Create and manage invoices",
	FeatureReportsBasic:       "Basic reports (daily/monthly summaries)",
	FeatureReportsAdvanced:    "Advanced analytics and custom reports",
	FeatureReportsExport:      "Export reports to PDF/Excel",
	FeatureSMSNotifications:   "Send SMS notifications",
	FeatureAPIAccess:          "REST API access for integrations",
	FeatureAuditLog:           "Full audit log access",
	FeatureCustomBranding:     "Custom logo and branding per branch",
	FeatureStorageMB:          "File storage in MB",
}

// Description explains the feature ("" when unknown).
func (f Feature) Description() string { return featureDescriptions[f] }

// IsKnown reports whether the feature exists in the catalog.
func (f Feature) IsKnown() bool {
	for _, known := range Features {
		if f == known {
			return true
		}
	}
	return false
}

// Subscription statuses.
const (
	StatusTrial     = "trial"
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusPastDue   = "past_due"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"
)

// Payment statuses.
const (
	PaymentPending   = "pending"
	PaymentCompleted = "completed"
	PaymentFailed    = "failed"
	PaymentRefunded  = "refunded"
)

// Approval statuses.
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
)

// Billing cycles.
const (
	CycleMonthly = "monthly"
	CycleYearly  = "yearly"
	CycleOneTime = "one_time"
)

// Payment methods.
const (
	MethodBkash        = "bkash"
	MethodNagad        = "nagad"
	MethodCash         = "cash"
	MethodBankTransfer = "bank_transfer"
)

// IsManualMethod reports whether the method requires staff approval before the
// subscription activates.
func IsManualMethod(method string) bool {
	return method == MethodCash || method == MethodBankTransfer
}
