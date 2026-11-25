package authn

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// Account returns the signed-in user's record (for account changes such as
// two-factor enrolment).
func (s *Service) Account(ctx context.Context, userID int64) (*UserRecord, error) {
	return s.users.FindByID(ctx, userID)
}

// ConfirmPasswordOnly is ConfirmPassword for callers that only need to know
// the password was right.
func (s *Service) ConfirmPasswordOnly(ctx context.Context, userID int64, password string) error {
	_, err := s.ConfirmPassword(ctx, userID, password)
	return err
}

// ConfirmPassword checks the signed-in user's password before a sensitive
// account change (turning two-factor on or off, new recovery codes). Wrong
// answers count against the account's sign-in throttle, so it cannot be
// used to guess the password faster than signing in.
func (s *Service) ConfirmPassword(ctx context.Context, userID int64, password string) (_ *UserRecord, err error) {
	ctx, end := tracing.Begin(ctx, "authn.confirm_password", attribute.Int64("user.id", userID))
	defer end(&err)
	record, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	key := accountThrottleKey(record.Email)
	if err := s.checkThrottle(ctx, key); err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(record.Password), []byte(password)) != nil {
		_ = s.throttle.Hit(ctx, key)
		return nil, apperr.Validation(map[string][]string{"password": {"The password is incorrect."}})
	}
	return record, nil
}
