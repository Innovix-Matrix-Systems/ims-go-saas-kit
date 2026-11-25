package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn/social"
)

// SocialLoginRequest is a sign-in proven by an outside provider. Which
// fields are needed depends on the provider: an id_token for Google, a code
// (with the PKCE verifier and redirect_uri the client used) for GitHub.
type SocialLoginRequest struct {
	IDToken      string `json:"id_token" validate:"omitempty,max=8192"`
	Code         string `json:"code" validate:"omitempty,max=512"`
	CodeVerifier string `json:"code_verifier" validate:"omitempty,min=43,max=128"`
	RedirectURI  string `json:"redirect_uri" validate:"omitempty,url,max=2048"`
	Device       string `json:"device" validate:"required,max=100"`
}

type socialProvider struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

func (h *Handler) socialProviders(w http.ResponseWriter, r *http.Request) {
	providers := []socialProvider{}
	for _, provider := range h.service.SocialProviders() {
		providers = append(providers, socialProvider{Name: provider.Name(), DisplayName: provider.DisplayName()})
	}
	h.Data(w, r, http.StatusOK, providers)
}

func (h *Handler) socialLogin(w http.ResponseWriter, r *http.Request) {
	var req SocialLoginRequest
	if !h.Bind(w, r, &req) {
		return
	}
	proof := social.Proof{IDToken: req.IDToken, Code: req.Code, CodeVerifier: req.CodeVerifier, RedirectURI: req.RedirectURI}
	result, err := h.service.SocialLogin(r.Context(), chi.URLParam(r, "provider"), proof, req.Device, clientIP(r))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.respondLogin(w, r, result)
}
