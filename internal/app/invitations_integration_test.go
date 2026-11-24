package app_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// Invite a user: an admin creates the user inactive with
// send_invitation; the invitee sets a password from the mailed link, which
// activates the account; then they sign in normally (docs/guides/authentication.md).

type invitedUser struct {
	ID               int64   `json:"id"`
	IsActive         bool    `json:"is_active"`
	InvitationStatus string  `json:"invitation_status"`
	EmailVerifiedAt  *string `json:"email_verified_at"`
}

func inviteUser(t *testing.T, app *testkit.App, admin *testkit.User, email string) invitedUser {
	t.Helper()
	var user invitedUser
	app.As(admin).Post("/api/v1/user", map[string]any{
		"name": "Nadia Islam", "email": email, "send_invitation": true,
		"branch_id": admin.BranchID,
	}).Status(t, http.StatusCreated).Data(t, &user)
	return user
}

// invitationToken reads the token from the latest invitation link.
func invitationToken(t *testing.T, app *testkit.App) string {
	t.Helper()
	link, _ := app.Queued(t, "invitation")["link"].(string)
	parsed, err := url.Parse(link)
	if err != nil || parsed.Query().Get("token") == "" {
		t.Fatalf("invitation link = %q", link)
	}
	return parsed.Query().Get("token")
}

func acceptInvitation(app *testkit.App, email, token, password string) *testkit.Response {
	return app.Guest().Post("/api/v1/invitation/accept", map[string]any{
		"email": email, "token": token, "password": password, "password_confirmation": password,
	})
}

func TestInvitedUserSetsAPasswordAndJoins(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	user := inviteUser(t, app, admin, "nadia@acme.test")
	if user.IsActive || user.InvitationStatus != "pending" || user.EmailVerifiedAt != nil {
		t.Fatalf("invited user = %+v, want inactive, pending and unverified", user)
	}
	if inviter, _ := app.Queued(t, "invitation")["inviter"].(string); inviter == "" {
		t.Fatal("the invitation does not name the inviter")
	}

	// Nobody can sign in before the invitation is accepted.
	app.Guest().Post("/api/v1/login", map[string]any{"email": "nadia@acme.test", "password": testkit.Password, "device": "web"}).
		Status(t, http.StatusUnauthorized)

	token := invitationToken(t, app)
	requireErrorCode(t, acceptInvitation(app, "someone@else.test", token, "a-new-password"), http.StatusUnprocessableEntity, "VALIDATION_FAILED")
	acceptInvitation(app, "nadia@acme.test", token, "a-new-password").Status(t, http.StatusOK)
	requireErrorCode(t, acceptInvitation(app, "nadia@acme.test", token, "another-password"), http.StatusUnprocessableEntity, "VALIDATION_FAILED")

	app.Guest().Post("/api/v1/login", map[string]any{"email": "nadia@acme.test", "password": "a-new-password", "device": "web"}).
		Status(t, http.StatusOK)
	var joined invitedUser
	app.As(admin).Get("/api/v1/user/"+strconv.FormatInt(user.ID, 10)).Status(t, http.StatusOK).Data(t, &joined)
	if !joined.IsActive || joined.InvitationStatus != "accepted" || joined.EmailVerifiedAt == nil {
		t.Fatalf("joined user = %+v, want active, accepted and verified", joined)
	}

	for action, want := range map[string]int{"user.invited": 1, "user.invitation_accepted": 1} {
		if n := countRows(t, app, `SELECT count(*) FROM activity_log WHERE action = $1 AND subject_id = $2`, action, user.ID); n != want {
			t.Errorf("%s rows = %d, want %d", action, n, want)
		}
	}
}

func TestResendingAnInvitation(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer", authz.PermUserViewAll)))
	stranger := app.User(t, testkit.InTenant(app.Tenant(t, "Elsewhere")))
	user := inviteUser(t, app, admin, "late@acme.test")
	path := "/api/v1/user/" + strconv.FormatInt(user.ID, 10) + "/invitation"
	first := invitationToken(t, app)

	app.As(viewer).Post(path, nil).Status(t, http.StatusForbidden)
	app.As(stranger).Post(path, nil).Status(t, http.StatusNotFound)
	app.As(admin).Post(path, nil).Status(t, http.StatusOK)
	second := invitationToken(t, app)
	if second == first {
		t.Fatal("resending reused the old link")
	}
	requireErrorCode(t, acceptInvitation(app, "late@acme.test", first, "a-new-password"), http.StatusUnprocessableEntity, "VALIDATION_FAILED")
	acceptInvitation(app, "late@acme.test", second, "a-new-password").Status(t, http.StatusOK)

	// Once joined, there is nothing to resend.
	app.As(admin).Post(path, nil).Status(t, http.StatusConflict)
	// A user created with a password who signed in is not invited either.
	app.As(admin).Post("/api/v1/user/"+strconv.FormatInt(viewer.ID, 10)+"/invitation", nil).Status(t, http.StatusConflict)

	var pending []invitedUser
	inviteUser(t, app, admin, "pending@acme.test")
	app.As(admin).Get("/api/v1/user?invitation=pending").Status(t, http.StatusOK).Data(t, &pending)
	if len(pending) != 1 || pending[0].InvitationStatus != "pending" {
		t.Fatalf("pending invitations = %+v, want 1", pending)
	}
	app.As(admin).Get("/api/v1/user?invitation=maybe").Status(t, http.StatusUnprocessableEntity)
}

func TestAnInvitationCannotReactivateAUser(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	user := inviteUser(t, app, admin, "gone@acme.test")
	token := invitationToken(t, app)
	acceptInvitation(app, "gone@acme.test", token, "a-new-password").Status(t, http.StatusOK)

	// The admin deactivates the user; the user gets a new link somehow (an
	// old mail) — but the invitation was already accepted.
	if _, err := app.Pool.Exec(t.Context(), `UPDATE users SET is_active = 0 WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	requireErrorCode(t, acceptInvitation(app, "gone@acme.test", token, "x-new-password"), http.StatusUnprocessableEntity, "VALIDATION_FAILED")
	if n := countRows(t, app, `SELECT count(*) FROM users WHERE id = $1 AND is_active = 0`, user.ID); n != 1 {
		t.Fatal("the user was reactivated")
	}
}
