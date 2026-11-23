package config

import (
	"fmt"
	"strings"
	"time"
)

// Authentication, password and default-tenant settings (AUTH_*, TENANCY_*).

// Auth holds authentication and password settings.
type Auth struct {
	TokenExpiration    time.Duration
	LoginMaxAttempts   int
	LoginDecay         time.Duration
	CheckIsActive      bool
	CheckEmailVerified bool
	CheckPhoneVerified bool
	OTPEnabled         bool
	OTPLength          int
	OTPExpiry          time.Duration
	DefaultPassword    string
	BcryptRounds       int
	// Account recovery / verification (Phase 18).
	PasswordResetExpiry time.Duration // AUTH_PASSWORD_RESET_EXPIRY (default 60m)
	EmailVerifyExpiry   time.Duration // AUTH_EMAIL_VERIFY_EXPIRY (default 24h)
	// PasswordResetURL / EmailVerifyURL are the client pages the e-mails link
	// to; {token} and {email} are replaced. Empty → APP_URL based defaults.
	PasswordResetURL string
	EmailVerifyURL   string
	// InvitationURL is the client page invitation mails link to ({token},
	// {email}); InvitationTTL is how long a link works (phase 26.5).
	InvitationURL string
	InvitationTTL time.Duration
	// NewUsersVerified marks users created by an admin/import as verified
	// (AUTH_NEW_USERS_VERIFIED, default true). Set false to make them confirm
	// their e-mail address first (a verification mail is sent).
	NewUsersVerified bool
	// SessionIdleDays signs out sessions unused for this many days
	// (AUTH_SESSION_IDLE_DAYS, default 0 = never; expired tokens are always
	// pruned by the auth.prune-tokens task).
	SessionIdleDays int
	// RequireTwoFactorPlatform makes platform accounts (no tenant) turn on
	// two-factor authentication (AUTH_REQUIRE_2FA_PLATFORM, default false);
	// tenants decide for their users with security.require_two_factor.
	RequireTwoFactorPlatform bool
}

// Tenancy holds default tenant/branch seeding settings.
type Tenancy struct {
	DefaultTenantID     int64
	DefaultTenantName   string
	DefaultTenantCode   string
	DefaultTenantSlug   string
	DefaultTenantEmail  string
	DefaultTenantPhone  string
	DefaultTenantActive bool
	DefaultBranchID     int64
	DefaultBranchName   string
}

// DefaultSeedPassword is the well-known default for AUTH_DEFAULT_PASSWORD.
// Seeding with it is refused in production.
const DefaultSeedPassword = "123456"

func (c *Config) loadAuth() error {
	cfg := Auth{
		DefaultPassword: getString("AUTH_DEFAULT_PASSWORD", DefaultSeedPassword),
	}

	var err error
	if cfg.LoginMaxAttempts, err = getInt("AUTH_LOGIN_MAX_ATTEMPTS", 5); err != nil {
		return err
	}
	if cfg.CheckIsActive, err = getBool("AUTH_CHECK_IS_ACTIVE", true); err != nil {
		return err
	}
	if cfg.CheckEmailVerified, err = getBool("AUTH_CHECK_EMAIL_VERIFIED", true); err != nil {
		return err
	}
	if cfg.CheckPhoneVerified, err = getBool("AUTH_CHECK_PHONE_VERIFIED", false); err != nil {
		return err
	}
	if cfg.PasswordResetExpiry, err = getDuration("AUTH_PASSWORD_RESET_EXPIRY", time.Hour); err != nil {
		return err
	}
	if cfg.EmailVerifyExpiry, err = getDuration("AUTH_EMAIL_VERIFY_EXPIRY", 24*time.Hour); err != nil {
		return err
	}
	if cfg.NewUsersVerified, err = getBool("AUTH_NEW_USERS_VERIFIED", true); err != nil {
		return err
	}
	if cfg.InvitationTTL, err = getDuration("AUTH_INVITATION_TTL", 7*24*time.Hour); err != nil {
		return err
	}
	cfg.PasswordResetURL = strings.TrimSpace(getString("AUTH_PASSWORD_RESET_URL", ""))
	cfg.EmailVerifyURL = strings.TrimSpace(getString("AUTH_EMAIL_VERIFY_URL", ""))
	cfg.InvitationURL = strings.TrimSpace(getString("AUTH_INVITATION_URL", ""))
	if cfg.OTPEnabled, err = getBool("AUTH_OTP_ENABLED", false); err != nil {
		return err
	}
	if cfg.OTPLength, err = getInt("AUTH_OTP_LENGTH", 6); err != nil {
		return err
	}
	if cfg.BcryptRounds, err = getInt("BCRYPT_ROUNDS", 12); err != nil {
		return err
	}
	if cfg.SessionIdleDays, err = getInt("AUTH_SESSION_IDLE_DAYS", 0); err != nil {
		return err
	}
	if cfg.RequireTwoFactorPlatform, err = getBool("AUTH_REQUIRE_2FA_PLATFORM", false); err != nil {
		return err
	}
	if cfg.SessionIdleDays < 0 {
		return fmt.Errorf("AUTH_SESSION_IDLE_DAYS must be zero or more, got %d", cfg.SessionIdleDays)
	}

	// 1440 minutes (24 h): a non-zero default so a stolen token cannot live
	// forever; raise via AUTH_TOKEN_EXPIRATION for long-lived device tokens.
	tokenMinutes, err := getInt("AUTH_TOKEN_EXPIRATION", 1440)
	if err != nil {
		return err
	}
	decayMinutes, err := getInt("AUTH_LOGIN_DECAY_MINUTES", 1)
	if err != nil {
		return err
	}
	otpMinutes, err := getInt("AUTH_OTP_EXPIRY_MINUTES", 5)
	if err != nil {
		return err
	}
	cfg.TokenExpiration = time.Duration(tokenMinutes) * time.Minute
	cfg.LoginDecay = time.Duration(decayMinutes) * time.Minute
	cfg.OTPExpiry = time.Duration(otpMinutes) * time.Minute
	if cfg.PasswordResetURL == "" {
		cfg.PasswordResetURL = strings.TrimRight(c.App.URL, "/") + "/reset-password?token={token}&email={email}"
	}
	if cfg.EmailVerifyURL == "" {
		cfg.EmailVerifyURL = strings.TrimRight(c.App.URL, "/") + "/verify-email?token={token}"
	}
	if cfg.InvitationURL == "" {
		cfg.InvitationURL = strings.TrimRight(c.App.URL, "/") + "/accept-invitation?token={token}&email={email}"
	}
	c.Auth = cfg
	return nil
}

func (c *Config) loadTenancy() error {
	tenantID, err := getInt64("DEFAULT_TENANT_ID", 1)
	if err != nil {
		return err
	}
	branchID, err := getInt64("DEFAULT_BRANCH_ID", 1)
	if err != nil {
		return err
	}
	active, err := getBool("DEFAULT_TENANT_IS_ACTIVE", true)
	if err != nil {
		return err
	}
	c.Tenancy = Tenancy{
		DefaultTenantID:     tenantID,
		DefaultTenantName:   getString("DEFAULT_TENANT_NAME", "Main Tenant"),
		DefaultTenantCode:   getString("DEFAULT_TENANT_CODE", "fad58de7366495db4650cfefac2fcd61"),
		DefaultTenantSlug:   getString("DEFAULT_TENANT_SLUG", "main-tenant"),
		DefaultTenantEmail:  getString("DEFAULT_TENANT_EMAIL", "tenant@example.com"),
		DefaultTenantPhone:  getString("DEFAULT_TENANT_PHONE", ""),
		DefaultTenantActive: active,
		DefaultBranchID:     branchID,
		DefaultBranchName:   getString("DEFAULT_BRANCH_NAME", "Default Branch"),
	}
	return nil
}
