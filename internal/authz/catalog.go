package authz

// Role names seeded by db:seed.
const (
	SuperAdminRole = "Super-Admin"
	AdminRole      = "Admin"
	UserRole       = "User"
	DeveloperRole  = "Developer"
)

// GuardName is the guard value stored on roles and permissions.
const GuardName = "sanctum"

// ModelTypeUser is the RBAC pivot model type for user rows.
const ModelTypeUser = "user"

// Permission is one entry of the application permission catalog.
type Permission struct {
	Name        string
	Group       string
	Description string
	IsSystem    bool
}

// Permission names referenced by platform code and services.
const (
	PermRoleViewAll        = "role.view.all"
	PermRoleView           = "role.view"
	PermRoleCreate         = "role.create"
	PermRoleInternalCreate = "role.internal.create"
	PermRoleUpdate         = "role.update"
	PermRoleInternalUpdate = "role.internal.update"
	PermRoleDelete         = "role.delete"

	PermUserViewAll            = "user.view.all"
	PermUserView               = "user.view"
	PermUserCreate             = "user.create"
	PermUserInternalCreate     = "user.internal.create"
	PermUserUpdate             = "user.update"
	PermUserTwoFactorReset     = "user.two_factor.reset"
	PermWebhookManage          = "webhook.manage"
	PermUserInternalUpdate     = "user.internal.update"
	PermUserDelete             = "user.delete"
	PermUserInternalDelete     = "user.internal.delete"
	PermUserRoleAssign         = "user.role.assign"
	PermUserInternalRoleAssign = "user.internal.role.assign"
	PermUserTenantSwitch       = "user.tenant.switch"
	PermUserBranchSwitch       = "user.branch.switch"
	PermUserImpersonate        = "user.impersonate"
	PermUserImport             = "user.import"
	PermUserExport             = "user.export"
	PermUserInternalImport     = "user.internal.import"
	PermUserInternalExport     = "user.internal.export"
	// Account erasure and personal-data export
	// (docs/design/account-deletion-and-export.md).
	PermUserErase                 = "user.erase"
	PermUserPrivacyExport         = "user.privacy_export"
	PermUserInternalErase         = "user.internal.erase"
	PermUserInternalPrivacyExport = "user.internal.privacy_export"

	PermFileView            = "file.view"
	PermFileViewAll         = "file.view.all"
	PermFileCreate          = "file.create"
	PermFileDelete          = "file.delete"
	PermFileInternalView    = "file.internal.view"
	PermFileInternalViewAll = "file.internal.view.all"
	PermFileInternalCreate  = "file.internal.create"
	PermFileInternalDelete  = "file.internal.delete"

	PermNotificationView    = "notification.view"
	PermNotificationViewAll = "notification.view.all"
	PermNotificationCreate  = "notification.create"
	PermNotificationUpdate  = "notification.update"

	PermActivityView    = "activity.view"
	PermActivityViewAll = "activity.view.all"

	// OAuth clients registered by hand for the MCP connector (phase 22).
	PermOAuthClientManage  = "oauth_client.manage"
	PermNotificationDelete = "notification.delete"

	PermDataProcessingViewAll = "data_processing_job.view.all"
	PermDataProcessingView    = "data_processing_job.view"

	PermBranchViewAll = "branch.view.all"
	PermBranchView    = "branch.view"
	PermBranchCreate  = "branch.create"
	PermBranchUpdate  = "branch.update"
	PermBranchDelete  = "branch.delete"

	// The organization's own plan, usage and payments (tenant routes). Staff
	// routes keep the system permissions subscription.* / payment.*.
	PermBillingView   = "billing.view"
	PermBillingManage = "billing.manage"

	// The caller's own organization (tenant-resolved routes).
	PermOrganizationUpdate = "organization.update"

	PermTenantViewAll = "tenant.view.all"
	PermTenantView    = "tenant.view"
	PermTenantCreate  = "tenant.create"
	PermTenantUpdate  = "tenant.update"
	PermTenantDelete  = "tenant.delete"

	PermPermissionView = "permission.view"

	PermSettingUpdate             = "setting.update"
	PermSettingInternalView       = "setting.internal.view"
	PermSettingInternalUpdate     = "setting.internal.update"
	PermFeatureFlagInternalUpdate = "feature_flag.internal.update"

	PermAnnouncementView   = "announcement.view"
	PermAnnouncementManage = "announcement.manage"

	PermSettingsGroupViewAll = "settings_group.view.all"
	PermSettingsGroupView    = "settings_group.view"
	PermSettingsGroupCreate  = "settings_group.create"
	PermSettingsGroupUpdate  = "settings_group.update"
	PermSettingsGroupDelete  = "settings_group.delete"

	PermDevView   = "dev.view"
	PermDevManage = "dev.manage"
)

// Catalog is the application permission catalog.
var Catalog = []Permission{
	// ── role ──────────────────────────────────────────────
	{Name: PermRoleView, Group: "role", Description: "Can view role details"},
	{Name: PermRoleViewAll, Group: "role", Description: "Can view all roles"},
	{Name: PermRoleCreate, Group: "role", Description: "Can create new roles"},
	{Name: PermRoleInternalCreate, Group: "role", IsSystem: true, Description: "Can create new internal roles"},
	{Name: PermRoleUpdate, Group: "role", Description: "Can modify existing roles"},
	{Name: PermRoleInternalUpdate, Group: "role", IsSystem: true, Description: "Can modify existing internal roles"},
	{Name: PermRoleDelete, Group: "role", Description: "Can delete roles"},

	// ── tenant ────────────────────────────────────────────
	{Name: PermTenantView, Group: "tenant", IsSystem: true, Description: "Can view tenant details"},
	{Name: PermTenantViewAll, Group: "tenant", IsSystem: true, Description: "Can view all tenants"},
	{Name: PermTenantCreate, Group: "tenant", IsSystem: true, Description: "Can create new tenants"},
	{Name: PermTenantUpdate, Group: "tenant", IsSystem: true, Description: "Can modify existing tenants"},
	{Name: PermTenantDelete, Group: "tenant", IsSystem: true, Description: "Can delete tenants"},

	// ── branch ────────────────────────────────────────────
	{Name: PermBranchView, Group: "branch", Description: "Can view branch details"},
	{Name: PermBranchViewAll, Group: "branch", Description: "Can view all branches"},
	{Name: PermBranchCreate, Group: "branch", Description: "Can create new branches"},
	{Name: PermBranchUpdate, Group: "branch", Description: "Can modify existing branches"},
	{Name: PermBranchDelete, Group: "branch", Description: "Can delete branches"},

	// ── webhook ───────────────────────────────────────────
	{Name: PermWebhookManage, Group: "webhook", Description: "Can manage the tenant's webhook endpoints and see their deliveries"},

	// ── user ──────────────────────────────────────────────
	{Name: PermUserView, Group: "user", Description: "Can view user details"},
	{Name: PermUserViewAll, Group: "user", Description: "Can view all users"},
	{Name: PermUserCreate, Group: "user", Description: "Can create new users"},
	{Name: PermUserInternalCreate, Group: "user", IsSystem: true, Description: "Can create new users"},
	{Name: PermUserUpdate, Group: "user", Description: "Can modify existing users"},
	{Name: PermUserInternalUpdate, Group: "user", IsSystem: true, Description: "Can modify existing users"},
	{Name: PermUserTwoFactorReset, Group: "user", Description: "Can turn off a user's two-factor authentication (lost phone or mailbox)"},
	{Name: PermUserDelete, Group: "user", Description: "Can delete users"},
	{Name: PermUserInternalDelete, Group: "user", IsSystem: true, Description: "Can delete users across tenants"},
	{Name: PermUserRoleAssign, Group: "user", Description: "Can Assign role to users"},
	{Name: PermUserInternalRoleAssign, Group: "user", IsSystem: true, Description: "Can assign roles to users across tenants"},
	{Name: "user.export", Group: "user", Description: "Can export all users"},
	{Name: "user.import", Group: "user", Description: "Can import users"},
	{Name: PermUserInternalExport, Group: "user", IsSystem: true, Description: "Can export internal users"},
	{Name: PermUserErase, Group: "user", Description: "Can erase a user's personal data (right to erasure)"},
	{Name: PermUserPrivacyExport, Group: "user", Description: "Can export a user's personal data (right of access)"},
	{Name: PermUserInternalErase, Group: "user", IsSystem: true, Description: "Can erase any user's personal data across tenants"},
	{Name: PermUserInternalPrivacyExport, Group: "user", IsSystem: true, Description: "Can export any user's personal data across tenants"},
	{Name: PermUserInternalImport, Group: "user", IsSystem: true, Description: "Can import internal users"},
	{Name: PermUserTenantSwitch, Group: "user", IsSystem: true, Description: "Can switch active tenant"},
	{Name: PermUserBranchSwitch, Group: "user", Description: "Can switch active branch"},
	{Name: PermUserImpersonate, Group: "user", IsSystem: true, Description: "Can act as another user (X-Acting-User)"},

	// ── file ──────────────────────────────────────────────
	{Name: PermFileView, Group: "file", Description: "Can view file details"},
	{Name: PermFileViewAll, Group: "file", Description: "Can view all files"},
	{Name: PermFileCreate, Group: "file", Description: "Can upload files"},
	{Name: PermFileDelete, Group: "file", Description: "Can delete files"},
	{Name: PermFileInternalView, Group: "file", IsSystem: true, Description: "Can view any file details across tenants"},
	{Name: PermFileInternalViewAll, Group: "file", IsSystem: true, Description: "Can view all files across tenants"},
	{Name: PermFileInternalCreate, Group: "file", IsSystem: true, Description: "Can upload global (platform) files"},
	{Name: PermFileInternalDelete, Group: "file", IsSystem: true, Description: "Can delete files across tenants"},

	// ── data_processing_job ───────────────────────────────
	{Name: PermDataProcessingViewAll, Group: "data_processing_job", Description: "Can view all data processing jobs"},
	{Name: PermDataProcessingView, Group: "data_processing_job", Description: "Can view data processing job details"},

	// ── organization (the tenant's own profile) ───────────
	{Name: PermOrganizationUpdate, Group: "organization", Description: "Can edit the organization's name and contact details"},

	// ── billing (the tenant's own plan and payments) ──────
	{Name: PermBillingView, Group: "billing", Description: "Can view the organization's plan, usage and payments"},
	{Name: PermBillingManage, Group: "billing", Description: "Can choose the organization's plan and pay for it"},

	// ── package ───────────────────────────────────────────
	{Name: "package.view.all", Group: "package", IsSystem: true, Description: "Can view all packages"},
	{Name: "package.view", Group: "package", IsSystem: true, Description: "Can view package details"},
	{Name: "package.create", Group: "package", IsSystem: true, Description: "Can create new packages"},
	{Name: "package.update", Group: "package", IsSystem: true, Description: "Can modify existing packages"},
	{Name: "package.delete", Group: "package", IsSystem: true, Description: "Can delete packages"},

	// ── subscription ──────────────────────────────────────
	{Name: "subscription.view.all", Group: "subscription", IsSystem: true, Description: "Can view all subscriptions"},
	{Name: "subscription.view", Group: "subscription", IsSystem: true, Description: "Can view subscription details"},
	{Name: "subscription.manage", Group: "subscription", IsSystem: true, Description: "Can manage subscriptions (subscribe, cancel)"},
	{Name: "subscription.override", Group: "subscription", IsSystem: true, Description: "Can apply feature overrides on subscriptions"},

	// ── payment ───────────────────────────────────────────
	{Name: "payment.view.all", Group: "payment", IsSystem: true, Description: "Can view all payments"},
	{Name: "payment.view", Group: "payment", IsSystem: true, Description: "Can view payment details"},
	{Name: "payment.record", Group: "payment", IsSystem: true, Description: "Can record manual payments (cash/bank transfer)"},

	// ── payment_approval ──────────────────────────────────
	{Name: "payment_approval.view.all", Group: "payment_approval", IsSystem: true, Description: "Can view all manual payment approval requests"},
	{Name: "payment_approval.review", Group: "payment_approval", IsSystem: true, Description: "Can approve or reject manual payment requests"},

	// ── notification ──────────────────────────────────────
	{Name: PermNotificationView, Group: "notification", IsSystem: true, Description: "Can view notification details"},
	{Name: PermNotificationViewAll, Group: "notification", IsSystem: true, Description: "Can view all notifications"},
	{Name: PermNotificationCreate, Group: "notification", IsSystem: true, Description: "Can create new notifications"},
	{Name: PermNotificationUpdate, Group: "notification", IsSystem: true, Description: "Can modify existing notifications"},

	{Name: PermActivityView, Group: "activity", Description: "Can view the activity log"},
	{Name: PermActivityViewAll, Group: "activity", IsSystem: true, Description: "Can view every tenant's activity log and the system audit log"},
	{Name: PermNotificationDelete, Group: "notification", IsSystem: true, Description: "Can delete notifications"},

	// ── oauth_client ──────────────────────────────────────
	{Name: PermOAuthClientManage, Group: "oauth_client", IsSystem: true, Description: "Can register and manage OAuth clients for the MCP connector"},

	// ── setting ───────────────────────────────────────────
	{Name: PermSettingUpdate, Group: "setting", Description: "Can change tenant setting values"},
	{Name: PermSettingInternalView, Group: "setting", IsSystem: true, Description: "Can view settings for any target"},
	{Name: PermSettingInternalUpdate, Group: "setting", IsSystem: true, Description: "Can change settings for any target"},

	// ── feature_flag ──────────────────────────────────────
	{Name: PermFeatureFlagInternalUpdate, Group: "feature_flag", IsSystem: true, Description: "Can change feature flags for any target"},

	// ── announcement ──────────────────────────────────────
	{Name: PermAnnouncementView, Group: "announcement", IsSystem: true, Description: "Can view announcements and how they did"},
	{Name: PermAnnouncementManage, Group: "announcement", IsSystem: true, Description: "Can create, change and delete announcements"},

	// ── settings_group ────────────────────────────────────
	{Name: PermSettingsGroupViewAll, Group: "settings_group", IsSystem: true, Description: "Can view all settings groups"},
	{Name: PermSettingsGroupView, Group: "settings_group", IsSystem: true, Description: "Can view settings group details"},
	{Name: PermSettingsGroupCreate, Group: "settings_group", IsSystem: true, Description: "Can create settings groups"},
	{Name: PermSettingsGroupUpdate, Group: "settings_group", IsSystem: true, Description: "Can modify settings groups"},
	{Name: PermSettingsGroupDelete, Group: "settings_group", IsSystem: true, Description: "Can delete settings groups"},

	// ── dev ───────────────────────────────────────────────
	{Name: PermDevView, Group: "dev", IsSystem: true, Description: "Can view the developer dashboard"},
	{Name: PermDevManage, Group: "dev", IsSystem: true, Description: "Can execute developer dashboard actions"},
}

// DeveloperPermissions is the hidden Developer role: developer-dashboard
// access (dev.view, dev.manage) plus user.impersonate. It holds no tenant or
// system data-administration permissions — a leaked developer token must not
// be a platform super-user (review H13). Acting as a user was granted on
// purpose (phase 21): every use is written to the system audit log, and the
// target refusals (no admins, no platform accounts) still apply.
func DeveloperPermissions() []Permission {
	out := make([]Permission, 0, 3)
	for _, permission := range Catalog {
		if permission.Group == "dev" || permission.Name == PermUserImpersonate {
			out = append(out, permission)
		}
	}
	return out
}

// AdminTenantPermissions is the tenant Admin role composition, matching
// RoleService::createDefaultRolesForTenant: role + branch + user permissions
// minus the internal role-management permissions; system permissions are never
// granted to tenant roles.
func AdminTenantPermissions() []Permission {
	exclude := map[string]struct{}{
		PermRoleInternalCreate: {},
		PermRoleInternalUpdate: {},
	}
	out := make([]Permission, 0, len(Catalog))
	for _, permission := range Catalog {
		if permission.IsSystem {
			continue
		}
		switch permission.Group {
		case "role", "branch", "user", "file", "setting", "activity", "webhook", "billing", "organization":
			if _, skip := exclude[permission.Name]; skip {
				continue
			}
			out = append(out, permission)
		}
	}
	return out
}
