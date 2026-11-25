package app_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// Two-factor authentication: TOTP and e-mail codes, recovery
// codes, the tenant policy with setup-only tokens, and the admin reset
// (docs/guides/authentication.md#two-factor-authentication).

type loginAnswer struct {
	Status string  `json:"status"`
	Method string  `json:"method"`
	Token  *string `json:"token"`
}

func passwordLogin(t *testing.T, app *testkit.App, email string) loginAnswer {
	t.Helper()
	var answer loginAnswer
	app.Guest().Post("/api/v1/login", map[string]any{"email": email, "password": testkit.Password, "device": "web"}).
		Status(t, http.StatusOK).Data(t, &answer)
	return answer
}

func verifyCode(app *testkit.App, email, code string) *testkit.Response {
	return app.Guest().Post("/api/v1/verify-otp", map[string]any{"email": email, "otp": code, "device": "web"})
}

// cachedCode reads a code the app mailed (it lives only in the cache).
func cachedCode(t *testing.T, app *testkit.App, key string) string {
	t.Helper()
	var code string
	if found, err := app.Deps.Platform.Cache.Get(context.Background(), key, &code); err != nil || !found {
		t.Fatalf("no code under %q (found=%v, err=%v)", key, found, err)
	}
	return code
}

func enrolTOTP(t *testing.T, app *testkit.App, user *testkit.User) (secret string, codes []string) {
	t.Helper()
	var setup struct {
		Method     string `json:"method"`
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}
	app.As(user).Post("/api/v1/two-factor", map[string]any{"method": "totp", "password": testkit.Password}).
		Status(t, http.StatusOK).Data(t, &setup)
	if setup.Secret == "" || !strings.HasPrefix(setup.OTPAuthURL, "otpauth://totp/") {
		t.Fatalf("setup = %+v", setup)
	}
	var confirmed struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	app.As(user).Post("/api/v1/two-factor/confirm", map[string]any{"code": testkit.TOTP(t, setup.Secret, -1)}).
		Status(t, http.StatusOK).Data(t, &confirmed)
	return setup.Secret, confirmed.RecoveryCodes
}

func TestTwoFactorWithAnAuthenticatorApp(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	other := app.User(t)

	app.As(user).Post("/api/v1/two-factor", map[string]any{"method": "totp", "password": "wrong-password"}).
		Status(t, http.StatusUnprocessableEntity)
	app.As(user).Post("/api/v1/two-factor/confirm", map[string]any{"code": "123456"}).Status(t, http.StatusConflict)

	secret, codes := enrolTOTP(t, app, user)
	if len(codes) != 8 {
		t.Fatalf("recovery codes = %v, want 8", codes)
	}
	var stored string
	if err := app.Pool.QueryRow(context.Background(), `SELECT secret FROM user_two_factor WHERE user_id = $1`, user.ID).Scan(&stored); err != nil {
		t.Fatalf("read secret: %v", err)
	}
	if !strings.HasPrefix(stored, "v1:") || strings.Contains(stored, secret) {
		t.Fatalf("the secret is stored readable: %q", stored)
	}
	app.As(user).Post("/api/v1/two-factor", map[string]any{"method": "email", "password": testkit.Password}).
		Status(t, http.StatusConflict)

	// The password alone no longer signs in.
	answer := passwordLogin(t, app, user.Email)
	if answer.Status != authn.StatusOTPRequired || answer.Method != authn.TwoFactorTOTP || answer.Token != nil {
		t.Fatalf("login = %+v, want OTP_REQUIRED for totp without a token", answer)
	}
	requireErrorCode(t, verifyCode(app, user.Email, "000000"), http.StatusBadRequest, "OTP_INVALID")
	code := testkit.TOTP(t, secret, 0)
	var signedIn loginAnswer
	verifyCode(app, user.Email, code).Status(t, http.StatusOK).Data(t, &signedIn)
	if signedIn.Status != authn.StatusAuthSuccess || signedIn.Token == nil {
		t.Fatalf("verify = %+v, want a token", signedIn)
	}
	// A code works once, and only after a password sign-in.
	passwordLogin(t, app, user.Email)
	requireErrorCode(t, verifyCode(app, user.Email, code), http.StatusBadRequest, "OTP_INVALID")
	requireErrorCode(t, verifyCode(app, other.Email, "123456"), http.StatusBadRequest, "OTP_EXPIRED")

	// A recovery code signs in once, and mails a notice.
	passwordLogin(t, app, user.Email)
	verifyCode(app, user.Email, strings.ToUpper(codes[0])).Status(t, http.StatusOK)
	passwordLogin(t, app, user.Email)
	requireErrorCode(t, verifyCode(app, user.Email, codes[0]), http.StatusBadRequest, "OTP_INVALID")
	var status struct {
		Enabled           bool   `json:"enabled"`
		Method            string `json:"method"`
		RecoveryCodesLeft int    `json:"recovery_codes_left"`
	}
	app.As(user).Get("/api/v1/two-factor").Status(t, http.StatusOK).Data(t, &status)
	if !status.Enabled || status.Method != "totp" || status.RecoveryCodesLeft != 7 {
		t.Fatalf("status = %+v", status)
	}
	if n := countRows(t, app, `SELECT count(*) FROM deliveries WHERE template IN ('two-factor-enabled', 'recovery-code-used') AND user_id = $1`, user.ID); n != 2 {
		t.Fatalf("security mails = %d, want 2", n)
	}

	// Turning it off needs the password, signs other sessions out, and the
	// password alone signs in again.
	session := &testkit.User{ID: user.ID, Token: *signedIn.Token}
	app.As(user).Delete("/api/v1/two-factor").Status(t, http.StatusBadRequest) // no password
	app.As(user).Do(http.MethodDelete, "/api/v1/two-factor", map[string]any{"password": testkit.Password}).Status(t, http.StatusNoContent)
	app.As(session).Get("/api/v1/sessions").Status(t, http.StatusUnauthorized)
	if after := passwordLogin(t, app, user.Email); after.Status != authn.StatusAuthSuccess {
		t.Fatalf("login after disabling = %+v", after)
	}
	if n := countRows(t, app, `SELECT count(*) FROM activity_log WHERE subject_id = $1
		AND action IN ('user.two_factor_enabled', 'user.two_factor_disabled')`, user.ID); n != 2 {
		t.Fatalf("two-factor activity rows = %d, want 2", n)
	}
}

func TestTwoFactorByEmail(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)

	app.As(user).Post("/api/v1/two-factor", map[string]any{"method": "email", "password": testkit.Password}).Status(t, http.StatusOK)
	enrolment := cachedCode(t, app, "2fa:enrol:"+strconv.FormatInt(user.ID, 10))
	var confirmed struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	app.As(user).Post("/api/v1/two-factor/confirm", map[string]any{"code": enrolment}).Status(t, http.StatusOK).Data(t, &confirmed)

	answer := passwordLogin(t, app, user.Email)
	if answer.Status != authn.StatusOTPRequired || answer.Method != authn.TwoFactorEmail {
		t.Fatalf("login = %+v, want an e-mail code", answer)
	}
	code := cachedCode(t, app, authn.OTPKey(user.ID))
	verifyCode(app, user.Email, code).Status(t, http.StatusOK)

	if n := countRows(t, app, `SELECT count(*) FROM deliveries WHERE template = 'otp-code' AND user_id = $1`, user.ID); n != 2 {
		t.Fatalf("code mails = %d, want 2 (enrolment and sign-in)", n)
	}

	var regenerated struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	app.As(user).Post("/api/v1/two-factor/recovery-codes", map[string]any{"password": testkit.Password}).
		Status(t, http.StatusOK).Data(t, &regenerated)
	passwordLogin(t, app, user.Email)
	requireErrorCode(t, verifyCode(app, user.Email, confirmed.RecoveryCodes[0]), http.StatusBadRequest, "OTP_INVALID")
	verifyCode(app, user.Email, regenerated.RecoveryCodes[0]).Status(t, http.StatusOK)
}

func TestTwoFactorPolicyGivesASetupOnlyToken(t *testing.T) {
	app := testkit.New(t)
	tenantID := app.Tenant(t, "Strict")
	user := app.User(t, testkit.InTenant(tenantID))
	if err := app.Deps.Platform.Settings.Set(context.Background(), tenancy.Scope{TenantID: tenantID}, "security.require_two_factor", true); err != nil {
		t.Fatalf("require two-factor: %v", err)
	}

	answer := passwordLogin(t, app, user.Email)
	if answer.Status != authn.StatusTwoFactorSetup || answer.Token == nil {
		t.Fatalf("login = %+v, want a setup-only token", answer)
	}
	setupOnly := &testkit.User{ID: user.ID, Token: *answer.Token}
	requireErrorCode(t, app.As(setupOnly).Get("/api/v1/branch"), http.StatusForbidden, "TWO_FACTOR_SETUP_REQUIRED")
	requireErrorCode(t, app.As(setupOnly).Get("/api/v1/files/1"), http.StatusForbidden, "TWO_FACTOR_SETUP_REQUIRED")
	var status struct {
		Required bool `json:"required"`
	}
	app.As(setupOnly).Get("/api/v1/two-factor").Status(t, http.StatusOK).Data(t, &status)
	if !status.Required {
		t.Fatal("status.required = false under the policy")
	}

	var setup struct {
		Secret string `json:"secret"`
	}
	app.As(setupOnly).Post("/api/v1/two-factor", map[string]any{"method": "totp", "password": testkit.Password}).
		Status(t, http.StatusOK).Data(t, &setup)
	var confirmed struct {
		Token *string `json:"token"`
	}
	app.As(setupOnly).Post("/api/v1/two-factor/confirm", map[string]any{"code": testkit.TOTP(t, setup.Secret, 0)}).
		Status(t, http.StatusOK).Data(t, &confirmed)
	if confirmed.Token == nil {
		t.Fatal("confirming with a setup-only token returned no full token")
	}
	app.As(setupOnly).Get("/api/v1/two-factor").Status(t, http.StatusUnauthorized)
	app.As(&testkit.User{ID: user.ID, Token: *confirmed.Token}).Get("/api/v1/branch").Status(t, http.StatusOK)

	// Other tenants are not affected.
	if answer := passwordLogin(t, app, app.User(t).Email); answer.Status != authn.StatusAuthSuccess {
		t.Fatalf("a user of another tenant got %+v", answer)
	}
}

func TestAdminResetsTwoFactor(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	admin := app.User(t)
	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer", authz.PermUserViewAll)))
	stranger := app.User(t, testkit.InTenant(app.Tenant(t, "Elsewhere")))
	enrolTOTP(t, app, user)
	path := "/api/v1/user/" + strconv.FormatInt(user.ID, 10) + "/two-factor"

	app.As(viewer).Delete(path).Status(t, http.StatusForbidden)
	app.As(stranger).Delete(path).Status(t, http.StatusNotFound) // another tenant: not in scope
	app.As(admin).Delete(path).Status(t, http.StatusNoContent)
	app.As(admin).Delete(path).Status(t, http.StatusConflict) // already off
	if answer := passwordLogin(t, app, user.Email); answer.Status != authn.StatusAuthSuccess {
		t.Fatalf("login after the reset = %+v", answer)
	}
	if n := countRows(t, app, `SELECT count(*) FROM activity_log WHERE action = 'user.two_factor_reset' AND subject_id = $1 AND actor_id = $2`, user.ID, admin.ID); n != 1 {
		t.Fatalf("reset activity rows = %d, want 1", n)
	}
}

// Concurrent sign-ins with one recovery code: exactly one succeeds. Two
// different codes at once both work, and both are marked used.
func TestRecoveryCodesUnderConcurrency(t *testing.T) {
	app := testkit.New(t)
	user := app.User(t)
	_, codes := enrolTOTP(t, app, user)
	record := &authn.UserRecord{ID: user.ID, Email: user.Email}
	verifyAll := func(attempts []string) int64 {
		var wg sync.WaitGroup
		var succeeded atomic.Int64
		start := make(chan struct{})
		for _, code := range attempts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if app.Deps.Platform.TwoFactor.Verify(context.Background(), record, code) == nil {
					succeeded.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		return succeeded.Load()
	}
	left := func() int {
		var status struct {
			RecoveryCodesLeft int `json:"recovery_codes_left"`
		}
		app.As(user).Get("/api/v1/two-factor").Status(t, http.StatusOK).Data(t, &status)
		return status.RecoveryCodesLeft
	}

	same := make([]string, 8)
	for i := range same {
		same[i] = codes[0]
	}
	if n := verifyAll(same); n != 1 {
		t.Fatalf("one recovery code signed in %d times, want once", n)
	}
	if n := left(); n != 7 {
		t.Fatalf("codes left = %d, want 7", n)
	}
	if n := verifyAll([]string{codes[1], codes[2]}); n != 2 {
		t.Fatalf("two different codes at once: %d succeeded, want 2", n)
	}
	if n := left(); n != 5 {
		t.Fatalf("codes left = %d, want 5 (no lost mark)", n)
	}
}
