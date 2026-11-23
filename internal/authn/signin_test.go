package authn

import (
	"context"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

// SignIn and SignInOTP back the MCP authorization page: the same checks as
// Login, but no session token.

func TestSignInChecksLikeLoginButIssuesNoToken(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*UserRecord)
		password string
		want     apperr.Code // "" = success
	}{
		{name: "valid", password: "secret123"},
		{name: "wrong password", password: "nope", want: apperr.InvalidCredentials},
		{name: "deactivated", password: "secret123", mutate: func(r *UserRecord) { r.IsActive = false }, want: apperr.AccountDeactivated},
		{name: "unverified e-mail", password: "secret123", mutate: func(r *UserRecord) { r.EmailVerified = false }, want: apperr.AccountUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := activeRecord(t)
			if tc.mutate != nil {
				tc.mutate(record)
			}
			service, lookup, auth, _ := testService(t, record, testCfg())

			user, otp, err := service.SignIn(context.Background(), record.Email, tc.password, "127.0.0.1")
			if tc.want != "" {
				if !apperr.Is(err, tc.want) {
					t.Fatalf("error = %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil || otp || user == nil || user.ID != record.ID {
				t.Fatalf("SignIn() = %+v, otp %v, err %v", user, otp, err)
			}
			if len(auth.issued) != 0 || lookup.touched {
				t.Fatalf("SignIn must not issue a token or touch the login (issued %v, touched %v)", auth.issued, lookup.touched)
			}
		})
	}
}

func TestSignInLocksOutLikeLogin(t *testing.T) {
	cfg := testCfg()
	record := activeRecord(t)
	service, _, _, _ := testService(t, record, cfg)
	ctx := context.Background()
	for i := 0; i < cfg.LoginMaxAttempts; i++ {
		_, _, _ = service.SignIn(ctx, record.Email, "nope", "127.0.0.1")
	}
	if _, _, err := service.SignIn(ctx, record.Email, "secret123", "127.0.0.1"); !apperr.Is(err, apperr.AccountLocked) {
		t.Fatalf("after %d failures: error = %v, want ACCOUNT_LOCKED", cfg.LoginMaxAttempts, err)
	}
}

func TestSignInWithOTP(t *testing.T) {
	cfg := testCfg()
	cfg.OTPEnabled = true
	record := activeRecord(t)
	service, _, auth, store := testService(t, record, cfg)
	ctx := context.Background()

	user, otp, err := service.SignIn(ctx, record.Email, "secret123", "127.0.0.1")
	if err != nil || !otp || user == nil {
		t.Fatalf("SignIn() = %+v, otp %v, err %v; want an OTP step", user, otp, err)
	}
	if !service.OTPEnabled() {
		t.Fatal("OTPEnabled() = false")
	}
	if _, err := service.SignInOTP(ctx, record.Email, "000000", "127.0.0.1"); !apperr.Is(err, apperr.OTPInvalid) {
		t.Fatalf("wrong OTP: error = %v, want OTP_INVALID", err)
	}
	var code string
	if found, err := store.Get(ctx, OTPKey(record.ID), &code); err != nil || !found {
		t.Fatalf("stored OTP: found %v, err %v", found, err)
	}
	user, err = service.SignInOTP(ctx, record.Email, code, "127.0.0.1")
	if err != nil || user.ID != record.ID {
		t.Fatalf("SignInOTP() = %+v, %v", user, err)
	}
	if len(auth.issued) != 0 {
		t.Fatalf("SignInOTP must not issue a token, issued %v", auth.issued)
	}
}
