// Package config loads the application settings from the environment (.env).
// Like Laravel's config/*.php, each section lives in its own file: app.go,
// database.go, cache.go, queue.go, auth.go, social.go, billing.go, storage.go,
// backup.go, mail.go and ops.go. config.go holds Load and the cross-section
// Validate.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Canonical APP_ENV values. The raw value is trimmed and lowercased once at
// load and validated against this allowlist, so "production ", "Production"
// or a typo like "prodution" can never silently disable production
// hardening.
const (
	EnvLocal       = "local"
	EnvDevelopment = "development"
	EnvDev         = "dev"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config is the fully typed application configuration assembled from
// environment variables (.env + process environment).
type Config struct {
	App          App
	AppKey       AppKey
	HTTP         HTTP
	RateLimit    RateLimit
	Database     Database
	Cache        Cache
	Redis        Redis
	Queue        Queue
	Schedule     Schedule
	Auth         Auth
	Social       Social
	Tenancy      Tenancy
	Billing      Billing
	Storage      Storage
	Files        Files
	Processing   Processing
	Backup       Backup
	Mail         Mail
	SMS          SMS
	Delivery     Delivery
	Dedupe       Dedupe
	Webhooks     Webhooks
	Tracing      Tracing
	Ops          Ops
	Security     Security
	DevDashboard DevDashboard
	Activity     Activity
	MCP          MCP
	Maintenance  Maintenance
}

// IsProduction reports whether the application runs in production.
func (c *Config) IsProduction() bool {
	return c.App.Env == EnvProduction
}

// IsLocal reports whether the app is running on a developer machine. The
// developer Playground is only exposed in local/development environments.
func (c *Config) IsLocal() bool {
	switch c.App.Env {
	case EnvLocal, EnvDevelopment, EnvDev:
		return true
	default:
		return false
	}
}

// IsDebug reports whether verbose/debug behavior is enabled and allowed.
func (c *Config) IsDebug() bool {
	return c.App.Debug && !c.IsProduction()
}

// RunningUnderServiceManager reports whether the process was started by a
// service manager (systemd sets INVOCATION_ID for every unit invocation).
func RunningUnderServiceManager() bool {
	return os.Getenv("INVOCATION_ID") != ""
}

// ValidateDeploymentPosture refuses to serve a development-postured config
// under a service manager (systemd — i.e. a deployment). A dev env ships with
// the dashboard on, docs on, unmasked 5xx details and text logs; booting that
// as a service is almost always a provisioning mistake. Override explicitly
// with APP_ALLOW_LOCAL_UNDER_SYSTEMD=true.
func (c *Config) ValidateDeploymentPosture() error {
	if !c.IsLocal() || !RunningUnderServiceManager() {
		return nil
	}
	allow, err := getBool("APP_ALLOW_LOCAL_UNDER_SYSTEMD", false)
	if err != nil {
		return err
	}
	if allow {
		return nil
	}
	return fmt.Errorf("refusing to serve: APP_ENV=%q is a development posture but the process runs under a service manager; set APP_ENV=%s (or %s) for deployments, or APP_ALLOW_LOCAL_UNDER_SYSTEMD=true to override", c.App.Env, EnvStaging, EnvProduction)
}

// Load resolves the environment file (explicit flag → APP_ENV_FILE → .env),
// loads it without overriding existing process values, then builds and returns
// the typed configuration. A missing default file is not an error.
func Load(envFile string) (*Config, error) {
	resolved, err := resolveEnvFile(envFile)
	if err != nil {
		return nil, err
	}
	if resolved != "" {
		if err := godotenv.Load(resolved); err != nil {
			return nil, fmt.Errorf("loading env file %q: %w", resolved, err)
		}
	}

	cfg := &Config{}
	if err := cfg.loadApp(resolved); err != nil {
		return nil, err
	}
	if err := cfg.loadHTTP(); err != nil {
		return nil, err
	}
	if err := cfg.loadRateLimit(); err != nil {
		return nil, err
	}
	if err := cfg.loadDatabase(); err != nil {
		return nil, err
	}
	if err := cfg.loadCache(); err != nil {
		return nil, err
	}
	if err := cfg.loadRedis(); err != nil {
		return nil, err
	}
	if err := cfg.loadQueue(); err != nil {
		return nil, err
	}
	if err := cfg.loadSchedule(); err != nil {
		return nil, err
	}
	if err := cfg.loadAuth(); err != nil {
		return nil, err
	}
	if err := cfg.loadSocial(); err != nil {
		return nil, err
	}
	if err := cfg.loadTenancy(); err != nil {
		return nil, err
	}
	if err := cfg.loadBilling(); err != nil {
		return nil, err
	}
	if err := cfg.loadStorage(); err != nil {
		return nil, err
	}
	if err := cfg.loadFiles(); err != nil {
		return nil, err
	}
	if err := cfg.loadProcessing(); err != nil {
		return nil, err
	}
	if err := cfg.loadBackup(); err != nil {
		return nil, err
	}
	if err := cfg.loadMail(); err != nil {
		return nil, err
	}
	if err := cfg.loadSMS(); err != nil {
		return nil, err
	}
	if err := cfg.loadDelivery(); err != nil {
		return nil, err
	}
	if err := cfg.loadDedupe(); err != nil {
		return nil, err
	}
	if err := cfg.loadWebhooks(); err != nil {
		return nil, err
	}
	if err := cfg.loadTracing(); err != nil {
		return nil, err
	}
	if err := cfg.loadOps(); err != nil {
		return nil, err
	}
	if err := cfg.loadMaintenance(); err != nil {
		return nil, err
	}
	if err := cfg.loadDevDashboard(); err != nil {
		return nil, err
	}
	if err := cfg.loadActivity(); err != nil {
		return nil, err
	}
	if err := cfg.loadMCP(); err != nil {
		return nil, err
	}
	if err := cfg.loadSecurity(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks the configuration required by long-running commands
// (e.g. serve). Short-lived commands (version, help) call it only when they
// actually need the validated values.
func (c *Config) Validate() error {
	if c.HTTP.ListenAddr == "" {
		return errors.New("APP_LISTEN_ADDR must not be empty")
	}
	if c.HTTP.MaxRequestBodyBytes <= 0 {
		return errors.New("HTTP_MAX_REQUEST_BODY_BYTES must be greater than zero")
	}
	if c.HTTP.RequestTimeout <= 0 {
		return errors.New("HTTP_REQUEST_TIMEOUT must be greater than zero")
	}
	if len(c.App.SupportedLocales) == 0 {
		return errors.New("APP_SUPPORTED_LOCALES must not be empty")
	}
	if !containsFold(c.App.SupportedLocales, c.App.FallbackLocale) {
		return fmt.Errorf("APP_FALLBACK_LOCALE %q must be part of APP_SUPPORTED_LOCALES", c.App.FallbackLocale)
	}
	if c.RateLimit.PerMinute <= 0 {
		return errors.New("RATE_LIMIT_PER_MINUTE must be greater than zero")
	}
	for _, conn := range c.Database.Connections {
		if err := validateConnection(conn); err != nil {
			return err
		}
	}
	switch c.Cache.Driver {
	case "memory", "file", "redis", "database":
	default:
		return fmt.Errorf("CACHE_DRIVER %q is not supported", c.Cache.Driver)
	}
	switch c.Queue.Driver {
	case "river", "sync":
	default:
		return fmt.Errorf("QUEUE_DRIVER %q is not supported", c.Queue.Driver)
	}
	if c.Queue.WorkerConcurrency < 1 {
		return errors.New("QUEUE_WORKER_CONCURRENCY must be at least 1")
	}
	for name, channel := range map[string]Channel{
		"QUEUE_CRITICAL": c.Queue.Critical,
		"QUEUE_DEFAULT":  c.Queue.Default,
		"QUEUE_HEAVY":    c.Queue.Heavy,
	} {
		if channel.Tries < 1 {
			return fmt.Errorf("%s_TRIES must be at least 1", name)
		}
		if channel.Timeout <= 0 {
			return fmt.Errorf("%s_TIMEOUT must be greater than zero", name)
		}
	}
	if _, err := time.LoadLocation(c.Schedule.Timezone); err != nil {
		return fmt.Errorf("SCHEDULE_TIMEZONE %q is invalid: %w", c.Schedule.Timezone, err)
	}
	if c.Auth.LoginMaxAttempts < 1 {
		return errors.New("AUTH_LOGIN_MAX_ATTEMPTS must be at least 1")
	}
	if c.Auth.OTPLength < 4 || c.Auth.OTPLength > 8 {
		return errors.New("AUTH_OTP_LENGTH must be between 4 and 8")
	}
	if c.Auth.BcryptRounds < 4 || c.Auth.BcryptRounds > 31 {
		return errors.New("BCRYPT_ROUNDS must be between 4 and 31")
	}
	if c.Auth.TokenExpiration < 0 {
		return errors.New("AUTH_TOKEN_EXPIRATION must not be negative")
	}
	if err := c.validateAppKey(); err != nil {
		return err
	}
	if c.IsProduction() && c.Auth.TokenExpiration == 0 {
		return errors.New("AUTH_TOKEN_EXPIRATION must be greater than zero in production (non-expiring tokens are not allowed)")
	}
	if err := c.validateStorage(); err != nil {
		return err
	}
	if c.Backup.Dir == "" {
		return errors.New("BACKUP_DIR must not be empty")
	}
	if c.Backup.Timeout <= 0 {
		return errors.New("BACKUP_TIMEOUT must be greater than zero")
	}
	if len(c.Backup.Destinations) == 0 {
		return errors.New("BACKUP_DESTINATIONS must include at least local")
	}
	for _, destination := range c.Backup.Destinations {
		switch destination {
		case "local":
		case "s3":
			if !c.Storage.S3.Configured() {
				return errors.New("BACKUP_DESTINATIONS=s3 requires S3_REGION, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY")
			}
		default:
			return fmt.Errorf("BACKUP_DESTINATIONS %q is not supported (local|s3)", destination)
		}
	}
	if c.Backup.KeepAllDays < 0 || c.Backup.KeepDailyDays < 0 || c.Backup.KeepWeeklyWeeks < 0 ||
		c.Backup.KeepMonthlyMonths < 0 || c.Backup.KeepYearlyYears < 0 {
		return errors.New("BACKUP_KEEP_* values must not be negative")
	}
	if c.Backup.MaxTotalMB < 0 {
		return errors.New("BACKUP_MAX_TOTAL_MB must not be negative")
	}
	switch c.Mail.Driver {
	case "log", "smtp":
	default:
		return fmt.Errorf("MAIL_DRIVER %q is not supported (log|smtp)", c.Mail.Driver)
	}
	if c.Mail.Driver == "smtp" {
		if c.Mail.Host == "" {
			return errors.New("MAIL_HOST must not be empty when MAIL_DRIVER=smtp")
		}
		if c.Mail.Port < 1 || c.Mail.Port > 65535 {
			return errors.New("MAIL_PORT must be between 1 and 65535")
		}
		if c.Mail.FromAddress == "" {
			return errors.New("MAIL_FROM_ADDRESS must not be empty when MAIL_DRIVER=smtp")
		}
	}
	if c.Mail.MaxAttachmentMB < 1 {
		return errors.New("MAIL_MAX_ATTACHMENT_MB must be at least 1")
	}
	switch c.SMS.Driver {
	case "log", "twilio":
	default:
		return fmt.Errorf("SMS_DRIVER %q is not supported (log|twilio)", c.SMS.Driver)
	}
	if c.SMS.Driver == "twilio" && (c.SMS.Twilio.AccountSID == "" || c.SMS.Twilio.AuthToken == "" || c.SMS.Twilio.From == "") {
		return errors.New("TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN and TWILIO_FROM are required when SMS_DRIVER=twilio")
	}
	return nil
}
