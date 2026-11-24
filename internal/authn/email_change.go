package authn

import (
	"context"
	"net/mail"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

// Mail templates for changing one's own e-mail address.
const (
	// TemplateEmailChange goes to the new address with the confirmation link.
	TemplateEmailChange = "email-change"
	// TemplateEmailChanged tells the old address that the change happened.
	TemplateEmailChanged = "email-changed"
)

const emailChangePurpose = "emailchange"

// EmailWriter is what an e-mail change needs (implemented by the identity
// repository).
type EmailWriter interface {
	EmailExists(ctx context.Context, email string, excludeID int64) (bool, error)
	// ChangeEmail sets the address and marks it verified.
	ChangeEmail(ctx context.Context, userID int64, email string) error
}

// EmailChangeAudit records a confirmed e-mail change (the activity log).
type EmailChangeAudit interface {
	EmailChanged(ctx context.Context, user *UserRecord, oldEmail, newEmail string)
}

// WithEmailChangeAudit records confirmed e-mail changes.
func (r *Recovery) WithEmailChangeAudit(audit EmailChangeAudit) *Recovery {
	r.emailAudit = audit
	return r
}

// WithEmailChange enables changing one's own e-mail address.
func (r *Recovery) WithEmailChange(writer EmailWriter) *Recovery {
	r.emails = writer
	return r
}

// RequestEmailChange checks the password and mails a confirmation link to the
// new address. Nothing changes until the link is opened, so a typo cannot
// lock anyone out of their account.
func (r *Recovery) RequestEmailChange(ctx context.Context, userID int64, password, newEmail string) error {
	if r.emails == nil {
		return apperr.New(apperr.OperationNotAllowed, "Changing the e-mail address is not available.")
	}
	user, err := r.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		return apperr.Validation(map[string][]string{"password": {"The password is incorrect."}})
	}
	newEmail = strings.TrimSpace(newEmail)
	if _, err := mail.ParseAddress(newEmail); err != nil || len(newEmail) > 255 {
		return apperr.Validation(map[string][]string{"email": {"Enter a valid e-mail address."}})
	}
	if strings.EqualFold(newEmail, user.Email) {
		return apperr.Validation(map[string][]string{"email": {"This is already your e-mail address."}})
	}
	if taken, err := r.emails.EmailExists(ctx, newEmail, user.ID); err != nil {
		return err
	} else if taken {
		return apperr.Validation(map[string][]string{"email": {"This e-mail address is already in use."}})
	}
	if !r.firstInWindow(ctx, emailChangePurpose+":sent:"+normalizeEmail(user.Email)) {
		return apperr.New(apperr.RateLimitExceeded, "A confirmation link was just sent. Wait a minute before asking for another.").WithRetryAfter(60)
	}
	// The ticket carries the new address; the mail goes there.
	target := *user
	target.Email = newEmail
	token, err := r.issue(ctx, emailChangePurpose, &target, r.cfg.EmailVerifyExpiry)
	if err != nil {
		return err
	}
	return r.send(ctx, &target, TemplateEmailChange, fillLink(r.cfg.EmailChangeURL, token, newEmail), r.cfg.EmailVerifyExpiry)
}

// ConfirmEmailChange switches the account to the address the link was sent
// to, tells the old address, and signs the user out everywhere (they sign in
// again with the new address).
func (r *Recovery) ConfirmEmailChange(ctx context.Context, token string) error {
	invalid := apperr.Validation(map[string][]string{"token": {"This confirmation link is invalid or has expired."}})
	if r.emails == nil {
		return invalid
	}
	ticket, err := r.redeem(ctx, emailChangePurpose, token)
	if err != nil {
		return invalid
	}
	user, err := r.users.FindByID(ctx, ticket.UserID)
	if err != nil {
		return invalid
	}
	if taken, err := r.emails.EmailExists(ctx, ticket.Email, user.ID); err != nil {
		return err
	} else if taken {
		return apperr.Validation(map[string][]string{"token": {"This e-mail address has been taken by another account in the meantime."}})
	}
	oldEmail := user.Email
	if err := r.emails.ChangeEmail(ctx, user.ID, ticket.Email); err != nil {
		return err
	}
	if r.emailAudit != nil {
		r.emailAudit.EmailChanged(ctx, user, oldEmail, ticket.Email)
	}
	if r.deliverer != nil {
		old := *user
		old.Email = oldEmail
		if err := r.deliverer.DeliverAccount(ctx, AccountMessage{
			User: &old, Template: TemplateEmailChanged,
			Data: map[string]any{"new_email": MaskEmail(ticket.Email)},
		}); err != nil {
			r.logger.Warn("e-mail change: notice to the old address failed", "user_id", user.ID, "error", err)
		}
	}
	return r.auth.RevokeAll(ctx, user.ID)
}

// MaskEmail shows enough of an address to recognize it: "na***@acme.test".
func MaskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	local := email[:at]
	keep := 2
	if len(local) <= 2 {
		keep = 1
	}
	return local[:keep] + "***" + email[at:]
}

// PeekInvitation reports which user a pending invitation link belongs to,
// without using it up. The e-mail must match the invited address.
func (r *Recovery) PeekInvitation(ctx context.Context, email, token string) (int64, error) {
	var ticket recoveryTicket
	found, err := r.store.Get(ctx, invitePurpose+":token:"+hashToken(strings.TrimSpace(token)), &ticket)
	if err != nil || !found || !strings.EqualFold(ticket.Email, strings.TrimSpace(email)) {
		return 0, apperr.Validation(map[string][]string{"token": {"This invitation link is invalid or has expired."}})
	}
	return ticket.UserID, nil
}
