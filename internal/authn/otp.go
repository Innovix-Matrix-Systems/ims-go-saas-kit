package authn

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// AccountMessage is a message authn sends to a user: a one-time code or a
// recovery link, rendered from a delivery template.
type AccountMessage struct {
	User     *UserRecord
	Template string
	Data     map[string]any
}

// Deliverer sends account messages through the delivery system. It is
// implemented in the composition root (internal/app), because the delivery
// package depends on the queue, which depends on tenancy and so on authn.
type Deliverer interface {
	DeliverAccount(ctx context.Context, message AccountMessage) error
}

// Delivery templates used by authn (internal/platform/delivery/templates).
const (
	TemplateOTPCode       = "otp-code"
	TemplatePasswordReset = "password-reset"
	TemplateEmailVerify   = "email-verify"
)

// OTPService stores short-lived one-time codes in the cache and delivers them
// through the delivery system when a deliverer is attached.
type OTPService struct {
	store     cache.Store
	cfg       config.Auth
	logger    *slog.Logger
	debug     bool
	deliverer Deliverer
}

// NewOTPService creates the OTP service. When debug is true the generated code
// is included in logs for local development.
func NewOTPService(store cache.Store, cfg config.Auth, logger *slog.Logger, debug bool) *OTPService {
	return &OTPService{store: store, cfg: cfg, logger: logger, debug: debug}
}

// WithDeliverer mails OTPs through the delivery system (template otp-code).
func (s *OTPService) WithDeliverer(deliverer Deliverer) *OTPService {
	s.deliverer = deliverer
	return s
}

// OTPKey namespaces codes by user.
func OTPKey(userID int64) string {
	return fmt.Sprintf("otp:user:%d", userID)
}

// Send generates a fresh OTP for user, stores it with the configured expiry
// and mails it. The code is returned for tests and debug logging.
func (s *OTPService) Send(ctx context.Context, user *UserRecord) (_ string, err error) {
	ctx, end := tracing.Begin(ctx, "authn.send_otp")
	defer end(&err)
	return s.sendTo(ctx, user, OTPKey(user.ID))
}

// Verify compares the provided code in constant time and clears it on success.
func (s *OTPService) Verify(ctx context.Context, userID int64, code string) (err error) {
	ctx, end := tracing.Begin(ctx, "authn.check_otp", attribute.Int64("user.id", userID))
	defer end(&err)
	return s.verifyAt(ctx, OTPKey(userID), code)
}

// sendTo stores a fresh code under key and mails it (template otp-code).
// Two-factor enrolment uses its own key, so a sign-in code and an
// enrolment code never stand in for each other.
func (s *OTPService) sendTo(ctx context.Context, user *UserRecord, key string) (string, error) {
	code, err := randomDigits(s.cfg.OTPLength)
	if err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "generate otp", err)
	}
	if err := s.store.Set(ctx, key, code, s.cfg.OTPExpiry); err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "store otp", err)
	}

	if s.deliverer != nil && user.Email != "" {
		err := s.deliverer.DeliverAccount(ctx, AccountMessage{
			User:     user,
			Template: TemplateOTPCode,
			Data:     map[string]any{"code": code, "minutes": int(s.cfg.OTPExpiry / time.Minute)},
		})
		if err != nil {
			return "", apperr.Wrap(apperr.ServiceUnavailable, "The OTP could not be sent.", err)
		}
	}

	attrs := []any{"user_id", user.ID, "expires_in", s.cfg.OTPExpiry.String()}
	if s.debug {
		attrs = append(attrs, "code", code)
	}
	s.logger.Info("otp generated", attrs...)
	return code, nil
}

func (s *OTPService) verifyAt(ctx context.Context, key, code string) error {
	var expected string
	found, err := s.store.Get(ctx, key, &expected)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "read otp", err)
	}
	if !found {
		return apperr.New(apperr.OTPExpired, "The OTP code has expired.")
	}
	if subtle.ConstantTimeCompare([]byte(expected), []byte(strings.TrimSpace(code))) != 1 {
		return apperr.New(apperr.OTPInvalid, "The OTP code is invalid.")
	}
	_ = s.store.Delete(ctx, key)
	return nil
}

// Clear removes any stored code.
func (s *OTPService) Clear(ctx context.Context, userID int64) {
	_ = s.store.Delete(ctx, OTPKey(userID))
}

func randomDigits(length int) (string, error) {
	max := big.NewInt(10)
	digits := make([]byte, 0, length)
	for len(digits) < length {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		digits = append(digits, byte('0'+n.Int64()))
	}
	if digits[0] == '0' {
		digits[0] = '1'
	}
	return string(digits), nil
}
