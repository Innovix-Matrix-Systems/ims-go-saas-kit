// Package auth serves the authentication HTTP endpoints (login, OTP, logout).
package auth

import (
	"context"
	"net"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/identity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// Handler serves authentication endpoints.
type Handler struct {
	service  *authn.Service
	recovery *authn.Recovery
	sessions *authn.Sessions
	users    *identity.Service
	checker  *authz.Checker
	signIns  SignInRecorder
	httpx.Kit
}

// DashboardDevice is the device name the developer dashboard signs in with;
// its sign-ins and sign-outs are written to the dashboard audit log.
const DashboardDevice = "dev-dashboard"

// SignInRecorder records dashboard sign-ins (implemented by the dev module's
// auditor; an interface keeps auth free of dev).
type SignInRecorder interface {
	SignIn(ctx context.Context, email string, userID int64, err error)
	SignOut(ctx context.Context, userID int64)
}

// WithSignInRecorder audits sign-ins made from the developer dashboard.
func (h *Handler) WithSignInRecorder(recorder SignInRecorder) *Handler {
	h.signIns = recorder
	return h
}

// NewHandler creates the auth handler.
func NewHandler(service *authn.Service, users *identity.Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, users: users, checker: checker, Kit: kit}
}

// WithRecovery enables the forgot-password and e-mail verification routes.
func (h *Handler) WithRecovery(recovery *authn.Recovery) *Handler {
	h.recovery = recovery
	return h
}

// WithSessions enables the self-service session routes.
func (h *Handler) WithSessions(sessions *authn.Sessions) *Handler {
	h.sessions = sessions
	return h
}

// EmailRequest carries just an e-mail address.
type EmailRequest struct {
	Email string `json:"email" validate:"required,email,max=255"`
}

// ResetPasswordRequest completes a password reset.
type ResetPasswordRequest struct {
	Email                string `json:"email" validate:"required,email,max=255"`
	Token                string `json:"token" validate:"required,max=200"`
	Password             string `json:"password" validate:"required,min=8,max=72"`
	PasswordConfirmation string `json:"password_confirmation" validate:"required,eqfield=Password"`
}

// AcceptInvitationRequest sets an invited user's password.
type AcceptInvitationRequest struct {
	Email                string `json:"email" validate:"required,email,max=255"`
	Token                string `json:"token" validate:"required,max=200"`
	Password             string `json:"password" validate:"required,min=8,max=72"`
	PasswordConfirmation string `json:"password_confirmation" validate:"required,eqfield=Password"`
}

// VerifyEmailRequest confirms an e-mail address.
type VerifyEmailRequest struct {
	Token string `json:"token" validate:"required,max=200"`
}

// sentMessage is deliberately the same whether or not the address exists.
const sentMessage = "If that e-mail address belongs to an account, a message is on its way."

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req EmailRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.recovery.SendPasswordResetLink(r.Context(), req.Email); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, sentMessage)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.recovery.ResetPassword(r.Context(), req.Email, req.Token, req.Password); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Your password has been reset. Please sign in again.")
}

func (h *Handler) resendVerification(w http.ResponseWriter, r *http.Request) {
	var req EmailRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.recovery.SendVerificationLink(r.Context(), req.Email); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, sentMessage)
}

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req VerifyEmailRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.recovery.VerifyEmail(r.Context(), req.Token); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Your e-mail address has been verified.")
}

// LoginRequest is the login payload.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,max=72"`
	Device   string `json:"device" validate:"required,max=100"`
}

// VerifyOTPRequest is the OTP completion payload.
type VerifyOTPRequest struct {
	Email  string `json:"email" validate:"required,email"`
	OTP    string `json:"otp" validate:"required,max=20"` // a 6-digit code or a recovery code
	Device string `json:"device" validate:"required,max=100"`
}

// LogoutRequest revokes one device token.
type LogoutRequest struct {
	Device string `json:"device" validate:"required,max=100"`
}

type loginData struct {
	User    identity.Response `json:"user"`
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Token   *string           `json:"token"`
	// Method is the second factor an OTP_REQUIRED sign-in waits for.
	Method string `json:"method,omitempty"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if !h.Bind(w, r, &req) {
		return
	}
	result, err := h.service.Login(r.Context(), req.Email, req.Password, req.Device, clientIP(r))
	if h.signIns != nil && req.Device == DashboardDevice {
		var userID int64
		if err == nil && result != nil && result.User != nil {
			userID = result.User.ID
		}
		h.signIns.SignIn(r.Context(), req.Email, userID, err)
	}
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondLogin(w, r, result)
}

func (h *Handler) verifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if !h.Bind(w, r, &req) {
		return
	}
	result, err := h.service.VerifyOTP(r.Context(), req.Email, req.OTP, req.Device, clientIP(r))
	if h.signIns != nil && req.Device == DashboardDevice {
		var userID int64
		if err == nil && result != nil && result.User != nil {
			userID = result.User.ID
		}
		h.signIns.SignIn(r.Context(), req.Email, userID, err)
	}
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondLogin(w, r, result)
}

// acceptInvitation sets the invitee's password; the account is then active
// and they sign in with POST /login like everyone else.
func (h *Handler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req AcceptInvitationRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.recovery.AcceptInvitation(r.Context(), req.Email, req.Token, req.Password); err != nil {
		h.Error(w, r, err)
		return
	}
	h.Message(w, r, http.StatusOK, "Your account is ready. Sign in with your new password.")
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	identity, _ := authn.IdentityFromContext(r.Context())
	var req LogoutRequest
	if !h.Bind(w, r, &req) {
		return
	}
	if err := h.service.Logout(r.Context(), identity.UserID, req.Device); err != nil {
		h.Error(w, r, err)
		return
	}
	if h.signIns != nil && req.Device == DashboardDevice {
		h.signIns.SignOut(r.Context(), identity.UserID)
	}
	h.NoContent(w)
}

func (h *Handler) respondLogin(w http.ResponseWriter, r *http.Request, result *authn.LoginResult) {
	user, err := h.users.GetRecord(r.Context(), result.User.ID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	roles, err := h.checker.RoleRefs(r.Context(), user.ID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	var token *string
	if result.Token != nil {
		token = &result.Token.PlainText
	}
	h.Data(w, r, http.StatusOK, loginData{
		User:    identity.NewResponse(*user, roles, nil),
		Status:  result.Status,
		Message: result.Message,
		Token:   token,
		Method:  result.Method,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
