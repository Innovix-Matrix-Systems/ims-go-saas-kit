package authn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeLookup struct {
	record   *UserRecord
	touched  bool
	touchErr error
}

func (f *fakeLookup) FindByEmail(ctx context.Context, email string) (*UserRecord, error) {
	if f.record == nil {
		return nil, apperr.New(apperr.ResourceNotFound, "not found")
	}
	return f.record, nil
}

func (f *fakeLookup) FindByID(ctx context.Context, id int64) (*UserRecord, error) {
	return f.FindByEmail(ctx, "")
}

func (f *fakeLookup) TouchLogin(ctx context.Context, userID int64, device string) error {
	f.touched = true
	return f.touchErr
}

type fakeAuthenticator struct {
	issued   []int64
	revoked  []string
	issueErr error
}

func (f *fakeAuthenticator) Issue(ctx context.Context, userID int64, device string) (Token, error) {
	if f.issueErr != nil {
		return Token{}, f.issueErr
	}
	f.issued = append(f.issued, userID)
	text := "42|secret"
	return Token{ID: 42, PlainText: text}, nil
}

func (f *fakeAuthenticator) Verify(ctx context.Context, raw string) (*Identity, error) {
	return nil, apperr.New(apperr.TokenInvalid, "invalid")
}

func (f *fakeAuthenticator) Revoke(ctx context.Context, userID int64, device string) error {
	f.revoked = append(f.revoked, device)
	return nil
}

func (f *fakeAuthenticator) RevokeOthers(context.Context, int64, int64) error { return nil }

func (f *fakeAuthenticator) IssueSetup(ctx context.Context, userID int64, device string) (Token, error) {
	return f.Issue(ctx, userID, device)
}

func (f *fakeAuthenticator) RevokeAll(ctx context.Context, userID int64) error {
	return nil
}

func testService(t *testing.T, record *UserRecord, cfg config.Auth) (*Service, *fakeLookup, *fakeAuthenticator, *memory.Store) {
	t.Helper()
	store := memory.New("authn-test-")
	t.Cleanup(store.Close)

	lookup := &fakeLookup{record: record}
	auth := &fakeAuthenticator{}
	otp := NewOTPService(store, cfg, testLogger(), true)
	throttle := NewThrottle(store, cfg.LoginMaxAttempts, cfg.LoginDecay)
	return NewService(lookup, auth, otp, throttle, cfg), lookup, auth, store
}

func testCfg() config.Auth {
	return config.Auth{
		LoginMaxAttempts:   3,
		LoginDecay:         time.Minute,
		CheckIsActive:      true,
		CheckEmailVerified: true,
		OTPLength:          6,
		OTPExpiry:          5 * time.Minute,
		BcryptRounds:       bcrypt.MinCost,
	}
}

func hashedPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return string(hash)
}

func activeRecord(t *testing.T) *UserRecord {
	t.Helper()
	return &UserRecord{
		ID:            1,
		Name:          "Test User",
		Email:         "user@example.com",
		Password:      hashedPassword(t, "secret123"),
		IsActive:      true,
		EmailVerified: true,
		TenantID:      1,
		BranchID:      1,
		TenantActive:  true,
	}
}

func TestLoginRefusesSuspendedTenant(t *testing.T) {
	record := activeRecord(t)
	record.TenantActive = false
	service, _, auth, _ := testService(t, record, testCfg())

	if _, err := service.Login(context.Background(), record.Email, "secret123", "web", "127.0.0.1"); !apperr.Is(err, apperr.TenantSuspended) {
		t.Fatalf("Login() error = %v, want TENANT_SUSPENDED", err)
	}
	// A wrong password answers as for any account: the tenant's state is not revealed.
	if _, err := service.Login(context.Background(), record.Email, "wrong", "web", "127.0.0.1"); !apperr.Is(err, apperr.InvalidCredentials) {
		t.Fatalf("Login(wrong password) error = %v, want INVALID_CREDENTIALS", err)
	}
	if len(auth.issued) != 0 {
		t.Fatalf("issued tokens = %v, want none", auth.issued)
	}
}

func TestLoginSuccess(t *testing.T) {
	cfg := testCfg()
	service, lookup, auth, _ := testService(t, activeRecord(t), cfg)

	result, err := service.Login(context.Background(), "user@example.com", "secret123", "test-device", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.Status != StatusAuthSuccess {
		t.Errorf("status = %q, want %q", result.Status, StatusAuthSuccess)
	}
	if result.Token == nil || result.Token.PlainText == "" {
		t.Error("token missing")
	}
	if len(auth.issued) != 1 {
		t.Errorf("issued tokens = %v", auth.issued)
	}
	if !lookup.touched {
		t.Error("last_login_at not touched")
	}
}

func TestLoginWrongPasswordIncrementsThrottle(t *testing.T) {
	cfg := testCfg()
	service, _, _, store := testService(t, activeRecord(t), cfg)
	ctx := context.Background()

	for i := 0; i < cfg.LoginMaxAttempts; i++ {
		_, err := service.Login(ctx, "user@example.com", "wrong", "device", "127.0.0.1")
		if !apperr.Is(err, apperr.InvalidCredentials) {
			t.Fatalf("attempt %d error = %v, want INVALID_CREDENTIALS", i, err)
		}
	}

	_, err := service.Login(ctx, "user@example.com", "wrong", "device", "127.0.0.1")
	if !apperr.Is(err, apperr.AccountLocked) {
		t.Fatalf("locked login error = %v, want ACCOUNT_LOCKED", err)
	}
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatal("error is not *apperr.Error")
	}
	if appErr.RetryAfter <= 0 {
		t.Error("retry_after not set on lockout")
	}

	// H6: rotating IPs must NOT reset the per-account budget — the account
	// stays locked even from a fresh source address.
	if _, err := service.Login(ctx, "user@example.com", "secret123", "device", "10.0.0.99"); !apperr.Is(err, apperr.AccountLocked) {
		t.Fatalf("rotated-IP login error = %v, want ACCOUNT_LOCKED (per-account counter)", err)
	}

	_ = store.Delete(ctx, accountThrottleKey("user@example.com"))
	_ = store.Delete(ctx, ipThrottleKey("127.0.0.1"))
}

func TestLoginUnknownEmailIsIndistinguishableFromWrongPassword(t *testing.T) {
	cfg := testCfg()
	service, _, _, _ := testService(t, nil, cfg)

	_, err := service.Login(context.Background(), "missing@example.com", "secret123", "device", "127.0.0.1")
	if !apperr.Is(err, apperr.InvalidCredentials) {
		t.Fatalf("error = %v, want INVALID_CREDENTIALS (uniform response)", err)
	}
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatal("error is not *apperr.Error")
	}
	if appErr.Message != "The provided credentials are incorrect." {
		t.Errorf("message = %q, want the uniform credentials message", appErr.Message)
	}
}

func TestLoginPerIPLockoutCatchesSprayAcrossAccounts(t *testing.T) {
	cfg := testCfg()
	service, _, _, _ := testService(t, activeRecord(t), cfg)
	ctx := context.Background()

	// Spraying many accounts from one source: the per-IP counter locks.
	for i := 0; i < cfg.LoginMaxAttempts; i++ {
		email := "victim" + string(rune('a'+i)) + "@example.com"
		if _, err := service.Login(ctx, email, "wrong", "device", "203.0.113.7"); !apperr.Is(err, apperr.InvalidCredentials) {
			t.Fatalf("spray attempt %d error = %v", i, err)
		}
	}
	if _, err := service.Login(ctx, "user@example.com", "secret123", "device", "203.0.113.7"); !apperr.Is(err, apperr.AccountLocked) {
		t.Fatalf("sprayed-IP login error = %v, want ACCOUNT_LOCKED (per-IP counter)", err)
	}
}

func TestLoginDeactivatedAccount(t *testing.T) {
	cfg := testCfg()
	record := activeRecord(t)
	record.IsActive = false
	service, _, _, _ := testService(t, record, cfg)

	_, err := service.Login(context.Background(), record.Email, "secret123", "device", "127.0.0.1")
	if !apperr.Is(err, apperr.AccountDeactivated) {
		t.Fatalf("error = %v, want ACCOUNT_DEACTIVATED", err)
	}
}

func TestOTPFlow(t *testing.T) {
	cfg := testCfg()
	cfg.OTPEnabled = true
	service, _, auth, store := testService(t, activeRecord(t), cfg)
	ctx := context.Background()

	result, err := service.Login(ctx, "user@example.com", "secret123", "device", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.Status != StatusOTPRequired {
		t.Fatalf("status = %q, want %q", result.Status, StatusOTPRequired)
	}
	if result.Token != nil {
		t.Fatal("token issued before OTP completion")
	}

	if _, err := service.VerifyOTP(ctx, "user@example.com", "000000", "device", "127.0.0.1"); !apperr.Is(err, apperr.OTPInvalid) {
		t.Fatalf("wrong OTP error = %v, want OTP_INVALID", err)
	}

	var stored string
	found, err := store.Get(ctx, OTPKey(1), &stored)
	if err != nil || !found || len(stored) != cfg.OTPLength {
		t.Fatalf("stored OTP = %q, found %v, err %v", stored, found, err)
	}

	result, err = service.VerifyOTP(ctx, "user@example.com", stored, "device", "127.0.0.1")
	if err != nil {
		t.Fatalf("VerifyOTP() error = %v", err)
	}
	if result.Status != StatusAuthSuccess || result.Token == nil {
		t.Fatalf("result = %+v", result)
	}
	if len(auth.issued) != 1 {
		t.Fatalf("issued tokens = %v", auth.issued)
	}
}

func TestThrottleWindow(t *testing.T) {
	store := memory.New("throttle-test-")
	defer store.Close()
	throttle := NewThrottle(store, 2, time.Minute)
	ctx := context.Background()

	if tooMany, _, _ := throttle.TooMany(ctx, "key"); tooMany {
		t.Fatal("unexpected lockout before hits")
	}
	_ = throttle.Hit(ctx, "key")
	_ = throttle.Hit(ctx, "key")
	tooMany, retryAfter, err := throttle.TooMany(ctx, "key")
	if err != nil || !tooMany || retryAfter <= 0 {
		t.Fatalf("TooMany() = %v, %v, %v", tooMany, retryAfter, err)
	}
	_ = throttle.Clear(ctx, "key")
	if tooMany, _, _ := throttle.TooMany(ctx, "key"); tooMany {
		t.Fatal("lockout not cleared")
	}
}
