package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// seen is what the fake /api/v1 router received.
type seen struct {
	method, path, query, body, host string
	headers                         http.Header
	identity                        *authn.Identity
	call                            httpx.MCPCall
}

func fakeAPI(status int, response string, got *seen) http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/*", func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		*got = seen{
			method: req.Method, path: req.URL.Path, query: req.URL.RawQuery, body: string(body),
			host: req.Host, headers: req.Header.Clone(),
		}
		// What authn.Authenticate would do with the dispatched identity.
		authn.Authenticate(nil, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got.identity, _ = authn.IdentityFromContext(r.Context())
		})).ServeHTTP(w, req)
		got.call, _ = httpx.MCPCallFrom(req.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	})
	return r
}

var branchTool = Tool{
	Name: "get_branch", Scope: ScopeRead, Method: http.MethodGet, Path: "/api/v1/branch/{id}",
	PathParams: []string{"id"}, QueryParams: []string{"include"},
}

var createTool = Tool{
	Name: "create_branch", Scope: ScopeWrite, Method: http.MethodPost, Path: "/api/v1/branch", HasBody: true,
}

func connection(tenantID int64, scopes ...string) Connection {
	return Connection{
		Identity: &authn.Identity{UserID: 7, IsActive: true, TenantID: tenantID, Scopes: scopes},
		TenantID: 3, BranchID: 9, Client: "Claude", Locale: "bn",
	}
}

func TestDispatchBuildsTheSubRequest(t *testing.T) {
	var got seen
	d := &Dispatcher{API: fakeAPI(200, `{"data":{"id":5}}`, &got), Host: "api.example.com"}

	res := d.Call(context.Background(), connection(0, ScopeRead), branchTool, json.RawMessage(`{"id":5,"include":"manager"}`))
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	if got.method != http.MethodGet || got.path != "/api/v1/branch/5" || got.query != "include=manager" {
		t.Errorf("request = %s %s?%s", got.method, got.path, got.query)
	}
	if got.host != "api.example.com" {
		t.Errorf("host = %q, want the public host (for pagination links)", got.host)
	}
	if got.headers.Get("Authorization") != "" {
		t.Error("the sub-request must not carry a token")
	}
	if got.identity == nil || got.identity.UserID != 7 {
		t.Errorf("dispatched identity = %+v", got.identity)
	}
	// A platform account (no tenant of its own) works in the connection's tenant.
	if got.headers.Get("X-Acting-Tenant") != "3" || got.headers.Get("X-Acting-Branch") != "9" {
		t.Errorf("acting headers = %q / %q", got.headers.Get("X-Acting-Tenant"), got.headers.Get("X-Acting-Branch"))
	}
	if got.headers.Get("Accept-Language") != "bn" {
		t.Errorf("Accept-Language = %q", got.headers.Get("Accept-Language"))
	}
	if got.call.Tool != "get_branch" || got.call.Client != "Claude" {
		t.Errorf("MCP call in context = %+v", got.call)
	}
	if string(res.StructuredContent.(json.RawMessage)) != `{"data":{"id":5}}` {
		t.Errorf("structured = %v", res.StructuredContent)
	}
}

func TestDispatchTenantUserSendsNoActingHeaders(t *testing.T) {
	var got seen
	d := &Dispatcher{API: fakeAPI(201, `{"data":{}}`, &got)}
	d.Call(context.Background(), connection(1, ScopeRead, ScopeWrite), createTool, json.RawMessage(`{"body":{"name":"X"}}`))
	if got.headers.Get("X-Acting-Tenant") != "" {
		t.Error("a tenant user's tenant comes from the account, not acting headers")
	}
	if got.body != `{"name":"X"}` || got.headers.Get("Content-Type") != "application/json" {
		t.Errorf("body = %q, content type %q", got.body, got.headers.Get("Content-Type"))
	}
}

func TestDispatchRefusesBadArguments(t *testing.T) {
	var got seen
	d := &Dispatcher{API: fakeAPI(200, `{}`, &got)}
	cases := map[string]struct {
		tool Tool
		args string
		want string
	}{
		"unknown argument":    {branchTool, `{"id":1,"drop":"table"}`, `unknown argument "drop"`},
		"missing path param":  {branchTool, `{}`, `"id" is required`},
		"not an object":       {branchTool, `[1,2]`, "must be a JSON object"},
		"write without scope": {createTool, `{"body":{}}`, "can read but not change"},
	}
	for name, tc := range cases {
		got = seen{}
		res := d.Call(context.Background(), connection(1, ScopeRead), tc.tool, json.RawMessage(tc.args))
		text := res.Content[0].(*sdk.TextContent).Text
		if !res.IsError || !strings.Contains(text, tc.want) {
			t.Errorf("%s: isError %v, text %q; want %q", name, res.IsError, text, tc.want)
		}
		if got.method != "" {
			t.Errorf("%s: the API must not be called", name)
		}
	}
}

func TestDispatchMapsErrors(t *testing.T) {
	var got seen
	problem := `{"title":"Unprocessable Entity","status":422,"error_code":"VALIDATION_FAILED","errors":{"name":["required"]}}`
	res := (&Dispatcher{API: fakeAPI(422, problem, &got)}).Call(context.Background(), connection(1, ScopeRead, ScopeWrite), createTool, json.RawMessage(`{"body":{}}`))
	raw, _ := json.Marshal(res.Content)
	if !res.IsError || !strings.Contains(string(raw), "VALIDATION_FAILED") || !strings.Contains(string(raw), "required") {
		t.Errorf("4xx: %s", raw)
	}

	internal := `{"title":"Internal Server Error","status":500,"error_code":"INTERNAL_SERVER_ERROR","detail":"pq: relation secret_table","request_id":"r1"}`
	res = (&Dispatcher{API: fakeAPI(500, internal, &got)}).Call(context.Background(), connection(1, ScopeRead), branchTool, json.RawMessage(`{"id":1}`))
	raw, _ = json.Marshal(res.Content)
	if !res.IsError || strings.Contains(string(raw), "secret_table") || !strings.Contains(string(raw), "r1") {
		t.Errorf("5xx must hide internals but keep the request id: %s", raw)
	}
}
