package onboarding

import (
	"math"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/ratelimit"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// signUpAccepted is the one answer to every accepted sign-up.
const signUpAccepted = "Check your e-mail: we sent you a link to confirm your address and finish creating your organization."

// Handler serves sign-up and onboarding progress.
type Handler struct {
	service *Service
	// limiter caps sign-up attempts per client IP (nil: no extra limit).
	limiter ratelimit.Limiter
	httpx.Kit
}

// NewHandler creates the handler.
func NewHandler(service *Service, limiter ratelimit.Limiter, kit httpx.Kit) *Handler {
	return &Handler{service: service, limiter: limiter, Kit: kit}
}

func (h *Handler) signUp(w http.ResponseWriter, r *http.Request) {
	if h.limiter != nil {
		if allowed, retry := h.limiter.Allow(r.Context(), "signup:ip:"+httpx.ClientIP(r.Context())); !allowed {
			h.Error(w, r, apperr.New(apperr.RateLimitExceeded, "Too many sign-up attempts. Please try again later.").
				WithRetryAfter(int(math.Ceil(retry.Seconds()))))
			return
		}
	}
	var req SignUpRequest
	if !h.Bind(w, r, &req) {
		return
	}
	err := h.service.SignUp(r.Context(), SignUpInput{
		Name: req.Name, Email: req.Email, Password: req.Password, OrganizationName: req.OrganizationName,
		Phone: req.Phone, Locale: req.Locale, AcceptTerms: req.AcceptTerms,
		IPAddress: httpx.ClientIP(r.Context()), UserAgent: httpx.UserAgent(r.Context()),
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusAccepted, SignUpResponse{Message: signUpAccepted})
}

func (h *Handler) progress(w http.ResponseWriter, r *http.Request) {
	progress, err := h.service.Progress(r.Context(), tenancy.From(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewProgressResponse(*progress, authn.UserID(r)))
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	var req CompleteRequest
	if !h.Bind(w, r, &req) {
		return
	}
	progress, err := h.service.Complete(r.Context(), tenancy.From(r), authn.UserID(r), req.SkippedSteps)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, NewProgressResponse(*progress, authn.UserID(r)))
}
