package oauth

import (
	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authz"
)

// RegisterEndpoints mounts the authorization server at the root:
// /.well-known/oauth-*, /oauth/*. With enabled false (MCP_ENABLED off) they
// answer 404, and the route table stays the same either way. They are
// public: the token endpoints authenticate the client themselves.
func (h *Handler) RegisterEndpoints(r chi.Router, enabled bool) {
	r.Group(func(r chi.Router) {
		r.Use(Gate(enabled, h.Responder))
		r.Get(ASMetadataPath, h.metadata)
		r.Get("/.well-known/oauth-protected-resource", h.protectedResource().ServeHTTP)
		r.Get("/.well-known/oauth-protected-resource/mcp", h.protectedResource().ServeHTTP)
		r.Get(AuthorizePath, h.pages.Authorize)
		r.Post(AuthorizePath+"/login", h.pages.Login)
		r.Post(AuthorizePath+"/consent", h.pages.Consent)
		r.Post(TokenPath, h.token)
		r.Options(TokenPath, h.token)
		r.Post(RevokePath, h.revoke)
		r.Options(RevokePath, h.revoke)
	})
}

// RegisterRoutes mounts the caller's own MCP connections (tenant-exempt;
// self-service, so no catalog permission).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/user/connections", h.connections)
	r.Delete("/user/connections/{id}", h.disconnect)
}

// RegisterInternalRoutes mounts registered-client management (internal
// accounts only, behind tenancy.InternalOnly).
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.With(h.can(authz.PermOAuthClientManage)).Get("/oauth-client", h.index)
	r.With(h.can(authz.PermOAuthClientManage)).Post("/oauth-client", h.store)
	r.With(h.can(authz.PermOAuthClientManage)).Get("/oauth-client/{id}", h.show)
	r.With(h.can(authz.PermOAuthClientManage)).Put("/oauth-client/{id}", h.update)
	r.With(h.can(authz.PermOAuthClientManage)).Delete("/oauth-client/{id}", h.destroy)
	r.With(h.can(authz.PermOAuthClientManage)).Post("/oauth-client/{id}/rotate-secret", h.rotate)
}
