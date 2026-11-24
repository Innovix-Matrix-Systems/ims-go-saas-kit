package authn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
)

// AccountWriter is what account recovery needs to change on a user
// (implemented by the identity repository; keeps authn free of identity).
type AccountWriter interface {
	SetPassword(ctx context.Context, userID int64, hash string) error
	MarkEmailVerified(ctx context.Context, userID int64) error
	// AcceptInvitation sets the password, activates the account and
	// verifies the address of a user with a pending invitation; false when
	// there is none.
	AcceptInvitation(ctx context.Context, userID int64, hash string) (bool, error)
}

// Recovery implements "forgot password" and e-mail verification, Laravel
// style: a random token is mailed as a link, only its SHA-256 lives in the
// cache, and it works once. Responses never reveal whether an e-mail exists.
type Recovery struct {
	users       UserLookup
	writer      AccountWriter
	auth        Authenticator
	store       cache.Store
	deliverer   Deliverer
	invitations InvitationAudit
	emails      EmailWriter
	emailAudit  EmailChangeAudit
	cfg         config.Auth
	logger      *slog.Logger
}

// NewRecovery creates the account recovery service.
func NewRecovery(users UserLookup, writer AccountWriter, auth Authenticator, store cache.Store, deliverer Deliverer, cfg config.Auth, logger *slog.Logger) *Recovery {
	return &Recovery{users: users, writer: writer, auth: auth, store: store, deliverer: deliverer, cfg: cfg, logger: logger}
}

// resendGap stops one address from being mail-bombed through the public
// endpoints (the per-IP rate limit still applies on top).
const resendGap = time.Minute

type recoveryTicket struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
}

// SendPasswordResetLink mails a reset link when the e-mail belongs to an
// active account. It returns nil for unknown e-mails too (no enumeration).
func (r *Recovery) SendPasswordResetLink(ctx context.Context, email string) error {
	user, ok := r.findActive(ctx, email)
	if !ok || !r.firstInWindow(ctx, "pwreset:sent:"+normalizeEmail(email)) {
		return nil
	}
	token, err := r.issue(ctx, "pwreset", user, r.cfg.PasswordResetExpiry)
	if err != nil {
		return err
	}
	return r.send(ctx, user, TemplatePasswordReset, fillLink(r.cfg.PasswordResetURL, token, user.Email), r.cfg.PasswordResetExpiry)
}

// ResetPassword sets a new password with a valid reset token and signs the
// user out everywhere.
func (r *Recovery) ResetPassword(ctx context.Context, email, token, password string) error {
	// Check the new password before using the link up.
	if err := ValidateNewPassword(password); err != nil {
		return err
	}
	ticket, err := r.redeem(ctx, "pwreset", token)
	if err != nil || !strings.EqualFold(ticket.Email, strings.TrimSpace(email)) {
		return apperr.Validation(map[string][]string{"token": {"This password reset link is invalid or has expired."}})
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), r.cfg.BcryptRounds)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "hash password", err)
	}
	if err := r.writer.SetPassword(ctx, ticket.UserID, string(hash)); err != nil {
		return err
	}
	// Proving control of the mailbox also verifies the address.
	_ = r.writer.MarkEmailVerified(ctx, ticket.UserID)
	return r.auth.RevokeAll(ctx, ticket.UserID)
}

// SendVerificationLink mails an e-mail verification link to an unverified
// account (silently does nothing otherwise).
func (r *Recovery) SendVerificationLink(ctx context.Context, email string) error {
	user, err := r.users.FindByEmail(ctx, strings.TrimSpace(email))
	if err != nil || user.EmailVerified || !r.firstInWindow(ctx, "emailverify:sent:"+normalizeEmail(email)) {
		return nil
	}
	return r.sendVerification(ctx, user)
}

// SendVerificationTo mails a verification link to a known user (used when an
// admin creates an unverified account).
func (r *Recovery) SendVerificationTo(ctx context.Context, userID int64) error {
	user, err := r.users.FindByID(ctx, userID)
	if err != nil || user.EmailVerified {
		return err
	}
	return r.sendVerification(ctx, user)
}

// VerifyEmail marks the token's account as verified.
func (r *Recovery) VerifyEmail(ctx context.Context, token string) error {
	ticket, err := r.redeem(ctx, "emailverify", token)
	if err != nil {
		return apperr.Validation(map[string][]string{"token": {"This verification link is invalid or has expired."}})
	}
	return r.writer.MarkEmailVerified(ctx, ticket.UserID)
}

func (r *Recovery) sendVerification(ctx context.Context, user *UserRecord) error {
	token, err := r.issue(ctx, "emailverify", user, r.cfg.EmailVerifyExpiry)
	if err != nil {
		return err
	}
	return r.send(ctx, user, TemplateEmailVerify, fillLink(r.cfg.EmailVerifyURL, token, user.Email), r.cfg.EmailVerifyExpiry)
}

func (r *Recovery) findActive(ctx context.Context, email string) (*UserRecord, bool) {
	user, err := r.users.FindByEmail(ctx, strings.TrimSpace(email))
	if err != nil || (r.cfg.CheckIsActive && !user.IsActive) {
		return nil, false
	}
	return user, true
}

// issue stores a fresh one-time token. A newer token replaces the previous
// one for the same user and purpose.
func (r *Recovery) issue(ctx context.Context, purpose string, user *UserRecord, ttl time.Duration) (string, error) {
	token, err := randomSecret()
	if err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "generate token", err)
	}
	userKey := fmt.Sprintf("%s:user:%d", purpose, user.ID)
	var previous string
	if found, _ := r.store.Get(ctx, userKey, &previous); found && previous != "" {
		_ = r.store.Delete(ctx, purpose+":token:"+previous)
	}
	hash := hashToken(token)
	if err := r.store.Set(ctx, purpose+":token:"+hash, recoveryTicket{UserID: user.ID, Email: user.Email}, ttl); err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "store token", err)
	}
	_ = r.store.Set(ctx, userKey, hash, ttl)
	return token, nil
}

// redeem returns the ticket for a token and deletes it (single use).
func (r *Recovery) redeem(ctx context.Context, purpose, token string) (*recoveryTicket, error) {
	key := purpose + ":token:" + hashToken(strings.TrimSpace(token))
	var ticket recoveryTicket
	found, err := r.store.Get(ctx, key, &ticket)
	if err != nil || !found {
		return nil, apperr.New(apperr.InvalidInput, "invalid token")
	}
	_ = r.store.Delete(ctx, key)
	_ = r.store.Delete(ctx, fmt.Sprintf("%s:user:%d", purpose, ticket.UserID))
	return &ticket, nil
}

func (r *Recovery) firstInWindow(ctx context.Context, key string) bool {
	created, err := r.store.SetNX(ctx, key, 1, resendGap)
	return err != nil || created // a cache outage must not block recovery
}

// send mails a recovery link through the delivery system.
func (r *Recovery) send(ctx context.Context, user *UserRecord, template, link string, expires time.Duration) error {
	if r.deliverer == nil {
		r.logger.Warn("account e-mail not sent: no deliverer configured", "template", template)
		return nil
	}
	err := r.deliverer.DeliverAccount(ctx, AccountMessage{
		User:     user,
		Template: template,
		Data:     map[string]any{"link": link, "minutes": int(expires / time.Minute)},
	})
	if err != nil {
		return apperr.Wrap(apperr.ServiceUnavailable, "The e-mail could not be sent.", err)
	}
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func fillLink(template, token, email string) string {
	link := strings.ReplaceAll(template, "{token}", url.QueryEscape(token))
	return strings.ReplaceAll(link, "{email}", url.QueryEscape(email))
}
