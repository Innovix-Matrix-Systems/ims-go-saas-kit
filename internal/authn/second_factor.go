package authn

import (
	"context"
	"fmt"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

// StatusTwoFactorSetup is a sign-in that succeeded with a setup-only token:
// the user must turn on two-factor authentication before working.
const StatusTwoFactorSetup = "TWO_FACTOR_SETUP_REQUIRED"

// TwoFactorPolicy decides whether a user must have two-factor authentication
// (the security.require_two_factor setting and AUTH_REQUIRE_2FA_PLATFORM,
// implemented in internal/app).
type TwoFactorPolicy interface {
	Required(ctx context.Context, user *UserRecord) bool
}

// WithTwoFactor asks enrolled users for their second factor at sign-in, and
// with a policy gives users who must enrol a setup-only token.
func (s *Service) WithTwoFactor(twoFactor *TwoFactor, policy TwoFactorPolicy) *Service {
	s.twoFactor, s.policy = twoFactor, policy
	return s
}

// TwoFactor returns the two-factor service (nil when not configured).
func (s *Service) TwoFactor() *TwoFactor { return s.twoFactor }

// challenge returns the second step a sign-in still needs after the
// password (or a provider) passed, or nil when it needs none:
//
//   - two-factor by app: OTP_REQUIRED, method totp;
//   - two-factor by e-mail: a code is mailed; OTP_REQUIRED, method email;
//   - otherwise AUTH_OTP_ENABLED mails a code to every password sign-in
//     (social logins are already authenticated by the provider).
//
// A pending marker binds the code check to this sign-in: without it, a TOTP
// code alone (no password) would sign someone in.
func (s *Service) challenge(ctx context.Context, record *UserRecord, social bool) (*LoginResult, error) {
	method, err := s.twoFactorMethod(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	result := &LoginResult{User: record, Status: StatusOTPRequired}
	switch {
	case method == TwoFactorTOTP:
		result.Method, result.Message = TwoFactorTOTP, "Enter the code from your authenticator app."
	case method == TwoFactorEmail:
		if err := s.twoFactor.SendSignInCode(ctx, record); err != nil {
			return nil, err
		}
		result.Method, result.Message = TwoFactorEmail, "A sign-in code has been sent to your e-mail address."
	case s.cfg.OTPEnabled && !social:
		if _, err := s.otp.Send(ctx, record); err != nil {
			return nil, err
		}
		result.Method, result.Message = TwoFactorEmail, "An OTP has been sent to your registered contact."
		return result, nil
	default:
		return nil, nil
	}
	if err := s.otp.store.Set(ctx, pendingKey(record.ID), true, s.cfg.OTPExpiry); err != nil {
		return nil, err
	}
	return result, nil
}

// verifySecondFactor checks the code of a sign-in challenge: the user's
// two-factor (app code, mailed code or recovery code) when enrolled, the
// AUTH_OTP_ENABLED code otherwise.
func (s *Service) verifySecondFactor(ctx context.Context, record *UserRecord, code string) error {
	method, err := s.twoFactorMethod(ctx, record.ID)
	if err != nil {
		return err
	}
	if method == "" {
		return s.otp.Verify(ctx, record.ID, code)
	}
	var pending bool
	if found, err := s.otp.store.Get(ctx, pendingKey(record.ID), &pending); err != nil || !found {
		return errSignInAgain()
	}
	if err := s.twoFactor.Verify(ctx, record, code); err != nil {
		return err
	}
	_ = s.otp.store.Delete(ctx, pendingKey(record.ID))
	return nil
}

// issueFor issues the sign-in's token: a setup-only one when the policy
// requires two-factor and the user has not turned it on.
func (s *Service) issueFor(ctx context.Context, record *UserRecord, device string) (Token, string, error) {
	if s.policy != nil && s.policy.Required(ctx, record) {
		method, err := s.twoFactorMethod(ctx, record.ID)
		if err != nil {
			return Token{}, "", err
		}
		if method == "" {
			token, err := s.auth.IssueSetup(ctx, record.ID, device)
			return token, StatusTwoFactorSetup, err
		}
	}
	token, err := s.auth.Issue(ctx, record.ID, device)
	return token, StatusAuthSuccess, err
}

func (s *Service) twoFactorMethod(ctx context.Context, userID int64) (string, error) {
	if s.twoFactor == nil {
		return "", nil
	}
	return s.twoFactor.Method(ctx, userID)
}

func pendingKey(userID int64) string { return fmt.Sprintf("2fa:pending:%d", userID) }

// errSignInAgain is a code check with no sign-in waiting for it.
func errSignInAgain() error {
	return apperr.New(apperr.OTPExpired, "Sign in again to get a new code.")
}

// TwoFactorRequired reports whether the policy requires two-factor for a
// user (false without a policy).
func (s *Service) TwoFactorRequired(ctx context.Context, user *UserRecord) bool {
	return s.policy != nil && s.policy.Required(ctx, user)
}
