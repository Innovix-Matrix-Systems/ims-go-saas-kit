package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/onboarding"
	webapp "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/web/app"
	webdev "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/web/dev"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn/social"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	activityexport "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity/export"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/announcements"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/auth"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	billingservice "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dashboard"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/oauth"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/realtime"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/system"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/webhooks"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/health"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/metrics"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/pubsub"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/ratelimit"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/scheduler"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/webui"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/version"
)

// Dependencies is the composition root's dependency graph. It is constructed
// per selected mode (serve, CLI commands) and passed explicitly to consumers.
type Dependencies struct {
	Config         *config.Config
	Logger         *slog.Logger
	Responder      *httpx.Responder
	Binder         *httpx.Binder
	Validator      *httpx.Validator
	I18n           *i18n.Translator
	Health         *health.Registry
	HealthHandler  *health.Handler
	Metrics        *metrics.Registry
	Limiter        ratelimit.Limiter
	DB             *database.Registry
	Tx             *database.TxManager
	Cache          cache.Store
	Checker        *authz.Checker
	Authenticator  authn.Authenticator
	TenancyOpts    tenancy.ResolveOptions
	Platform       *Platform
	System         *system.Module
	Auth           *auth.Handler
	Users          *identity.Handler
	Privacy        *privacy.Handler
	Tenants        *tenants.Handler
	Branches       *branches.Handler
	Activity       *activity.Handler
	ActivityExport *activityexport.Handler
	Roles          *access.RoleHandler
	Permissions    *access.PermissionHandler
	Billing        *billing.Handler
	Onboarding     *onboarding.Handler
	Files          *files.Handler
	Notifications  *notifications.Handler
	Dashboard      *dashboard.Handler
	Announcements  *announcements.Handler
	Webhooks       *webhooks.Handler
	Processing     *dataprocessing.Handler
	Settings       *settings.Handler
	Flags          *featureflags.Handler
	Dev            *dev.Handler
	// Realtime serves the live-update stream; RealtimeHub holds this
	// process's open streams and RealtimeListener feeds it from Postgres
	// (`ims serve` runs it).
	Realtime         *realtime.Handler
	RealtimeHub      *pubsub.Hub
	RealtimeListener *pubsub.Listener
	// OAuth is the authorization server behind the MCP connector, and
	// MCPTools the tools built from the OpenAPI document.
	OAuth        *oauth.Handler
	OAuthService *oauth.Service
	MCPTools     []mcp.Tool
	MCPLimiter   ratelimit.Limiter
	// routes is the built router, read by the dashboard's route inspector.
	routes chi.Routes
	DevUI  *webui.Handler
	AppUI  *webui.Handler
}

// NewDependencies constructs the dependency graph for the HTTP API mode. The
// returned cleanup function releases owned resources.
func NewDependencies(cfg *config.Config, logger *slog.Logger) (*Dependencies, func(), error) {
	platform, platformCleanup, err := NewPlatform(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	logger = platform.Logger

	responder := httpx.NewResponder(cfg.App.URL, cfg.App.Env, cfg.App.Debug)
	binder := httpx.NewBinder(cfg.HTTP.MaxRequestBodyBytes, cfg.HTTP.StrictJSONFields)
	validator := httpx.NewValidator()
	kit := httpx.NewKit(binder, validator, responder)

	// Live updates: a hub for this process's streams, fed by one LISTEN
	// connection (started by `ims serve`).
	var realtimeHub *pubsub.Hub
	var realtimeListener *pubsub.Listener
	if cfg.Realtime.Enabled {
		realtimeHub = pubsub.NewHub(cfg.Realtime.MaxStreamsPerUser)
		realtimeListener = pubsub.NewListener(cfg.Realtime.ListenDSN, realtimeHub, logger)
	}
	translator, err := i18n.Load()
	if err != nil {
		platformCleanup()
		return nil, nil, err
	}

	queries := platform.Tx.Queries()
	store := platform.Cache
	checker := platform.Checker

	healthRegistry := health.NewRegistry()
	healthRegistry.Add(health.EnvironmentCheck{Env: cfg.App.Env})
	healthRegistry.Add(health.DebugModeCheck{Env: cfg.App.Env, Debug: cfg.App.Debug})
	healthRegistry.Add(health.ApplicationCheck{NameValue: cfg.App.Name, VersionValue: version.Version})
	healthRegistry.Add(database.HealthCheck{Registry: platform.DB})
	healthRegistry.Add(database.MigrationCheck{Pool: platform.DB.Primary().Pool})
	healthRegistry.Add(cache.HealthCheck{Store: store})
	healthRegistry.Add(queue.HealthCheck{Pool: platform.DB.Primary().Pool})
	healthRegistry.AddAdvisory(scheduler.HealthCheck{Store: store})
	if cfg.Backup.Enabled && platform.Backup != nil {
		healthRegistry.AddAdvisory(backup.HealthCheck{Service: platform.Backup})
	}
	maintenanceChecker := platform.Maintenance.Checker()
	healthHandler := health.NewHandler(healthRegistry, 30*time.Second).
		WithMaintenance(func(r *http.Request) any { return maintenanceBlock(maintenanceChecker.Current(r.Context())) })

	limiter := ratelimit.NewCacheLimiter(store, cfg.RateLimit.PerMinute, time.Minute)

	distFS, err := webdev.Dist()
	if err != nil {
		platformCleanup()
		return nil, nil, err
	}
	devUI := webui.New(http.FS(distFS))

	appFS, err := webapp.Dist()
	if err != nil {
		platformCleanup()
		return nil, nil, err
	}
	appUI := webui.NewClient(http.FS(appFS))

	// Repositories
	identityRepo := platform.IdentityRepo
	roleRepo := access.NewRoleRepository(queries)
	tenantLookup := access.NewTenantLookup(queries)
	permissionRepo := access.NewPermissionRepository(queries)
	branchRepo := branches.NewRepository(queries)
	tenantRepo := tenants.NewRepository(queries)

	// Services
	roleService := access.NewRoleService(roleRepo, permissionRepo, checker, platform.Tx).WithActivity(platform.Activity)
	permissionService := access.NewPermissionService(permissionRepo, checker)
	branchService := branches.NewService(branchRepo, identityRepo, store, cfg.Tenancy, platform.BillingGuard).
		WithTx(platform.Tx).
		WithActivity(platform.Activity)
	tenantService := tenants.NewService(tenantRepo, branchService, roleService, platform.Tx, store, cfg.Tenancy).
		WithActivity(platform.Activity)

	authenticator := platform.Authenticator
	throttle := authn.NewThrottle(store, cfg.Auth.LoginMaxAttempts, cfg.Auth.LoginDecay)
	authService := authn.NewService(identityRepo, authenticator, platform.OTP, throttle, cfg.Auth).
		WithSocial(social.New(cfg.Social)).
		WithAudit(platform.Activity).
		WithSignInWatcher(signInAlerts{settings: platform.Settings, queries: queries, delivery: platform.Delivery, logger: logger}).
		WithTwoFactor(platform.TwoFactor, twoFactorPolicy{settings: platform.Settings, cfg: cfg.Auth, logger: logger})
	userService := platform.Identity
	tenantService.WithOwnership(tenants.OwnershipCollaborators{Passwords: authService, Members: identityRepo, Checker: checker})

	// MCP connector: the authorization server and the tool catalog.
	oauthService := platform.OAuthService()
	oauthPages, err := oauth.NewPages(oauthService, authService, identityRepo, store, cfg.App.Name, "", logger)
	if err != nil {
		platformCleanup()
		return nil, nil, err
	}
	mcpTools, err := mcpCatalog()
	if err != nil {
		platformCleanup()
		return nil, nil, err
	}

	// Handlers and modules
	systemModule := system.NewModule(
		system.NewService(cfg.App.Name, cfg.App.Env, version.Version, cfg.App.Timezone),
		kit,
	)

	var deps *Dependencies
	deps = &Dependencies{
		Config:        cfg,
		Logger:        logger,
		Responder:     responder,
		Binder:        binder,
		Validator:     validator,
		I18n:          translator,
		Health:        healthRegistry,
		HealthHandler: healthHandler,
		Metrics:       metrics.New(),
		Limiter:       limiter,
		DB:            platform.DB,
		Tx:            platform.Tx,
		Cache:         store,
		Checker:       checker,
		Authenticator: authenticator,
		TenancyOpts: tenancy.ResolveOptions{
			Queries:   queries,
			Cache:     store,
			Checker:   checker,
			Responder: responder,
			Audit:     platform.Activity,
		},
		Platform:         platform,
		System:           systemModule,
		Realtime:         realtime.NewHandler(realtimeHub, cfg.Realtime, logger, kit),
		RealtimeHub:      realtimeHub,
		RealtimeListener: realtimeListener,
		Auth: auth.NewHandler(authService, userService, checker, kit).
			WithRecovery(platform.Recovery).
			WithSessions(platform.Sessions),
		Users:       identity.NewHandler(userService, platform.Files, platform.Exchange, checker, kit),
		Privacy:     privacy.NewHandler(platform.Privacy, authService, checker, kit),
		Tenants:     tenants.NewHandler(tenantService, checker, kit).WithSettings(platform.Settings),
		Branches:    branches.NewHandler(branchService, checker, kit),
		Activity:    activity.NewHandler(activity.NewService(activity.NewRepository(queries)), checker, kit),
		Roles:       access.NewRoleHandler(roleService, checker, tenantLookup, kit),
		Permissions: access.NewPermissionHandler(permissionService, checker, kit),
		Files: files.NewHandler(platform.Files, checker, kit).WithLogoSetter(func(ctx context.Context, tenantID int64, url string) error {
			_, err := platform.Settings.SetTenant(ctx, tenancy.Scope{TenantID: tenantID}, "branding.logo_url", url)
			return err
		}),
		Notifications: notifications.NewHandler(platform.Notifications, checker, kit),
		Dashboard:     dashboard.NewHandler(dashboard.NewService(dashboard.NewRepository(queries)), checker, kit),
		Announcements: announcements.NewHandler(platform.Announcements, checker, kit),
		Webhooks:      webhooks.NewHandler(platform.Webhooks, checker, kit),
		Processing:    dataprocessing.NewHandler(platform.Processing, platform.Settings, checker, kit),
		Settings:      settings.NewHandler(platform.Settings, checker, kit),
		Flags:         featureflags.NewHandler(platform.FeatureFlags, checker, kit),
		Dev: dev.NewHandler(dev.Deps{
			Checker:       checker,
			Permissions:   permissionService,
			QueueAdmin:    platform.QueueAdmin,
			Queue:         platform.Queue,
			Playground:    cfg.IsLocal(),
			Actions:       dev.NewActionRepository(queries),
			Telemetry:     dev.NewTelemetryRepository(queries),
			Sessions:      dev.NewSessionRepository(queries),
			Developers:    dev.NewDeveloperRepository(queries),
			Files:         dev.NewFileRepository(queries),
			FileService:   platform.Files,
			Backups:       platform.Backup,
			Targets:       dev.NewTargetRepository(queries),
			FeatureFlags:  platform.FeatureFlags,
			Settings:      platform.Settings,
			Identity:      platform.Identity,
			Exchange:      platform.Exchange,
			Roles:         roleRepo,
			RoleService:   roleService,
			Collector:     platform.Telemetry,
			Logger:        logger,
			RetentionDays: cfg.DevDashboard.RetentionDays,

			AuditRetentionDays: cfg.DevDashboard.AuditRetentionDays,
			Inspector: dev.NewInspectorService(queries, cfg, func() chi.Routes {
				if deps == nil {
					return nil
				}
				return deps.routes
			}),
			Queries:     queries,
			Maintenance: platform.Maintenance,
			Deliveries:  platform.Delivery,
			Health:      healthHandler,
			Pools:       platform.DB,
			Scheduler:   platform.ScheduleRunner,
			Commands:    dev.NewCommandService(queries, logger, devCommands(platform, permissionService, healthRegistry)...),
			Kit:         kit,
		}),
		DevUI:        devUI,
		AppUI:        appUI,
		OAuth:        oauth.NewHandler(oauthService, oauthPages, checker, kit),
		OAuthService: oauthService,
		MCPTools:     mcpTools,
		MCPLimiter:   ratelimit.NewCacheLimiter(store, cfg.MCP.RateLimit, time.Minute),
		Billing: billing.NewHandler(
			platform.BillingPackages,
			platform.BillingSubs,
			platform.BillingPayments,
			platform.BillingApprovals,
			checker,
			kit,
		).WithUsage(billingservice.NewUsageService(platform.BillingGuard, billingdomain.NewReportRepository(queries))).
			WithInvoices(platform.BillingInvoices),
	}
	deps.ActivityExport = activityexport.NewHandler(platform.ActivityExport, checker, kit)
	deps.Onboarding = onboarding.NewHandler(
		onboarding.NewService(onboarding.Deps{
			Repo:      onboarding.NewRepository(queries),
			Tenants:   tenantService,
			Plans:     platform.BillingSubs,
			Packages:  platform.BillingPackages,
			Users:     userService,
			Accounts:  identityRepo,
			Roles:     roleRepo,
			Branches:  branchRepo,
			Settings:  platform.Settings,
			Mailer:    platform.Delivery,
			ClientURL: onboarding.ClientOrigin(cfg.Auth.PasswordResetURL),
			Logger:    logger,
		}).WithActivity(platform.Activity),
		// Sign-up is anonymous and creates rows: ten attempts per IP an hour.
		ratelimit.NewCacheLimiter(store, 10, time.Hour),
		kit,
	)
	deps.Auth.WithSignInRecorder(deps.Dev.Auditor())
	cleanup := func() {
		limiter.Close()
		platformCleanup()
	}
	return deps, cleanup, nil
}

// maintenanceBlock is the "maintenance" object of GET /api/health: whether the
// app is down, since when and the message clients see (never the secret or
// who took it down).
func maintenanceBlock(state maintenance.State) map[string]any {
	block := map[string]any{"down": state.Down}
	if state.Down {
		block["since"] = state.Since
		block["message"] = state.Message
		block["retry_after"] = state.RetryAfter
	}
	return block
}
