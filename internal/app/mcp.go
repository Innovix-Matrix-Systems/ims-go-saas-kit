package app

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	api "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/api"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/oauth"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/version"
)

// mcpCatalog builds the MCP tools from the operations marked x-mcp in the
// embedded OpenAPI document.
func mcpCatalog() ([]mcp.Tool, error) {
	doc, err := ValidateSpec(api.Spec)
	if err != nil {
		return nil, err
	}
	return mcp.Catalog(doc)
}

// mcpHandler is the /mcp endpoint, dispatching tool calls into apiRouter
// (the router mounted at /api/v1).
func (d *Dependencies) mcpHandler(apiRouter http.Handler) http.Handler {
	return mcp.Handler(mcp.Options{
		Name:           d.Config.App.Name,
		Version:        version.Version,
		Resource:       d.OAuthService.Settings().Resource,
		Tools:          d.MCPTools,
		Verifier:       d.OAuthService,
		Dispatch:       &mcp.Dispatcher{API: apiRouter, MaxResult: d.Config.MCP.MaxResultBytes, Host: appHost(d.Config.App.URL)},
		AllowedOrigins: d.Config.Ops.CORS.AllowedOrigins,
		MaxRequest:     d.Config.MCP.MaxRequestBytes,
		Limiter:        d.MCPLimiter,
		Logger:         d.Logger,
		OnCall: func(tool, outcome string, _ time.Duration) {
			if d.Metrics != nil {
				d.Metrics.IncCounter("ims_mcp_tool_calls_total", map[string]string{"tool": tool, "outcome": outcome})
			}
		},
	})
}

func appHost(appURL string) string {
	if u, err := url.Parse(appURL); err == nil {
		return u.Host
	}
	return ""
}

// OAuthService builds the MCP connector's authorization server. The HTTP
// server and `ims oauth:client` share it.
func (p *Platform) OAuthService() *oauth.Service {
	cfg := p.Config
	queries := p.Tx.Queries()
	issuer := strings.TrimRight(cfg.App.URL, "/")
	repo := oauth.NewRepository(queries)
	return oauth.NewService(repo,
		oauth.NewClients(repo, p.Cache, cfg.MCP.CIMDEnabled, cfg.MCP.AllowedClientHosts),
		p.Authenticator, p.IdentityRepo, oauth.Settings{
			Issuer:          issuer,
			Resource:        issuer + mcp.Path,
			AccessTokenTTL:  cfg.MCP.AccessTokenTTL,
			RefreshTokenTTL: cfg.MCP.RefreshTokenTTL,
			SecretGrace:     cfg.MCP.SecretGrace,
		}).
		WithTx(p.Tx).
		WithActivity(p.Activity)
}
