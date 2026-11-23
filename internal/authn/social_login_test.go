package authn

import (
	"context"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn/social"
)

type fakeProvider struct {
	identity *social.Identity
	err      error
}

func (f *fakeProvider) Name() string        { return "google" }
func (f *fakeProvider) DisplayName() string { return "Google" }

func (f *fakeProvider) Verify(context.Context, social.Proof) (*social.Identity, error) {
	return f.identity, f.err
}

type fakeAudit struct{ events []LoginEvent }

func (f *fakeAudit) Login(_ context.Context, event LoginEvent) { f.events = append(f.events, event) }

func TestSocialLogin(t *testing.T) {
	verified := &social.Identity{Email: "user@example.com", EmailVerified: true}
	tests := []struct {
		name     string
		provider string
		identity *social.Identity
		err      error
		record   *UserRecord
		wantCode apperr.Code
	}{
		{name: "signs in the user with the verified e-mail", provider: "google", identity: verified,
			record: &UserRecord{ID: 7, Email: "user@example.com", IsActive: true, EmailVerified: true}},
		{name: "provider not enabled", provider: "facebook", wantCode: apperr.ResourceNotFound},
		{name: "provider rejects the proof", provider: "google",
			err: apperr.New(apperr.InvalidCredentials, "bad token"), wantCode: apperr.InvalidCredentials},
		{name: "e-mail not verified by the provider", provider: "google",
			identity: &social.Identity{Email: "user@example.com"},
			record:   &UserRecord{ID: 7, IsActive: true, EmailVerified: true}, wantCode: apperr.AccountUnverified},
		{name: "no e-mail at all", provider: "google", identity: &social.Identity{EmailVerified: true},
			wantCode: apperr.AccountUnverified},
		{name: "no user with that e-mail", provider: "google", identity: verified, wantCode: apperr.SocialAccountNotFound},
		{name: "inactive user, as on password login", provider: "google", identity: verified,
			record: &UserRecord{ID: 7, EmailVerified: true}, wantCode: apperr.AccountDeactivated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testCfg()
			cfg.OTPEnabled = true // social login never asks for an OTP
			service, lookup, auth, _ := testService(t, tt.record, cfg)
			service.WithSocial(social.NewRegistry(&fakeProvider{identity: tt.identity, err: tt.err}))

			result, err := service.SocialLogin(context.Background(), tt.provider, social.Proof{IDToken: "x"}, "web", "10.0.0.1")
			if tt.wantCode != "" {
				if !apperr.Is(err, tt.wantCode) {
					t.Fatalf("err = %v, want %s", err, tt.wantCode)
				}
				if len(auth.issued) != 0 {
					t.Fatal("a token was issued for a refused sign-in")
				}
				return
			}
			if err != nil {
				t.Fatalf("SocialLogin: %v", err)
			}
			if result.Status != StatusAuthSuccess || result.Token == nil || !lookup.touched {
				t.Fatalf("result = %+v, want a token without OTP", result)
			}
		})
	}
}

func TestSocialLoginFailuresCountAgainstTheIP(t *testing.T) {
	cfg := testCfg() // LoginMaxAttempts: 3
	service, _, _, _ := testService(t, nil, cfg)
	service.WithSocial(social.NewRegistry(&fakeProvider{err: apperr.New(apperr.InvalidCredentials, "bad token")}))

	for range cfg.LoginMaxAttempts {
		_, _ = service.SocialLogin(context.Background(), "google", social.Proof{}, "web", "10.0.0.1")
	}
	_, err := service.SocialLogin(context.Background(), "google", social.Proof{}, "web", "10.0.0.1")
	if !apperr.Is(err, apperr.AccountLocked) {
		t.Fatalf("err = %v, want ACCOUNT_LOCKED after %d failures", err, cfg.LoginMaxAttempts)
	}
}

func TestSocialLoginProviderOutageDoesNotLockTheIP(t *testing.T) {
	cfg := testCfg() // LoginMaxAttempts: 3
	service, _, _, _ := testService(t, nil, cfg)
	service.WithSocial(social.NewRegistry(&fakeProvider{err: apperr.New(apperr.ServiceUnavailable, "GitHub is down")}))

	for range cfg.LoginMaxAttempts + 2 {
		_, err := service.SocialLogin(context.Background(), "google", social.Proof{}, "web", "10.0.0.1")
		if !apperr.Is(err, apperr.ServiceUnavailable) {
			t.Fatalf("err = %v, want SERVICE_UNAVAILABLE, never a lockout", err)
		}
	}
}

func TestLoginAudit(t *testing.T) {
	record := &UserRecord{ID: 7, Email: "user@example.com", Password: hashedPassword(t, "secret123"), IsActive: true, EmailVerified: true}

	t.Run("password login, success and failure", func(t *testing.T) {
		service, _, _, _ := testService(t, record, testCfg())
		audit := &fakeAudit{}
		service.WithAudit(audit)

		_, _ = service.Login(context.Background(), "user@example.com", "wrong", "web", "10.0.0.1")
		_, _ = service.Login(context.Background(), "user@example.com", "secret123", "web", "10.0.0.1")

		if len(audit.events) != 2 {
			t.Fatalf("events = %d, want 2", len(audit.events))
		}
		failed, ok := audit.events[0], audit.events[1]
		if failed.Method != MethodPassword || failed.Err == nil || failed.User != nil {
			t.Errorf("failed event = %+v", failed)
		}
		if ok.Err != nil || ok.User == nil || ok.User.ID != 7 || ok.Device != "web" {
			t.Errorf("success event = %+v", ok)
		}
	})

	t.Run("an OTP challenge is recorded when the code is verified", func(t *testing.T) {
		cfg := testCfg()
		cfg.OTPEnabled = true
		service, _, _, _ := testService(t, record, cfg)
		audit := &fakeAudit{}
		service.WithAudit(audit)

		result, err := service.Login(context.Background(), "user@example.com", "secret123", "web", "10.0.0.1")
		if err != nil || result.Status != StatusOTPRequired {
			t.Fatalf("Login = %+v, %v; want OTP_REQUIRED", result, err)
		}
		if len(audit.events) != 0 {
			t.Fatalf("events = %+v, want none before the OTP", audit.events)
		}
	})

	t.Run("social login records the provider", func(t *testing.T) {
		service, _, _, _ := testService(t, record, testCfg())
		audit := &fakeAudit{}
		service.WithAudit(audit).WithSocial(social.NewRegistry(&fakeProvider{
			identity: &social.Identity{Email: "user@example.com", EmailVerified: true},
		}))

		if _, err := service.SocialLogin(context.Background(), "google", social.Proof{}, "web", "10.0.0.1"); err != nil {
			t.Fatalf("SocialLogin: %v", err)
		}
		if len(audit.events) != 1 || audit.events[0].Method != "google" || audit.events[0].Email != "user@example.com" {
			t.Fatalf("events = %+v, want one google login", audit.events)
		}
	})
}
