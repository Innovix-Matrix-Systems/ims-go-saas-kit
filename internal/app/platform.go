package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity"
	activityexport "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/activity/export"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/announcements"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/backup"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	billingservice "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/branches"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/currencies"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dev"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/webhooks"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	cachefactory "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache/factory"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/crypto"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/mail"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/maintenance"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/pubsub"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/scheduler"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/storage"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/telemetry"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/version"
)

// Platform is the infrastructure dependency set shared by the HTTP API and the
// CLI modes: database connections, cache store, registries and platform
// services.
type Platform struct {
	Config   *config.Config
	Logger   *slog.Logger
	DB       *database.Registry
	Tx       *database.TxManager
	Cache    cache.Store
	Storage  *storage.Registry
	Queue    queue.Engine
	Recovery *authn.Recovery
	// Sessions lets users list and revoke their own sessions.
	Sessions *authn.Sessions
	// OTP mails one-time sign-in codes; TwoFactor enrols and verifies second
	// factors.
	OTP              *authn.OTPService
	TwoFactor        *authn.TwoFactor
	QueueAdmin       queue.Admin
	QueueRegistry    *queue.Registry
	Telemetry        *telemetry.Collector
	ScheduleRegistry *scheduler.Registry
	ScheduleRunner   *scheduler.Runner
	Checker          *authz.Checker
	Authenticator    authn.Authenticator
	IdentityRepo     identity.Repository
	Identity         *identity.Service
	Mail             mail.Driver
	// Delivery reaches people by mail, SMS and push.
	Delivery *delivery.Service
	// Crypto encrypts values the app must read back, with APP_KEY.
	Crypto        *crypto.Encrypter
	Settings      *settings.Service
	FeatureFlags  *featureflags.Service
	Processing    *dataprocessing.Service
	Files         *files.Service
	Backup        *backup.Service
	Notifications *notifications.Service
	Announcements *announcements.Service
	// Webhooks delivers activity-log events to tenants' endpoints.
	Webhooks       *webhooks.Service
	Activity       *activity.Recorder
	Exchange       *identity.ExchangeService
	ActivityExport *activityexport.Exporter
	// Currencies says how amounts are written and which currency is the
	// default.
	Currencies       *currencies.Service
	BillingPackages  *billingservice.PackageService
	BillingSubs      *billingservice.SubscriptionService
	BillingPayments  *billingservice.PaymentService
	BillingApprovals *billingservice.ApprovalService
	BillingGuard     *billingservice.Guard
	// BillingInvoices builds invoices (HTML and PDF) for payments.
	BillingInvoices *billingservice.InvoiceService
	// Maintenance is maintenance mode: `ims down` / `ims up`,
	// the dashboard toggle, the HTTP guard and the scheduler skip.
	Maintenance *maintenance.Service
	// Privacy erases and exports a user's personal data
	// (docs/design/account-deletion-and-export.md).
	Privacy *privacy.Service
	// Realtime tells open live-update streams that something changed
	// (pubsub.Discard when REALTIME_ENABLED is off).
	Realtime pubsub.Publisher
	// I18n holds the registered languages and their catalogs (the process
	// default for i18n.T outside a request).
	I18n *i18n.Translator
}

// NewPlatform constructs the platform dependencies. Pools connect lazily, so
// this succeeds even when the database is temporarily unavailable.
func NewPlatform(cfg *config.Config, logger *slog.Logger) (_ *Platform, _ func(), err error) {
	// One cleanup owner: every resource registers its closer as soon as it
	// exists, and any error return closes them all (newest first).
	var closers []func()
	closeAll := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	defer func() {
		if err != nil {
			closeAll()
		}
	}()

	translator, err := newTranslator(cfg)
	if err != nil {
		return nil, nil, err
	}

	// OpenTelemetry: a no-op until an OTLP endpoint is set.
	shutdownTracing, err := tracing.Setup(context.Background(), tracing.Settings{
		Enabled: cfg.Tracing.Enabled, ServiceName: cfg.App.Name, Version: version.Version, Environment: cfg.App.Env,
		SampleRatio: cfg.Tracing.SampleRatio, SamplerFromEnv: cfg.Tracing.SamplerFromEnv,
	})
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(ctx) // flush buffered spans
	})
	if cfg.Tracing.Enabled {
		logger = slog.New(tracing.LogHandler(logger.Handler()))
	}

	lazyTracer := telemetry.NewLazyTracer()
	var queryTracer pgx.QueryTracer = lazyTracer
	if cfg.Tracing.Enabled {
		queryTracer = database.Tracers(lazyTracer, tracing.QueryTracer{})
	}
	dbRegistry, err := database.Open(context.Background(), cfg.Database, database.WithTracer(queryTracer))
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, dbRegistry.Close)
	txManager := database.NewTxManager(dbRegistry.Primary())

	store, cacheCleanup, err := cachefactory.New(*cfg, txManager.Queries())
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, cacheCleanup)
	if !cfg.IsLocal() && (cfg.Cache.Driver == "memory" || cfg.Cache.Driver == "file") {
		logger.Warn("per-process cache driver outside local development: permission/tenancy caches, rate limits, duplicate-request protection and the scheduler heartbeat are not shared between processes; use CACHE_DRIVER=database or redis",
			"driver", cfg.Cache.Driver, "env", cfg.App.Env)
	}

	// Telemetry: the collector's sink writes through the same pools it
	// observes, so the tracer attaches lazily once the collector exists.
	redactor := telemetry.NewRedactor(cfg.DevDashboard.RedactKeys)
	collector := telemetry.NewCollector(telemetry.CaptureConfig{
		Enabled:      cfg.DevDashboard.Enabled,
		Capture:      cfg.DevDashboard.Capture,
		SampleRate:   cfg.DevDashboard.SampleRate,
		MaxBodyBytes: cfg.DevDashboard.MaxBodyBytes,
	}, redactor, dev.NewTelemetrySink(txManager.Queries()), logger)
	closers = append(closers, collector.Close)
	lazyTracer.Attach(collector)
	logger = slog.New(telemetry.WrapLogHandler(logger.Handler(), collector))
	store = telemetry.WrapCache(store, collector)

	encrypter, err := crypto.New(cfg.AppKey.Key, cfg.AppKey.Previous...)
	if err != nil {
		return nil, nil, err
	}
	if cfg.AppKey.Development {
		logger.Warn("APP_KEY is not set: using the public local-development key; run `ims key:generate` before storing anything real")
	}

	queueRegistry := queue.NewRegistry()
	scheduleRegistry := scheduler.NewRegistry()
	activityRecorder := activity.NewRecorder(txManager.Queries(), redactor, logger)
	settingsService := settings.NewService(settings.NewRepository(txManager.Queries()), store).
		WithActivity(activityRecorder)
	featureFlagsService := featureflags.NewService(featureflags.NewRepository(txManager.Queries()), store)

	if cfg.DevDashboard.Enabled && cfg.IsLocal() {
		if err := dev.RegisterPlaygroundJob(queueRegistry); err != nil {
			return nil, nil, err
		}
	}

	queueEngine, err := newQueueEngine(cfg, dbRegistry.Primary().Pool, queueRegistry, logger)
	if err != nil {
		return nil, nil, err
	}

	mailDriver, err := mail.New(cfg.Mail, logger, cfg.IsDebug())
	if err != nil {
		return nil, nil, err
	}
	if err := mail.RegisterJob(queueRegistry, mailDriver); err != nil {
		return nil, nil, err
	}

	// Live updates: any process (API, worker, scheduler) publishes; `ims
	// serve` listens and feeds its open streams.
	realtimePublisher := pubsub.Discard
	if cfg.Realtime.Enabled {
		realtimePublisher = pubsub.NewPostgres(txManager.Queries())
	}

	notificationsService := notifications.NewService(
		notifications.NewRepository(txManager.Queries(), txManager),
		store,
		queueEngine,
		settingsService,
		logger,
	).WithPublisher(realtimePublisher)
	announcementService := announcements.NewService(announcements.NewRepository(txManager.Queries()), txManager, logger).
		WithPublisher(realtimePublisher).
		WithNotifier(func(ctx context.Context, tenantID int64, a announcements.Announcement) error {
			return notifyAnnouncement(ctx, notificationsService, tenantID, a)
		})
	if err := notificationsService.RegisterJobs(queueRegistry); err != nil {
		return nil, nil, err
	}

	storageRegistry, err := storage.New(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	filesService := files.NewService(files.NewRepository(txManager.Queries()), storageRegistry, cfg.Files.Disk, txManager, logger).
		WithPublicDisk(cfg.Files.PublicDisk)
	processingService := dataprocessing.NewService(dataprocessing.NewRepository(txManager.Queries())).
		WithFiles(storageRegistry, cfg.Processing).
		WithLogger(logger)
	backupService := backup.NewService(backup.Options{
		Config:     cfg.Backup,
		Repository: backup.NewRepository(txManager.Queries()),
		Disks:      storageRegistry,
		Pool:       dbRegistry.Primary().Pool,
		Queue:      queueEngine,
		DSN:        cfg.Database.Primary.DSN,
		AppVersion: version.Version,
		Logger:     logger,
	})
	if cfg.Backup.Enabled {
		if err := backup.RegisterJob(queueRegistry, backupService); err != nil {
			return nil, nil, err
		}
	}

	// Currencies: how amounts are written; the default is the currency of
	// plan prices and new payments.
	currencyService := currencies.NewService(currencies.NewRepository(txManager.Queries()), store).WithTx(txManager)

	// Billing
	billingPackages := billingdomain.NewPackageRepository(txManager.Queries())
	billingSubscriptions := billingdomain.NewSubscriptionRepository(txManager.Queries())
	billingPayments := billingdomain.NewPaymentRepository(txManager.Queries())
	billingApprovals := billingdomain.NewApprovalRepository(txManager.Queries())
	orchestrator := billing.NewOrchestrator(billingPayments, currencyService).
		Register(billingdomain.MethodBkash, billing.NewBkashDriver(cfg.Billing.Bkash, store)).
		Register(billingdomain.MethodNagad, billing.NagadDriver{}).
		Register(billingdomain.MethodCash, billing.CashDriver{}).
		Register(billingdomain.MethodBankTransfer, billing.BankTransferDriver{})
	billingPackageService := billingservice.NewPackageService(billingPackages).WithTx(txManager)
	billingSubscriptionService := billingservice.NewSubscriptionService(billingSubscriptions, billingPackages, billingPayments, billingApprovals, orchestrator).
		WithNotifier(notificationsService).
		WithCurrencies(currencyService).
		WithTx(txManager).
		WithActivity(activityRecorder)
	billingPaymentService := billingservice.NewPaymentService(billingPayments, currencyService)
	billingApprovalService := billingservice.NewApprovalService(billingApprovals, billingPayments, billingSubscriptions, txManager).
		WithActivity(activityRecorder)
	billingGuard := billingservice.NewGuard(billingSubscriptions, billingPackages)
	filesService.WithStorageLimit(billingGuard)

	deliveryService, err := newDelivery(cfg, translator, delivery.NewRepository(txManager.Queries()), txManager.Queries(), queueEngine, mailDriver,
		settingsService, billingGuard, storageRegistry, encrypter, logger)
	if err != nil {
		return nil, nil, err
	}
	if err := deliveryService.RegisterJob(queueRegistry); err != nil {
		return nil, nil, err
	}
	notificationsService.WithDelivery(deliveryService, billingFeatures{guard: billingGuard}, encrypter,
		strings.TrimRight(cfg.App.URL, "/")+"/api/v1/notification/unsubscribe", cfg.SMS.DailyLimitPerTenant)

	// Invoices are issued under the platform's name (system.name). Payment
	// notices tell staff and the organization about payments, by in-app
	// notification, mail (the receipt carries the invoice PDF) and live update.
	billingInvoices := billingservice.NewInvoiceService(billingdomain.NewReportRepository(txManager.Queries()), func(ctx context.Context) string {
		value, _ := settingsService.Get(ctx, tenancy.Scope{}, "system.name")
		name, _ := value.(string)
		return name
	}).WithCurrencies(currencyService)
	paymentNotices := newBillingNotices(billingNotices{
		notifications: notificationsService,
		invoices:      billingInvoices,
		delivery:      deliveryService,
		disks:         storageRegistry,
		queries:       txManager.Queries(),
		publisher:     realtimePublisher,
		appURL:        strings.TrimRight(cfg.App.URL, "/"),
		logger:        logger,
	})
	billingSubscriptionService.WithPaymentNotifier(paymentNotices)
	billingApprovalService.WithPaymentNotifier(paymentNotices)

	webhooksService := webhooks.NewService(webhooks.NewRepository(txManager.Queries()), encrypter, queueEngine,
		cfg.Webhooks, cfg.App.Name+"-Webhooks/"+version.Version, logger).
		WithFeatures(billingFeatures{guard: billingGuard}).
		WithNotifier(newWebhookNotices(notificationsService, logger)).
		WithActivity(activityRecorder)
	if err := webhooksService.RegisterJob(queueRegistry); err != nil {
		return nil, nil, err
	}
	activityRecorder.WithSink(webhooksService)

	// Identity (shared by HTTP handlers, CLI commands and job handlers).
	identityRepo := identity.NewRepository(txManager.Queries())
	roleRepo := access.NewRoleRepository(txManager.Queries())
	checker := authz.NewChecker(txManager.Queries(), store)
	authenticator := authn.NewOpaqueAuthenticator(txManager.Queries(), identityRepo, cfg.Auth)
	accounts := accountDeliverer{delivery: deliveryService}
	recovery := authn.NewRecovery(identityRepo, identityRepo, authenticator, store, accounts, cfg.Auth, logger).
		WithInvitationAudit(activityRecorder).
		WithEmailChange(identityRepo).
		WithEmailChangeAudit(activityRecorder)
	sessions := authn.NewSessions(txManager.Queries(), authenticator).WithAudit(activityRecorder)
	otpService := authn.NewOTPService(store, cfg.Auth, logger, cfg.IsDebug()).WithDeliverer(accounts)
	twoFactor := authn.NewTwoFactor(txManager.Queries(), encrypter, otpService, cfg.App.Name).
		WithDeliverer(accounts).
		WithAudit(activityRecorder)
	identityService := identity.NewService(identityRepo, roleRepo, branches.NewRepository(txManager.Queries()), checker, authenticator, cfg.Auth, billingGuard).
		WithTx(txManager).
		WithVerifier(recovery).
		WithActivity(activityRecorder).
		WithTwoFactor(twoFactor)
	exchangeService := identity.NewExchangeService(identityService, identityRepo, roleRepo, processingService, storageRegistry, queueEngine, cfg, logger)
	if err := exchangeService.RegisterJobs(queueRegistry); err != nil {
		return nil, nil, err
	}
	activityExport := activityexport.New(activity.NewService(activity.NewRepository(txManager.Queries())), processingService, storageRegistry, cfg.Processing,
		queueEngine, activityexport.NewRepository(txManager.Queries()), logger)
	if err := activityExport.RegisterJobs(queueRegistry); err != nil {
		return nil, nil, err
	}

	privacyService, err := newPrivacy(cfg, txManager, storageRegistry, checker, authenticator, activityRecorder,
		settingsService, deliveryService, processingService, queueEngine, queueRegistry, logger)
	if err != nil {
		return nil, nil, err
	}

	runner, err := scheduler.NewRunner(scheduleRegistry, logger, store, cfg.Schedule.Timezone)
	if err != nil {
		return nil, nil, err
	}

	maintenanceStore := maintenance.NewStore(txManager.Queries())
	maintenanceService := maintenance.NewService(maintenanceStore,
		maintenance.NewChecker(maintenanceStore, cfg.Maintenance.CheckInterval, logger)).
		WithAudit(activityRecorder)
	runner.WithMaintenance(maintenanceService.Checker().IsDown).
		WithRunLog(scheduler.NewRunLog(txManager.Queries()))

	platform := &Platform{
		I18n:             translator,
		Activity:         activityRecorder,
		Config:           cfg,
		Logger:           logger,
		DB:               dbRegistry,
		Tx:               txManager,
		Cache:            store,
		Storage:          storageRegistry,
		Queue:            queueEngine,
		QueueRegistry:    queueRegistry,
		Telemetry:        collector,
		ScheduleRegistry: scheduleRegistry,
		ScheduleRunner:   runner,
		Checker:          checker,
		Authenticator:    authenticator,
		Recovery:         recovery,
		Sessions:         sessions,
		OTP:              otpService,
		TwoFactor:        twoFactor,
		IdentityRepo:     identityRepo,
		Identity:         identityService,
		Exchange:         exchangeService,
		ActivityExport:   activityExport,
		Mail:             mailDriver,
		Delivery:         deliveryService,
		Crypto:           encrypter,
		Settings:         settingsService,
		FeatureFlags:     featureFlagsService,
		Processing:       processingService,
		Files:            filesService,
		Backup:           backupService,
		Notifications:    notificationsService,
		Announcements:    announcementService.WithActivity(activityRecorder),
		Webhooks:         webhooksService,
		Currencies:       currencyService,
		BillingPackages:  billingPackageService,
		BillingSubs:      billingSubscriptionService,
		BillingPayments:  billingPaymentService,
		BillingApprovals: billingApprovalService,
		BillingGuard:     billingGuard,
		BillingInvoices:  billingInvoices,
		Maintenance:      maintenanceService,
		Privacy:          privacyService,
		Realtime:         realtimePublisher,
	}
	if riverEngine, ok := queueEngine.(*queue.RiverEngine); ok {
		riverEngine.SetObserver(collector)
		platform.QueueAdmin = riverEngine.Admin()
	}
	for _, task := range platform.schedule() {
		if err := scheduleRegistry.Register(task); err != nil {
			return nil, nil, err
		}
	}
	return platform, closeAll, nil
}

// NewQueueEngine selects the configured queue engine.
func (p *Platform) NewQueueEngine(logger *slog.Logger) (queue.Engine, error) {
	engine, err := newQueueEngine(p.Config, p.DB.Primary().Pool, p.QueueRegistry, logger)
	if err != nil {
		return nil, err
	}
	if riverEngine, ok := engine.(*queue.RiverEngine); ok {
		riverEngine.SetObserver(p.Telemetry)
	}
	return engine, nil
}

func newQueueEngine(cfg *config.Config, pool *pgxpool.Pool, registry *queue.Registry, logger *slog.Logger) (queue.Engine, error) {
	switch cfg.Queue.Driver {
	case "sync":
		return queue.NewSyncEngine(registry), nil
	case "river":
		return queue.NewRiverEngine(pool, registry, cfg.Queue, logger, nil)
	default:
		return nil, fmt.Errorf("queue: unsupported driver %q", cfg.Queue.Driver)
	}
}
