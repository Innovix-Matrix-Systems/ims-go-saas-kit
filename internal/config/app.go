package config

import "time"

// App, HTTP server and rate-limit settings (APP_*, HTTP_*, RATE_LIMIT_*).

// App holds application-level settings.
type App struct {
	Name             string
	Env              string
	Debug            bool
	URL              string
	Timezone         string
	Locale           string
	FallbackLocale   string
	SupportedLocales []string
	DataDir          string
	StorageDir       string
	CacheDir         string
	LogDir           string
	LogLevel         string
	EnvFile          string
}

// HTTP holds HTTP server and transport settings.
type HTTP struct {
	ListenAddr          string
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	ShutdownTimeout     time.Duration
	RequestTimeout      time.Duration
	MaxRequestBodyBytes int64
	StrictJSONFields    bool
	TrustedProxies      []string
}

// RateLimit holds the global request-rate limiting settings.
type RateLimit struct {
	PerMinute int
}

func (c *Config) loadApp(envFile string) error {
	env, err := normalizeEnv(getString("APP_ENV", "local"))
	if err != nil {
		return err
	}
	c.App = App{
		Name:             getString("APP_NAME", "ims-go-admin-starter"),
		Env:              env,
		URL:              getString("APP_URL", "http://localhost:8080"),
		Timezone:         getString("APP_TIMEZONE", "UTC"),
		Locale:           getString("APP_LOCALE", "en"),
		FallbackLocale:   getString("APP_FALLBACK_LOCALE", "en"),
		SupportedLocales: splitCSV(getString("APP_SUPPORTED_LOCALES", "en")),
		DataDir:          getString("APP_DATA_DIR", "./var"),
		StorageDir:       getString("APP_STORAGE_DIR", "./var/storage"),
		CacheDir:         getString("APP_CACHE_DIR", "./var/cache"),
		LogDir:           getString("APP_LOG_DIR", ""),
		LogLevel:         getString("APP_LOG_LEVEL", "info"),
		EnvFile:          envFile,
	}
	debug, err := getBool("APP_DEBUG", false)
	if err != nil {
		return err
	}
	c.App.Debug = debug
	return c.loadAppKey()
}

func (c *Config) loadHTTP() error {
	c.HTTP.ListenAddr = getString("APP_LISTEN_ADDR", ":8080")

	var err error
	if c.HTTP.ReadTimeout, err = getDuration("HTTP_READ_TIMEOUT", 15*time.Second); err != nil {
		return err
	}
	if c.HTTP.WriteTimeout, err = getDuration("HTTP_WRITE_TIMEOUT", 30*time.Second); err != nil {
		return err
	}
	if c.HTTP.IdleTimeout, err = getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second); err != nil {
		return err
	}
	if c.HTTP.ShutdownTimeout, err = getDuration("HTTP_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return err
	}
	if c.HTTP.RequestTimeout, err = getDuration("HTTP_REQUEST_TIMEOUT", 60*time.Second); err != nil {
		return err
	}
	// The server write deadline must outlive the request timeout, otherwise
	// the connection is cut before the Timeout middleware can write its 503
	// and long downloads break mid-body (H23). 0 keeps "no write deadline".
	if c.HTTP.WriteTimeout > 0 && c.HTTP.WriteTimeout <= c.HTTP.RequestTimeout {
		c.HTTP.WriteTimeout = c.HTTP.RequestTimeout + 30*time.Second
	}
	if c.HTTP.MaxRequestBodyBytes, err = getInt64("HTTP_MAX_REQUEST_BODY_BYTES", 10*1024*1024); err != nil {
		return err
	}
	if c.HTTP.StrictJSONFields, err = getBool("HTTP_STRICT_JSON_FIELDS", true); err != nil {
		return err
	}
	c.HTTP.TrustedProxies = splitCSV(getString("HTTP_TRUSTED_PROXIES", ""))
	return nil
}

func (c *Config) loadRateLimit() error {
	perMinute, err := getInt("RATE_LIMIT_PER_MINUTE", 1000)
	if err != nil {
		return err
	}
	c.RateLimit.PerMinute = perMinute
	return nil
}
