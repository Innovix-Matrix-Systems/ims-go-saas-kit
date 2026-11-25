package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/crypto"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// Two-factor authentication. A user has one method:
//
//   - totp: an authenticator app (RFC 6238); the secret is encrypted with
//     APP_KEY and each code works once (last_step);
//   - email: a code mailed at sign-in through the delivery system.
//
// Either way the user also gets recovery codes, each usable once, for a lost
// phone or mailbox. Sign-in asks for the second factor after the password
// (Service.Login → OTP_REQUIRED → Service.VerifyOTP).

// Two-factor methods.
const (
	TwoFactorTOTP  = "totp"
	TwoFactorEmail = "email"
)

// Delivery templates for two-factor security mails.
const (
	TemplateTwoFactorEnabled  = "two-factor-enabled"
	TemplateTwoFactorDisabled = "two-factor-disabled"
	TemplateRecoveryCodeUsed  = "recovery-code-used"
)

const recoveryCodeCount = 8

// TwoFactorStatus is a user's two-factor state.
type TwoFactorStatus struct {
	Enabled           bool       `json:"enabled"`
	Method            *string    `json:"method"`
	Pending           bool       `json:"pending"` // enrolment started, not confirmed
	ConfirmedAt       *time.Time `json:"confirmed_at"`
	RecoveryCodesLeft int        `json:"recovery_codes_left"`
}

// TwoFactorSetup is what starting an enrolment returns. For totp the secret
// and the otpauth:// URL (render it as a QR code); for email a code was
// mailed.
type TwoFactorSetup struct {
	Method     string `json:"method"`
	Secret     string `json:"secret,omitempty"`
	OTPAuthURL string `json:"otpauth_url,omitempty"`
}

type recoveryCode struct {
	Hash   string     `json:"hash"`
	UsedAt *time.Time `json:"used_at,omitempty"`
}

// Two-factor activity actions.
const (
	ActionTwoFactorEnabled  = "user.two_factor_enabled"
	ActionTwoFactorDisabled = "user.two_factor_disabled"
	ActionTwoFactorReset    = "user.two_factor_reset"
)

// TwoFactorAudit records two-factor changes (implemented by
// activity.Recorder).
type TwoFactorAudit interface {
	TwoFactorChanged(ctx context.Context, user *UserRecord, action, method string)
}

// TwoFactor enrols, verifies and resets second factors.
type TwoFactor struct {
	queries   *dbgen.Queries
	encrypter *crypto.Encrypter
	otp       *OTPService
	deliverer Deliverer
	audit     TwoFactorAudit
	issuer    string
	now       func() time.Time
}

// NewTwoFactor creates the two-factor service. issuer is the name
// authenticator apps show (APP_NAME).
func NewTwoFactor(queries *dbgen.Queries, encrypter *crypto.Encrypter, otp *OTPService, issuer string) *TwoFactor {
	return &TwoFactor{queries: queries, encrypter: encrypter, otp: otp, issuer: issuer, now: time.Now}
}

// WithDeliverer sends the security mails (enabled, disabled, recovery code
// used).
func (t *TwoFactor) WithDeliverer(deliverer Deliverer) *TwoFactor {
	t.deliverer = deliverer
	return t
}

// WithAudit records enabling, disabling and resets in the activity log.
func (t *TwoFactor) WithAudit(audit TwoFactorAudit) *TwoFactor {
	t.audit = audit
	return t
}

// Status returns a user's two-factor state.
func (t *TwoFactor) Status(ctx context.Context, userID int64) (TwoFactorStatus, error) {
	row, found, err := t.get(ctx, userID)
	if err != nil || !found {
		return TwoFactorStatus{}, err
	}
	status := TwoFactorStatus{Method: &row.Method, Pending: !row.ConfirmedAt.Valid}
	if row.ConfirmedAt.Valid {
		confirmed := row.ConfirmedAt.Time
		status.Enabled, status.ConfirmedAt = true, &confirmed
		codes, _ := decodeRecoveryCodes(row.RecoveryCodes)
		for _, code := range codes {
			if code.UsedAt == nil {
				status.RecoveryCodesLeft++
			}
		}
	}
	return status, nil
}

// Method returns the user's confirmed method ("" when two-factor is off).
func (t *TwoFactor) Method(ctx context.Context, userID int64) (string, error) {
	row, found, err := t.get(ctx, userID)
	if err != nil || !found || !row.ConfirmedAt.Valid {
		return "", err
	}
	return row.Method, nil
}

// Begin starts an enrolment (replacing an unconfirmed one). Confirm finishes
// it with a code.
func (t *TwoFactor) Begin(ctx context.Context, user *UserRecord, method string) (_ *TwoFactorSetup, err error) {
	ctx, end := tracing.Begin(ctx, "authn.begin_two_factor", attribute.String("two_factor.method", method))
	defer end(&err)
	if method != TwoFactorTOTP && method != TwoFactorEmail {
		return nil, apperr.Validation(map[string][]string{"method": {"The method must be totp or email."}})
	}
	if method == TwoFactorEmail && (user.Email == "" || !user.EmailVerified) {
		return nil, apperr.Validation(map[string][]string{"method": {"Verify your e-mail address before using it for two-factor sign-in."}})
	}
	setup := &TwoFactorSetup{Method: method}
	var sealed *string
	if method == TwoFactorTOTP {
		secret, err := newTOTPSecret()
		if err != nil {
			return nil, apperr.Wrap(apperr.InternalServerError, "generate two-factor secret", err)
		}
		value, err := t.encrypter.EncryptString(secret)
		if err != nil {
			return nil, apperr.Wrap(apperr.InternalServerError, "encrypt two-factor secret", err)
		}
		sealed = &value
		setup.Secret, setup.OTPAuthURL = secret, totpURL(t.issuer, user.Email, secret)
	}
	_, err = t.queries.StartTwoFactor(ctx, dbgen.StartTwoFactorParams{UserID: user.ID, Method: method, Secret: sealed})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.New(apperr.InvalidState, "Two-factor authentication is already on. Turn it off first to change the method.")
	}
	if err != nil {
		return nil, database.MapError("authn.start_two_factor", err)
	}
	if method == TwoFactorEmail {
		if _, err := t.otp.sendTo(ctx, user, enrolmentKey(user.ID)); err != nil {
			return nil, err
		}
	}
	return setup, nil
}

// Confirm finishes an enrolment with a code from the app (totp) or the mail
// (email) and returns the recovery codes, shown once.
func (t *TwoFactor) Confirm(ctx context.Context, user *UserRecord, code string) (_ []string, err error) {
	ctx, end := tracing.Begin(ctx, "authn.confirm_two_factor")
	defer end(&err)
	row, found, err := t.get(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if !found || row.ConfirmedAt.Valid {
		return nil, apperr.New(apperr.InvalidState, "There is no two-factor enrolment to confirm. Start one first.")
	}
	switch row.Method {
	case TwoFactorTOTP:
		if err := t.acceptTOTP(ctx, row, code); err != nil {
			return nil, err
		}
	default:
		if err := t.otp.verifyAt(ctx, enrolmentKey(user.ID), code); err != nil {
			return nil, err
		}
	}
	codes, hashed, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	affected, err := t.queries.ConfirmTwoFactor(ctx, dbgen.ConfirmTwoFactorParams{UserID: user.ID, RecoveryCodes: hashed})
	if err != nil {
		return nil, database.MapError("authn.confirm_two_factor", err)
	}
	if affected == 0 {
		return nil, apperr.New(apperr.InvalidState, "There is no two-factor enrolment to confirm. Start one first.")
	}
	t.mail(ctx, user, TemplateTwoFactorEnabled, map[string]any{"method": row.Method})
	if t.audit != nil {
		t.audit.TwoFactorChanged(ctx, user, ActionTwoFactorEnabled, row.Method)
	}
	return codes, nil
}

// RegenerateRecoveryCodes replaces the recovery codes (the old ones stop
// working) and returns the new ones, shown once.
func (t *TwoFactor) RegenerateRecoveryCodes(ctx context.Context, user *UserRecord) (_ []string, err error) {
	ctx, end := tracing.Begin(ctx, "authn.regenerate_recovery_codes")
	defer end(&err)
	codes, hashed, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if method, err := t.Method(ctx, user.ID); err != nil {
		return nil, err
	} else if method == "" {
		return nil, apperr.New(apperr.InvalidState, "Two-factor authentication is off.")
	}
	if _, err := t.queries.SetTwoFactorRecoveryCodes(ctx, dbgen.SetTwoFactorRecoveryCodesParams{UserID: user.ID, RecoveryCodes: hashed}); err != nil {
		return nil, database.MapError("authn.set_recovery_codes", err)
	}
	return codes, nil
}

// Disable turns two-factor off. reset is true when an administrator did it
// (the security mail says so).
func (t *TwoFactor) Disable(ctx context.Context, user *UserRecord, reset bool) (err error) {
	ctx, end := tracing.Begin(ctx, "authn.disable_two_factor", attribute.Bool("two_factor.reset", reset))
	defer end(&err)
	method, err := t.Method(ctx, user.ID)
	if err != nil {
		return err
	}
	affected, err := t.queries.DeleteTwoFactor(ctx, user.ID)
	if err != nil {
		return database.MapError("authn.disable_two_factor", err)
	}
	if affected == 0 {
		return apperr.New(apperr.InvalidState, "Two-factor authentication is off.")
	}
	t.mail(ctx, user, TemplateTwoFactorDisabled, map[string]any{"reset": reset})
	if t.audit != nil {
		action := ActionTwoFactorDisabled
		if reset {
			action = ActionTwoFactorReset
		}
		t.audit.TwoFactorChanged(ctx, user, action, method)
	}
	return nil
}

// Verify checks a sign-in's second factor: a code from the app (totp), the
// code mailed at sign-in (email), or a recovery code (either method).
func (t *TwoFactor) Verify(ctx context.Context, user *UserRecord, code string) (err error) {
	ctx, end := tracing.Begin(ctx, "authn.verify_two_factor")
	defer end(&err)
	row, found, err := t.get(ctx, user.ID)
	if err != nil {
		return err
	}
	if !found || !row.ConfirmedAt.Valid {
		return apperr.New(apperr.OTPInvalid, "The code is invalid.")
	}
	if looksLikeRecoveryCode(code) {
		return t.useRecoveryCode(ctx, user, row, code)
	}
	if row.Method == TwoFactorTOTP {
		return t.acceptTOTP(ctx, row, code)
	}
	return t.otp.Verify(ctx, user.ID, code)
}

// SendSignInCode mails the sign-in code of an email-method user.
func (t *TwoFactor) SendSignInCode(ctx context.Context, user *UserRecord) (err error) {
	ctx, end := tracing.Begin(ctx, "authn.send_two_factor_code")
	defer end(&err)
	_, err = t.otp.Send(ctx, user)
	return err
}

// acceptTOTP verifies a TOTP code and records its time step, so the same
// code cannot be replayed (even by a concurrent request).
func (t *TwoFactor) acceptTOTP(ctx context.Context, row dbgen.UserTwoFactor, code string) error {
	if row.Secret == nil {
		return apperr.New(apperr.OTPInvalid, "The code is invalid.")
	}
	secret, err := t.encrypter.DecryptString(*row.Secret)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "decrypt two-factor secret", err)
	}
	step := totpMatch(secret, code, t.now())
	if step == 0 {
		return apperr.New(apperr.OTPInvalid, "The code is invalid.")
	}
	accepted, err := t.queries.AcceptTwoFactorStep(ctx, dbgen.AcceptTwoFactorStepParams{UserID: row.UserID, Step: step})
	if err != nil {
		return database.MapError("authn.accept_two_factor_step", err)
	}
	if accepted == 0 {
		return apperr.New(apperr.OTPInvalid, "This code was already used. Wait for the next one.")
	}
	if t.encrypter.NeedsRotation(*row.Secret) { // APP_KEY rotated: re-seal with the new key
		if fresh, err := t.encrypter.EncryptString(secret); err == nil {
			_ = t.queries.SetTwoFactorSecret(ctx, dbgen.SetTwoFactorSecretParams{UserID: row.UserID, Secret: &fresh})
		}
	}
	return nil
}

// useRecoveryCode marks one unused recovery code used. The write is
// conditional on the codes being the ones read: when a concurrent sign-in got
// there first, it reads them again, so a code works once and another code's
// mark is never lost.
func (t *TwoFactor) useRecoveryCode(ctx context.Context, user *UserRecord, row dbgen.UserTwoFactor, code string) error {
	hash := hashRecoveryCode(code)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			fresh, found, err := t.get(ctx, user.ID)
			if err != nil {
				return err
			}
			if !found || !fresh.ConfirmedAt.Valid {
				return apperr.New(apperr.OTPInvalid, "The code is invalid.")
			}
			row = fresh
		}
		codes, err := decodeRecoveryCodes(row.RecoveryCodes)
		if err != nil {
			return apperr.Wrap(apperr.InternalServerError, "read recovery codes", err)
		}
		used, left := -1, 0
		for i := range codes {
			if codes[i].UsedAt != nil {
				continue
			}
			if used < 0 && subtle.ConstantTimeCompare([]byte(codes[i].Hash), []byte(hash)) == 1 {
				used = i
				continue
			}
			left++
		}
		if used < 0 {
			return apperr.New(apperr.OTPInvalid, "The recovery code is invalid or was already used.")
		}
		now := t.now().UTC()
		codes[used].UsedAt = &now
		encoded, _ := json.Marshal(codes)
		affected, err := t.queries.UseTwoFactorRecoveryCode(ctx, dbgen.UseTwoFactorRecoveryCodeParams{
			UserID: user.ID, RecoveryCodes: encoded, Previous: row.RecoveryCodes,
		})
		if err != nil {
			return database.MapError("authn.use_recovery_code", err)
		}
		if affected == 1 {
			t.mail(ctx, user, TemplateRecoveryCodeUsed, map[string]any{"left": left})
			return nil
		}
	}
	return apperr.New(apperr.OTPInvalid, "The recovery code could not be used. Try again.")
}

func (t *TwoFactor) get(ctx context.Context, userID int64) (dbgen.UserTwoFactor, bool, error) {
	row, err := t.queries.GetTwoFactor(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.UserTwoFactor{}, false, nil
	}
	if err != nil {
		return dbgen.UserTwoFactor{}, false, database.MapError("authn.get_two_factor", err)
	}
	return row, true, nil
}

// mail sends a security mail; it never fails the change it reports.
func (t *TwoFactor) mail(ctx context.Context, user *UserRecord, template string, data map[string]any) {
	if t.deliverer == nil || user.Email == "" {
		return
	}
	data["when"] = t.now().UTC().Format("2 Jan 2006, 15:04 UTC")
	_ = t.deliverer.DeliverAccount(ctx, AccountMessage{User: user, Template: template, Data: data})
}

func enrolmentKey(userID int64) string { return fmt.Sprintf("2fa:enrol:%d", userID) }

// Recovery codes look like "k7p2m-x9q4w": 10 characters of an alphabet
// without look-alikes, shown once and stored as SHA-256 hashes.
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func newRecoveryCodes() ([]string, []byte, error) {
	plain := make([]string, recoveryCodeCount)
	hashed := make([]recoveryCode, recoveryCodeCount)
	for i := range plain {
		raw := make([]byte, 10)
		for j := range raw {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(recoveryAlphabet))))
			if err != nil {
				return nil, nil, apperr.Wrap(apperr.InternalServerError, "generate recovery codes", err)
			}
			raw[j] = recoveryAlphabet[n.Int64()]
		}
		plain[i] = string(raw[:5]) + "-" + string(raw[5:])
		hashed[i] = recoveryCode{Hash: hashRecoveryCode(plain[i])}
	}
	encoded, err := json.Marshal(hashed)
	if err != nil {
		return nil, nil, apperr.Wrap(apperr.InternalServerError, "encode recovery codes", err)
	}
	return plain, encoded, nil
}

func hashRecoveryCode(code string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// looksLikeRecoveryCode tells a recovery code ("k7p2m-x9q4w") from a
// six-digit code, so one field accepts both.
func looksLikeRecoveryCode(code string) bool {
	return len(strings.ReplaceAll(strings.TrimSpace(code), "-", "")) == 10
}

func decodeRecoveryCodes(raw []byte) ([]recoveryCode, error) {
	var codes []recoveryCode
	if len(raw) == 0 {
		return codes, nil
	}
	err := json.Unmarshal(raw, &codes)
	return codes, err
}
