package authn

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/crypto/bcrypt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
)

// Invitations. An admin creates a user inactive and invites
// them; the invitation is a single-use link, like a password reset, and
// setting a password from it activates the account and verifies the
// address. The invitee then signs in normally, so every sign-in check (and
// the two-factor policy) applies.

// TemplateInvitation is the invitation mail's delivery template.
const TemplateInvitation = "invitation"

const invitePurpose = "invite"

// InvitationAudit records accepted invitations (implemented by
// activity.Recorder).
type InvitationAudit interface {
	InvitationAccepted(ctx context.Context, user *UserRecord)
}

// WithInvitationAudit records accepted invitations in the activity log.
func (r *Recovery) WithInvitationAudit(audit InvitationAudit) *Recovery {
	r.invitations = audit
	return r
}

// SendInvitation mails an invitation link to a user; a newer invitation
// replaces the previous link. inviter is the name shown in the mail.
func (r *Recovery) SendInvitation(ctx context.Context, userID int64, inviter string) (err error) {
	ctx, end := tracing.Begin(ctx, "authn.send_invitation", attribute.Int64("user.id", userID))
	defer end(&err)
	user, err := r.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	token, err := r.issue(ctx, invitePurpose, user, r.cfg.InvitationTTL)
	if err != nil {
		return err
	}
	if r.deliverer == nil {
		r.logger.Warn("invitation not sent: no deliverer configured", "user_id", userID)
		return nil
	}
	err = r.deliverer.DeliverAccount(ctx, AccountMessage{
		User:     user,
		Template: TemplateInvitation,
		Data: map[string]any{
			"link":    fillLink(r.cfg.InvitationURL, token, user.Email),
			"minutes": int(r.cfg.InvitationTTL.Minutes()),
			"inviter": inviter,
		},
	})
	if err != nil {
		return apperr.Wrap(apperr.ServiceUnavailable, "The invitation could not be sent.", err)
	}
	return nil
}

// AcceptInvitation sets the invitee's password from a valid link: the
// account becomes active and its address verified. The link works once, and
// only while the invitation is pending, so an old link cannot bring back a
// user an admin deactivated after they joined.
func (r *Recovery) AcceptInvitation(ctx context.Context, email, token, password string) (err error) {
	ctx, end := tracing.Begin(ctx, "authn.accept_invitation")
	defer end(&err)
	invalid := apperr.Validation(map[string][]string{"token": {"This invitation link is invalid or has expired."}})
	if err := ValidateNewPassword(password); err != nil {
		return err
	}
	// Check the address before using the link up: a typo must not cost the
	// invitee a link that is meant to last days.
	var peeked recoveryTicket
	found, err := r.store.Get(ctx, invitePurpose+":token:"+hashToken(strings.TrimSpace(token)), &peeked)
	if err != nil || !found || !strings.EqualFold(peeked.Email, strings.TrimSpace(email)) {
		return invalid
	}
	ticket, err := r.redeem(ctx, invitePurpose, token)
	if err != nil {
		return invalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), r.cfg.BcryptRounds)
	if err != nil {
		return apperr.Wrap(apperr.InternalServerError, "hash password", err)
	}
	accepted, err := r.writer.AcceptInvitation(ctx, ticket.UserID, string(hash))
	if err != nil {
		return err
	}
	if !accepted {
		return invalid
	}
	if r.invitations != nil {
		if user, err := r.users.FindByID(ctx, ticket.UserID); err == nil {
			r.invitations.InvitationAccepted(ctx, user)
		}
	}
	return nil
}
