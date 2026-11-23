package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/ratelimit"
)

// Path is where the MCP endpoint is mounted.
const Path = "/mcp"

// ProtectedResourcePath is the RFC 9728 metadata document for /mcp.
const ProtectedResourcePath = "/.well-known/oauth-protected-resource"

// TokenVerifier checks an MCP access token and returns the connection it
// runs as, with the token's expiry. The OAuth module implements it.
type TokenVerifier interface {
	VerifyMCP(ctx context.Context, raw string) (*Connection, time.Time, error)
}

// ErrInvalidToken is what a TokenVerifier returns for a token /mcp refuses
// (unknown, expired, revoked, a session token, another audience).
var ErrInvalidToken = auth.ErrInvalidToken

// Options configures the MCP endpoint.
type Options struct {
	Name     string // the product's name, shown to agents
	Version  string
	Resource string // the canonical /mcp URL: APP_URL + /mcp
	Tools    []Tool
	Verifier TokenVerifier
	Dispatch *Dispatcher
	// AllowedOrigins are the browser origins allowed to call /mcp (CORS
	// origins). A request with any other Origin header gets 403.
	AllowedOrigins []string
	MaxRequest     int64
	Limiter        ratelimit.Limiter // per-token tool-call budget; nil = none
	Logger         *slog.Logger
	// OnCall is told about every tool call (metrics).
	OnCall func(tool, outcome string, took time.Duration)
}

// Handler is the /mcp endpoint: origin check, body limit, bearer token
// (answering 401 with the RFC 9728 pointer), scope check, per-token rate
// limit, then the SDK's stateless Streamable HTTP handler.
func Handler(opts Options) http.Handler {
	server := NewServer(opts)
	streamable := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		Logger:       opts.Logger,
		// Behind a tunnel or a reverse proxy on the same host, requests
		// arrive from 127.0.0.1 with the public Host; our own Origin check
		// below is the DNS-rebinding protection.
		DisableLocalhostProtection: true,
	})

	writes := map[string]bool{}
	for _, tool := range opts.Tools {
		if tool.Scope == ScopeWrite {
			writes[tool.Name] = true
		}
	}

	metadataURL := strings.TrimSuffix(opts.Resource, Path) + ProtectedResourcePath + Path
	bearer := auth.RequireBearerToken(verifier(opts.Verifier), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: metadataURL,
	})

	var h http.Handler = streamable
	h = rateLimit(opts.Limiter, h)
	h = scopeGate(writes, metadataURL, opts.MaxRequest, h)
	h = bearer(h)
	h = originGate(opts.AllowedOrigins, h)
	return h
}

// NewServer builds the MCP server with one tool per catalog entry.
func NewServer(opts Options) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{
		Name:    "ims-api",
		Title:   opts.Name,
		Version: opts.Version,
	}, &sdk.ServerOptions{
		Instructions: fmt.Sprintf("%s's API. These tools act as the signed-in user, with their "+
			"permissions and tenant. Call whoami first to learn who that is.", opts.Name),
		HasTools: true,
	})

	for _, tool := range opts.Tools {
		destructive := tool.Destructive
		server.AddTool(&sdk.Tool{
			Name:         tool.Name,
			Title:        tool.Title,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: outputSchema(tool),
			Annotations: &sdk.ToolAnnotations{
				Title:           tool.Title,
				ReadOnlyHint:    tool.ReadOnly,
				DestructiveHint: &destructive,
				IdempotentHint:  tool.Idempotent,
			},
		}, handler(opts, tool))
	}
	return server
}

func outputSchema(tool Tool) any {
	if tool.OutputSchema == nil {
		return nil
	}
	return tool.OutputSchema
}

func handler(opts Options, tool Tool) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		start := time.Now()
		conn, ok := connectionFrom(req)
		if !ok {
			return nil, errors.New("no connection for this call")
		}
		call := *conn
		if info := req.ClientInfo(); info != nil {
			if name := firstNonEmpty(info.Title, info.Name); name != "" {
				call.Client = name
			}
		}
		if req.Extra != nil && req.Extra.Header != nil {
			call.Locale = req.Extra.Header.Get("Accept-Language")
		}
		var arguments json.RawMessage
		if req.Params != nil {
			arguments = req.Params.Arguments
		}

		result := opts.Dispatch.Call(ctx, call, tool, arguments)
		if opts.OnCall != nil {
			outcome := "ok"
			if result.IsError {
				outcome = "error"
			}
			opts.OnCall(tool.Name, outcome, time.Since(start))
		}
		return result, nil
	}
}

const connectionKey = "connection"

func verifier(v TokenVerifier) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		conn, expires, err := v.VerifyMCP(ctx, token)
		if err != nil {
			return nil, err
		}
		return &auth.TokenInfo{
			Scopes:     conn.Identity.Scopes,
			Expiration: expires,
			UserID:     strconv.FormatInt(conn.Identity.UserID, 10),
			Extra:      map[string]any{connectionKey: conn},
		}, nil
	}
}

func connectionFrom(req *sdk.CallToolRequest) (*Connection, bool) {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return nil, false
	}
	conn, ok := req.Extra.TokenInfo.Extra[connectionKey].(*Connection)
	return conn, ok && conn != nil && conn.Identity != nil
}

// scopeGate answers a write tool called with a read-only token with 403
// insufficient_scope, so the client can ask the user for more (MCP
// authorization: scope challenge). It reads the JSON-RPC body and puts it
// back for the SDK.
func scopeGate(writes map[string]bool, metadataURL string, maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		info := auth.TokenInfoFromContext(r.Context())
		if info != nil && !hasScope(info.Scopes, ScopeWrite) {
			for _, name := range calledTools(body) {
				if writes[name] {
					w.Header().Set("WWW-Authenticate", fmt.Sprintf(
						`Bearer error="insufficient_scope", scope="%s %s", resource_metadata="%s", error_description="%s needs %s"`,
						ScopeRead, ScopeWrite, metadataURL, name, ScopeWrite))
					http.Error(w, "insufficient_scope", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// calledTools returns the tool names of tools/call messages in a JSON-RPC
// body (a single message or a batch).
func calledTools(body []byte) []string {
	type message struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	var batch []message
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		_ = json.Unmarshal(trimmed, &batch)
	} else {
		var one message
		if json.Unmarshal(trimmed, &one) == nil {
			batch = []message{one}
		}
	}
	var names []string
	for _, m := range batch {
		if m.Method == "tools/call" && m.Params.Name != "" {
			names = append(names, m.Params.Name)
		}
	}
	return names
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// originGate refuses browser requests from origins that are not allowed:
// the Streamable HTTP transport's DNS-rebinding protection. Native clients
// and Claude's servers send no Origin.
func originGate(allowed []string, next http.Handler) http.Handler {
	set := map[string]bool{}
	for _, o := range allowed {
		set[strings.TrimRight(strings.ToLower(strings.TrimSpace(o)), "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !set["*"] &&
			!set[strings.TrimRight(strings.ToLower(origin), "/")] {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit spends one unit of the token's budget per request.
func rateLimit(limiter ratelimit.Limiter, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if info := auth.TokenInfoFromContext(r.Context()); info != nil {
			if conn, ok := info.Extra[connectionKey].(*Connection); ok {
				allowed, retry := limiter.Allow(r.Context(), "mcp:"+strconv.FormatInt(conn.Identity.TokenID, 10))
				if !allowed {
					w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
					http.Error(w, "too many tool calls", http.StatusTooManyRequests)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
