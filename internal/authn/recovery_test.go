package authn

import (
	"context"
	"net/url"
	"regexp"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/memory"
)

type captureDeliverer struct{ sent []AccountMessage }

func (d *captureDeliverer) DeliverAccount(_ context.Context, message AccountMessage) error {
	d.sent = append(d.sent, message)
	return nil
}

type fakeWriter struct {
	password string
	verified bool
}

func (f *fakeWriter) SetPassword(_ context.Context, _ int64, hash string) error {
	f.password = hash
	return nil
}

func (f *fakeWriter) MarkEmailVerified(context.Context, int64) error {
	f.verified = true
	return nil
}

func (f *fakeWriter) AcceptInvitation(_ context.Context, _ int64, hash string) (bool, error) {
	f.password, f.verified = hash, true
	return true, nil
}

type revokeCounter struct {
	fakeAuthenticator
	revokedAll int
}

func (r *revokeCounter) RevokeAll(context.Context, int64) error {
	r.revokedAll++
	return nil
}

var tokenInLink = regexp.MustCompile(`token=([0-9a-f]+)`)

func tokenFrom(t *testing.T, message AccountMessage) string {
	t.Helper()
	link, _ := message.Data["link"].(string)
	match := tokenInLink.FindStringSubmatch(link)
	if len(match) != 2 {
		t.Fatalf("no token link in message: %+v", message)
	}
	token, _ := url.QueryUnescape(match[1])
	return token
}

func newRecoveryUnderTest(t *testing.T, user *UserRecord) (*Recovery, *captureDeliverer, *fakeWriter, *revokeCounter) {
	t.Helper()
	store := memory.New("recovery-")
	t.Cleanup(store.Close)
	mailer := &captureDeliverer{}
	writer := &fakeWriter{}
	auth := &revokeCounter{}
	cfg := config.Auth{
		CheckIsActive:       true,
		BcryptRounds:        bcrypt.MinCost,
		PasswordResetExpiry: time.Hour,
		EmailVerifyExpiry:   24 * time.Hour,
		PasswordResetURL:    "https://app.test/reset?token={token}&email={email}",
		EmailVerifyURL:      "https://app.test/verify?token={token}",
	}
	return NewRecovery(&fakeLookup{record: user}, writer, auth, store, mailer, cfg, testLogger()), mailer, writer, auth
}

func TestPasswordResetFlow(t *testing.T) {
	user := &UserRecord{ID: 9, Name: "Jane", Email: "jane@app.test", IsActive: true}
	recovery, mailer, writer, auth := newRecoveryUnderTest(t, user)
	ctx := context.Background()

	if err := recovery.SendPasswordResetLink(ctx, "jane@app.test"); err != nil {
		t.Fatalf("SendPasswordResetLink() error = %v", err)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].User.Email != "jane@app.test" || mailer.sent[0].Template != TemplatePasswordReset {
		t.Fatalf("mails = %+v", mailer.sent)
	}
	token := tokenFrom(t, mailer.sent[0])

	// A second request inside the resend window sends nothing new.
	_ = recovery.SendPasswordResetLink(ctx, "jane@app.test")
	if len(mailer.sent) != 1 {
		t.Fatalf("resend window ignored: %d mails", len(mailer.sent))
	}

	if err := recovery.ResetPassword(ctx, "other@app.test", token, "new-password-1"); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("wrong e-mail error = %v, want validation", err)
	}
	// The failed attempt above consumed nothing it should not: tokens are
	// single-use, so ask for a fresh one past the window by issuing directly.
	token, err := recovery.issue(ctx, "pwreset", user, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := recovery.ResetPassword(ctx, "Jane@App.test", token, "new-password-1"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(writer.password), []byte("new-password-1")) != nil {
		t.Fatal("stored hash does not match the new password")
	}
	if !writer.verified || auth.revokedAll != 1 {
		t.Fatalf("verified=%v revokedAll=%d, want true/1", writer.verified, auth.revokedAll)
	}
	if err := recovery.ResetPassword(ctx, "jane@app.test", token, "another-pass-2"); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("reused token error = %v, want validation", err)
	}
}

func TestPasswordResetDoesNotRevealAccounts(t *testing.T) {
	recovery, mailer, _, _ := newRecoveryUnderTest(t, nil)
	if err := recovery.SendPasswordResetLink(context.Background(), "nobody@app.test"); err != nil {
		t.Fatalf("unknown e-mail error = %v, want nil", err)
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("mail sent for an unknown address")
	}
}

func TestEmailVerificationFlow(t *testing.T) {
	user := &UserRecord{ID: 4, Name: "Sam", Email: "sam@app.test", IsActive: true}
	recovery, mailer, writer, _ := newRecoveryUnderTest(t, user)
	ctx := context.Background()

	if err := recovery.SendVerificationTo(ctx, user.ID); err != nil {
		t.Fatalf("SendVerificationTo() error = %v", err)
	}
	token := tokenFrom(t, mailer.sent[0])
	if err := recovery.VerifyEmail(ctx, "not-the-token"); !apperr.Is(err, apperr.ValidationFailed) {
		t.Fatalf("bad token error = %v", err)
	}
	if err := recovery.VerifyEmail(ctx, token); err != nil || !writer.verified {
		t.Fatalf("VerifyEmail() = %v, verified=%v", err, writer.verified)
	}

	user.EmailVerified = true
	before := len(mailer.sent)
	_ = recovery.SendVerificationLink(ctx, user.Email)
	if len(mailer.sent) != before {
		t.Fatal("verification mail sent to an already verified account")
	}
}
