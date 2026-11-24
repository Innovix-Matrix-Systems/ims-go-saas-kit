package authn

import (
	"context"
	"math"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn/social"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
)

// Login status values returned by the login flow.
const (
	StatusAuthSuccess = "AUTH_SUCCESS"
	StatusOTPRequired = "OTP_REQUIRED"
)

// MethodPassword is the LoginEvent method of a password login; a social
// login uses the provider's name ("google").
const MethodPassword = "password"

// LoginResult is the outcome of a successful login or OTP dispatch.
type LoginResult struct {
	User    *UserRecord
	Status  string
	Message string
	Token   *Token
	// Method is the second factor an OTP_REQUIRED sign-in waits for: totp
	// (an authenticator app) or email (a mailed code).
	Method string
}

// LoginEvent is one API login attempt, for the activity log.
type LoginEvent struct {
	Method string // MethodPassword or a social provider name
	Email  string
	Device string
	User   *UserRecord // the signed-in user; nil when the attempt failed
	Err    error
}

// LoginAudit records login attempts. The activity module implements it;
// authn only decides when to call it.
type LoginAudit interface {
	Login(ctx context.Context, event LoginEvent)
}

// SignInWatcher hears about every successful sign-in before it is recorded
// (the new sign-in alert, implemented in internal/app). It must not block
// or fail the sign-in.
type SignInWatcher interface {
	SignedIn(ctx context.Context, user *UserRecord, device string)
}

// Service implements the login, OTP verification and logout workflows.
type Service struct {
	users     UserLookup
	auth      Authenticator
	otp       *OTPService
	throttle  *Throttle
	cfg       config.Auth
	dummyHash []byte
	social    *social.Registry
	audit     LoginAudit
	watcher   SignInWatcher
	twoFactor *TwoFactor
	policy    TwoFactorPolicy
}

// NewService creates the authentication workflow service.
func NewService(users UserLookup, auth Authenticator, otp *OTPService, throttle *Throttle, cfg config.Auth) *Service {
	// A throwaway hash at the configured cost: comparing against it on the
	// unknown-email path equalises timing with the wrong-password path.
	dummy, _ := bcrypt.GenerateFromPassword([]byte("ims-go-admin-starter:dummy:credentials"), cfg.BcryptRounds)
	return &Service{users: users, auth: auth, otp: otp, throttle: throttle, cfg: cfg, dummyHash: dummy}
}

// WithSocial enables social login with the given providers.
func (s *Service) WithSocial(registry *social.Registry) *Service {
	s.social = registry
	return s
}

// WithAudit records every finished login attempt (password, OTP and social).
func (s *Service) WithAudit(audit LoginAudit) *Service {
	s.audit = audit
	return s
}

// WithSignInWatcher reports successful sign-ins (the new sign-in alert).
func (s *Service) WithSignInWatcher(watcher SignInWatcher) *Service {
	s.watcher = watcher
	return s
}

// Login authenticates credentials and issues a device token (or dispatches an
// OTP when enabled).
//
// The response for "unknown email" and "wrong password" is identical in
// status, body and (via the dummy hash) timing; account-status checks run
// only after the password matches, so login cannot be used to enumerate
// accounts or their state.
func (s *Service) Login(ctx context.Context, email, password, device, ip string) (result *LoginResult, err error) {
	ctx, end := tracing.Begin(ctx, "authn.login")
	defer end(&err)
	defer func() { s.recordLogin(ctx, MethodPassword, email, device, result, err) }()

	record, err := s.checkPassword(ctx, email, password, ip)
	if err != nil {
		return nil, err
	}
	if challenge, err := s.challenge(ctx, record, false); err != nil || challenge != nil {
		return challenge, err
	}
	return s.completeLogin(ctx, record, device, email, ip)
}

// SignIn checks credentials like Login, with the same throttles and account
// checks, but issues no token: the OAuth sign-in page (MCP connectors) uses
// it and then issues its own. otpRequired is true when an OTP was sent and
// SignInOTP must follow.
func (s *Service) SignIn(ctx context.Context, email, password, ip string) (record *UserRecord, otpRequired bool, err error) {
	ctx, end := tracing.Begin(ctx, "authn.sign_in")
	defer end(&err)
	record, err = s.checkPassword(ctx, email, password, ip)
	if err != nil {
		return nil, false, err
	}
	if challenge, err := s.challenge(ctx, record, false); err != nil {
		return nil, false, err
	} else if challenge != nil {
		return record, true, nil
	}
	s.clearThrottles(ctx, email, ip)
	return record, false, nil
}

// SignInOTP completes SignIn when an OTP was required. It issues no token.
func (s *Service) SignInOTP(ctx context.Context, email, code, ip string) (_ *UserRecord, err error) {
	ctx, end := tracing.Begin(ctx, "authn.sign_in_otp")
	defer end(&err)
	record, err := s.checkOTP(ctx, email, code, ip)
	if err != nil {
		return nil, err
	}
	s.clearThrottles(ctx, email, ip)
	return record, nil
}

// OTPEnabled reports whether sign-in asks for a one-time code.
func (s *Service) OTPEnabled() bool { return s.cfg.OTPEnabled }

func (s *Service) checkPassword(ctx context.Context, email, password, ip string) (*UserRecord, error) {
	if err := s.checkThrottles(ctx, email, ip); err != nil {
		return nil, err
	}

	record, err := s.users.FindByEmail(ctx, email)
	if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}
	if record == nil || bcrypt.CompareHashAndPassword([]byte(record.Password), []byte(password)) != nil {
		if record == nil {
			_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		}
		s.hitThrottles(ctx, email, ip)
		return nil, apperr.New(apperr.InvalidCredentials, "The provided credentials are incorrect.")
	}
	if err := s.verifyBeforeLogin(record); err != nil {
		return nil, err
	}
	return record, nil
}

// VerifyOTP completes a login started with an OTP challenge.
func (s *Service) VerifyOTP(ctx context.Context, email, code, device, ip string) (result *LoginResult, err error) {
	ctx, end := tracing.Begin(ctx, "authn.verify_otp")
	defer end(&err)
	defer func() { s.recordLogin(ctx, MethodPassword, email, device, result, err) }()

	record, err := s.checkOTP(ctx, email, code, ip)
	if err != nil {
		return nil, err
	}
	return s.completeLogin(ctx, record, device, email, ip)
}

func (s *Service) checkOTP(ctx context.Context, email, code, ip string) (*UserRecord, error) {
	if err := s.checkThrottles(ctx, email, ip); err != nil {
		return nil, err
	}

	record, err := s.users.FindByEmail(ctx, email)
	if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, err
	}
	if record == nil {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(code))
		s.hitThrottles(ctx, email, ip)
		return nil, apperr.New(apperr.InvalidCredentials, "The provided credentials are incorrect.")
	}
	if err := s.verifyBeforeLogin(record); err != nil {
		return nil, err
	}

	if err := s.verifySecondFactor(ctx, record, code); err != nil {
		s.hitThrottles(ctx, email, ip)
		return nil, err
	}
	return record, nil
}

// SocialProviders returns the enabled social login providers.
func (s *Service) SocialProviders() []social.Provider {
	return s.social.List()
}

// SocialLogin signs in the user an outside provider vouches for. The
// provider proves the e-mail address; everything after that is ours and the
// same as a password login: the account checks and a device token. Tenant,
// branch and roles come from our user record, as always. There is no
// AUTH_OTP_ENABLED step (the provider has already authenticated the user),
// but a user with two-factor turned on is asked for it: the provider proves
// an e-mail address, not the second factor.
//
// Failed attempts count against the IP only: there is no account to key the
// per-account counter on until the provider has answered, and a provider's
// proof cannot be guessed like a password. A failure on our side or the
// provider's (5xx) does not count, so an outage cannot lock out an office
// behind one IP.
func (s *Service) SocialLogin(ctx context.Context, provider string, proof social.Proof, device, ip string) (result *LoginResult, err error) {
	ctx, end := tracing.Begin(ctx, "authn.social_login", attribute.String("auth.provider", provider))
	defer end(&err)
	p, ok := s.social.Get(provider)
	if !ok {
		return nil, apperr.New(apperr.ResourceNotFound, "This sign-in method is not available.")
	}
	var email string
	defer func() { s.recordLogin(ctx, p.Name(), email, device, result, err) }()

	if err := s.checkThrottle(ctx, ipThrottleKey(ip)); err != nil {
		return nil, err
	}
	record, email, err := s.socialUser(ctx, p, proof)
	if err != nil {
		if apperr.StatusFor(apperr.CodeOf(err)) < 500 {
			_ = s.throttle.Hit(ctx, ipThrottleKey(ip))
		}
		return nil, err
	}
	if err := s.verifyBeforeLogin(record); err != nil {
		return nil, err
	}
	if challenge, err := s.challenge(ctx, record, true); err != nil || challenge != nil {
		return challenge, err
	}
	return s.completeLogin(ctx, record, device, email, ip)
}

// socialUser asks the provider who signed in and finds our user with that
// address. It returns the address even when no user has it, for the log.
func (s *Service) socialUser(ctx context.Context, p social.Provider, proof social.Proof) (*UserRecord, string, error) {
	identity, err := p.Verify(ctx, proof)
	if err != nil {
		return nil, "", err
	}
	// Never sign in on an address the provider has not verified: anyone can
	// open an account there with someone else's address.
	if identity.Email == "" || !identity.EmailVerified {
		return nil, identity.Email, apperr.New(apperr.AccountUnverified, p.DisplayName()+" has not verified the e-mail address of this account.")
	}
	record, err := s.users.FindByEmail(ctx, identity.Email)
	if err != nil && !apperr.Is(err, apperr.ResourceNotFound) {
		return nil, identity.Email, err
	}
	if record == nil {
		return nil, identity.Email, apperr.New(apperr.SocialAccountNotFound, "No account uses this e-mail address. Ask your administrator for access.")
	}
	return record, identity.Email, nil
}

// recordLogin reports a finished attempt: a token issued, or an error. An
// OTP challenge is not finished yet; VerifyOTP reports it.
func (s *Service) recordLogin(ctx context.Context, method, email, device string, result *LoginResult, err error) {
	if s.audit == nil || (err == nil && result.Token == nil) {
		return
	}
	event := LoginEvent{Method: method, Email: email, Device: device, Err: err}
	if err == nil {
		event.User = result.User
	}
	s.audit.Login(ctx, event)
}

// Logout revokes the caller's token for one device.
func (s *Service) Logout(ctx context.Context, userID int64, device string) (err error) {
	ctx, end := tracing.Begin(ctx, "authn.logout", attribute.Int64("user.id", userID))
	defer end(&err)
	return s.auth.Revoke(ctx, userID, device)
}

func (s *Service) completeLogin(ctx context.Context, record *UserRecord, device, email, ip string) (*LoginResult, error) {
	token, status, err := s.issueFor(ctx, record, device)
	if err != nil {
		return nil, err
	}
	s.clearThrottles(ctx, email, ip)
	if err := s.users.TouchLogin(ctx, record.ID, device); err != nil {
		return nil, err
	}
	if s.watcher != nil {
		s.watcher.SignedIn(ctx, record, device)
	}
	result := &LoginResult{User: record, Status: status, Message: "Login is successful.", Token: &token}
	if status == StatusTwoFactorSetup {
		result.Message = "Two-factor authentication is required for this account. Turn it on to continue."
	}
	return result, nil
}

// Throttling keeps two independent counters — one per account, one per IP —
// and locks on either. IP rotation no longer resets a targeted account's
// budget, and password spraying one account from many IPs is bounded by the
// per-IP counter.
func (s *Service) checkThrottles(ctx context.Context, email, ip string) error {
	if err := s.checkThrottle(ctx, accountThrottleKey(email)); err != nil {
		return err
	}
	return s.checkThrottle(ctx, ipThrottleKey(ip))
}

func (s *Service) checkThrottle(ctx context.Context, key string) error {
	tooMany, retryAfter, err := s.throttle.TooMany(ctx, key)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "check login attempts", err)
	}
	if !tooMany {
		return nil
	}
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return apperr.New(apperr.AccountLocked, "Too many login attempts. Please try again later.").WithRetryAfter(seconds)
}

func (s *Service) hitThrottles(ctx context.Context, email, ip string) {
	_ = s.throttle.Hit(ctx, accountThrottleKey(email))
	_ = s.throttle.Hit(ctx, ipThrottleKey(ip))
}

func (s *Service) clearThrottles(ctx context.Context, email, ip string) {
	_ = s.throttle.Clear(ctx, accountThrottleKey(email))
	_ = s.throttle.Clear(ctx, ipThrottleKey(ip))
}

func (s *Service) verifyBeforeLogin(record *UserRecord) error {
	if s.cfg.CheckIsActive && !record.IsActive {
		return apperr.New(apperr.AccountDeactivated, "Your account is deactivated.")
	}
	if record.TenantID != 0 && !record.TenantActive {
		return ErrTenantSuspended()
	}
	if s.cfg.CheckEmailVerified && !record.EmailVerified {
		return apperr.New(apperr.AccountUnverified, "Your email address is not verified.")
	}
	if s.cfg.CheckPhoneVerified && !record.PhoneVerified {
		return apperr.New(apperr.AccountUnverified, "Your phone number is not verified.")
	}
	return nil
}

func accountThrottleKey(email string) string {
	return "login:acct:" + strings.ToLower(strings.TrimSpace(email))
}

func ipThrottleKey(ip string) string {
	return "login:ip:" + ip
}
