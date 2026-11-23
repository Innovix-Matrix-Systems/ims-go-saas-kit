package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/mcp"
)

// Endpoint paths.
const (
	AuthorizePath   = "/oauth/authorize"
	TokenPath       = "/oauth/token"
	RevokePath      = "/oauth/revoke"
	ASMetadataPath  = "/.well-known/oauth-authorization-server"
	maxTokenRequest = 16 << 10
)

// The OAuth protocol endpoints speak OAuth's own JSON (RFC 6749 errors),
// not the API's problem documents, so they write their responses here
// instead of through the responder.

func (h *Handler) metadata(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, h.service.Metadata())
}

// protectedResource is the RFC 9728 metadata for /mcp: which authorization
// server issues its tokens.
func (h *Handler) protectedResource() http.Handler {
	settings := h.service.Settings()
	return auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               settings.Resource,
		AuthorizationServers:   []string{settings.Issuer},
		ScopesSupported:        []string{mcp.ScopeRead, mcp.ScopeWrite},
		BearerMethodsSupported: []string{"header"},
	})
}

func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	if !h.tokenRequest(w, r) {
		return
	}
	client, failure := h.authenticateClient(r)
	if failure != nil {
		tokenError(w, failure)
		return
	}
	resp, err := h.service.Token(r.Context(), TokenRequest{
		Client:       client,
		GrantType:    r.PostForm.Get("grant_type"),
		Code:         r.PostForm.Get("code"),
		RedirectURI:  r.PostForm.Get("redirect_uri"),
		CodeVerifier: r.PostForm.Get("code_verifier"),
		RefreshToken: r.PostForm.Get("refresh_token"),
		Resource:     r.PostForm.Get("resource"),
	})
	if err != nil {
		var oe *Error
		if errors.As(err, &oe) {
			tokenError(w, oe)
			return
		}
		tokenError(w, oauthErr("server_error", "The token could not be issued."))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

// revoke is RFC 7009; it answers 200 for unknown tokens too.
func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	if !h.tokenRequest(w, r) {
		return
	}
	client, failure := h.authenticateClient(r)
	if failure != nil {
		tokenError(w, failure)
		return
	}
	h.service.Revoke(r.Context(), client, r.PostForm.Get("token"))
	w.WriteHeader(http.StatusOK)
}

// tokenRequest opens CORS (browser clients such as the MCP Inspector call
// these endpoints; they carry no cookies, so any origin is safe), answers
// preflights, and parses the form. It reports whether to go on.
func (h *Handler) tokenRequest(w http.ResponseWriter, r *http.Request) bool {
	headers := w.Header()
	headers.Set("Access-Control-Allow-Origin", "*")
	headers.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	headers.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	headers.Set("Access-Control-Max-Age", "600")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTokenRequest)
	if err := r.ParseForm(); err != nil {
		tokenError(w, oauthErr("invalid_request", "The body must be application/x-www-form-urlencoded."))
		return false
	}
	return true
}

// authenticateClient reads the client's credentials: HTTP Basic, or
// client_id (and client_secret) in the body.
func (h *Handler) authenticateClient(r *http.Request) (*Client, *Error) {
	clientID, secret, basic := r.BasicAuth()
	if basic {
		clientID, _ = url.QueryUnescape(clientID)
		secret, _ = url.QueryUnescape(secret)
	} else {
		clientID = r.PostForm.Get("client_id")
		secret = r.PostForm.Get("client_secret")
	}
	return h.service.AuthenticateClient(r.Context(), clientID, secret)
}

func tokenError(w http.ResponseWriter, e *Error) {
	status := http.StatusBadRequest
	switch e.Code {
	case "invalid_client":
		status = http.StatusUnauthorized
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth"`)
	case "server_error":
		status = http.StatusInternalServerError
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, e)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
