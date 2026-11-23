package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Operational surfaces: docs, metrics, CORS, security headers, dev dashboard.

// Ops holds optional operational surfaces (docs UI, metrics) and the CORS
// policy. All are disabled by default in production and config-gated.
type Ops struct {
	DocsEnabled    bool
	MetricsEnabled bool
	// MetricsToken, when set, is required as "Authorization: Bearer <token>"
	// on /metrics. When empty, /metrics only answers loopback requests.
	MetricsToken string
	CORS         CORS
}

// CORS is the cross-origin policy. An empty AllowedOrigins list disables CORS
// handling entirely (no CORS headers are emitted).
type CORS struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         time.Duration
}

// Security holds hardening switches applied by the security-headers middleware.
type Security struct {
	HeadersEnabled bool
}

// DevDashboard holds the developer dashboard gate and telemetry capture
// settings. The whole surface is disabled in production unless explicitly
// enabled.
type DevDashboard struct {
	Enabled       bool
	Capture       DevCapture
	SampleRate    float64
	RetentionDays int
	// AuditRetentionDays keeps the dashboard audit log (dev_actions).
	AuditRetentionDays int
	MaxBodyBytes       int64
	RedactKeys         []string
	Store              string
}

// DevCapture toggles the telemetry capture categories.
type DevCapture struct {
	HTTP  bool
	SQL   bool
	Cache bool
	Queue bool
	Logs  bool
}

// loadOps reads the optional operational surfaces. Docs default to enabled
// outside production (and off in production) unless DOCS_ENABLED is set;
// metrics and CORS are off unless explicitly configured.
func (c *Config) loadOps() error {
	docsDefault := !c.IsProduction()
	docs, err := getBool("DOCS_ENABLED", docsDefault)
	if err != nil {
		return err
	}
	metrics, err := getBool("METRICS_ENABLED", false)
	if err != nil {
		return err
	}
	maxAgeSecs, err := getInt("CORS_MAX_AGE", 600)
	if err != nil {
		return err
	}
	c.Ops = Ops{
		DocsEnabled:    docs,
		MetricsEnabled: metrics,
		MetricsToken:   strings.TrimSpace(getString("METRICS_TOKEN", "")),
		CORS: CORS{
			AllowedOrigins: splitCSV(getString("CORS_ALLOWED_ORIGINS", "")),
			AllowedMethods: splitCSV(getString("CORS_ALLOWED_METHODS", "GET,POST,PATCH,PUT,DELETE,OPTIONS")),
			AllowedHeaders: splitCSV(getString("CORS_ALLOWED_HEADERS",
				"Authorization,Content-Type,Accept,Accept-Language,X-Acting-Tenant,X-Acting-Branch,X-Acting-User,X-Tenant-Code,X-Branch-Id,X-Request-Id,X-Maintenance-Bypass,Idempotency-Key,traceparent,tracestate")),
			MaxAge: time.Duration(maxAgeSecs) * time.Second,
		},
	}
	return nil
}

// loadDevDashboard reads the developer dashboard gate and telemetry capture
// settings. The dashboard defaults to enabled outside production and disabled
// in production, matching the docs/metrics convention.
func (c *Config) loadDevDashboard() error {
	enabled, err := getBool("DEV_DASHBOARD_ENABLED", !c.IsProduction())
	if err != nil {
		return err
	}
	rawSample := getString("DEV_DASHBOARD_SAMPLE_RATE", "1")
	sample, err := strconv.ParseFloat(rawSample, 64)
	if err != nil || sample < 0 || sample > 1 {
		return fmt.Errorf("DEV_DASHBOARD_SAMPLE_RATE: must be a number between 0 and 1")
	}
	retention, err := getInt("DEV_DASHBOARD_RETENTION_DAYS", 7)
	if err != nil {
		return err
	}
	if retention < 1 {
		return errors.New("DEV_DASHBOARD_RETENTION_DAYS must be at least 1")
	}
	auditRetention, err := getInt("DEV_DASHBOARD_AUDIT_RETENTION_DAYS", 180)
	if err != nil {
		return err
	}
	if auditRetention < 1 {
		return errors.New("DEV_DASHBOARD_AUDIT_RETENTION_DAYS must be at least 1")
	}
	maxBody, err := getInt64("DEV_DASHBOARD_MAX_BODY_BYTES", 64*1024)
	if err != nil {
		return err
	}
	if maxBody < 1 {
		return errors.New("DEV_DASHBOARD_MAX_BODY_BYTES must be greater than zero")
	}
	store := strings.ToLower(strings.TrimSpace(getString("DEV_DASHBOARD_STORE", "database")))
	if store == "" {
		store = "database"
	}
	if store != "database" {
		return fmt.Errorf("DEV_DASHBOARD_STORE: %q is not supported (only \"database\")", store)
	}
	c.DevDashboard = DevDashboard{
		Enabled:       enabled,
		Capture:       parseDevCapture(getString("DEV_DASHBOARD_CAPTURE", "http,sql,cache,queue,logs")),
		SampleRate:    sample,
		RetentionDays: retention,
		MaxBodyBytes:  maxBody,

		AuditRetentionDays: auditRetention,
		RedactKeys:         splitCSV(getString("DEV_DASHBOARD_REDACT_KEYS", "")),
		Store:              store,
	}
	return nil
}

func parseDevCapture(raw string) DevCapture {
	var capture DevCapture
	for _, item := range splitCSV(raw) {
		switch strings.ToLower(item) {
		case "http":
			capture.HTTP = true
		case "sql":
			capture.SQL = true
		case "cache":
			capture.Cache = true
		case "queue":
			capture.Queue = true
		case "logs":
			capture.Logs = true
		}
	}
	return capture
}

// loadSecurity reads the hardening switches.
func (c *Config) loadSecurity() error {
	headers, err := getBool("SECURITY_HEADERS_ENABLED", true)
	if err != nil {
		return err
	}
	c.Security = Security{HeadersEnabled: headers}
	return nil
}
