package testkit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/oauth"
)

// MCP. An Agent is an AI client connected to /mcp as a user,
// through the real OAuth flow: sign-in page, consent, code, token. A module
// that marks an operation x-mcp tests its tool next to its REST tests:
//
//	app := testkit.New(t, testkit.WithMCP())
//	admin := app.User(t, testkit.Role("Admin"))
//	agent := app.Agent(t, admin, testkit.AllowChanges())
//	var product struct{ Name string `json:"name"` }
//	agent.Call(t, "create_product", map[string]any{"body": map[string]any{"name": "Tea"}}).OK(t).Data(t, &product)
//	agent.Call(t, "create_product", map[string]any{"body": map[string]any{}}).Refused(t, "VALIDATION_FAILED")

// WithMCP turns the MCP server on (MCP_ENABLED=true) for this App.
func WithMCP() Option {
	return func(s *newSettings) { s.mcp = true }
}

// AgentOption customises App.Agent.
type AgentOption func(*agentSpec)

type agentSpec struct {
	write    bool
	tenantID int64
}

// AllowChanges ticks "Allow changes" on the consent page (mcp:write).
func AllowChanges() AgentOption { return func(s *agentSpec) { s.write = true } }

// WorkIn picks the tenant a platform account's agent works in. Without it a
// platform account connects with "no tenant: system work only".
func WorkIn(tenantID int64) AgentOption { return func(s *agentSpec) { s.tenantID = tenantID } }

// Agent is an MCP client signed in as one user.
type Agent struct {
	app          *App
	AccessToken  string
	RefreshToken string
	Scope        string
}

const agentRedirect = "http://localhost/callback"

// Agent connects an MCP client as user: it signs in on the authorization
// page, consents (read-only unless AllowChanges) and redeems the code. It
// fails the test at the first step that does not succeed.
func (a *App) Agent(t *testing.T, user *User, options ...AgentOption) *Agent {
	t.Helper()
	spec := agentSpec{}
	for _, option := range options {
		option(&spec)
	}
	clientID := a.agentClient(t)
	flow := &browser{app: a}

	verifier := strings.Repeat("testkit-verifier-", 4)
	sum := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id": {clientID}, "redirect_uri": {agentRedirect}, "response_type": {"code"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"state": {"testkit"}, "scope": {"mcp:read mcp:write"},
	}
	page := flow.do(httptest.NewRequest(http.MethodGet, oauth.AuthorizePath+"?"+query.Encode(), nil))
	if page.Code != http.StatusOK {
		t.Fatalf("testkit: authorize = %d: %.300s", page.Code, page.Body.String())
	}
	consent := flow.form(oauth.AuthorizePath+"/login", url.Values{
		"csrf": {csrfOf(t, page.Body.String())}, "email": {user.Email}, "password": {Password},
	})
	if consent.Code != http.StatusOK || !strings.Contains(consent.Body.String(), `name="decision"`) {
		t.Fatalf("testkit: sign-in = %d: %.300s", consent.Code, consent.Body.String())
	}
	values := url.Values{
		"csrf": {csrfOf(t, consent.Body.String())}, "decision": {"allow"},
		"tenant_id": {strconv.FormatInt(spec.tenantID, 10)},
	}
	if spec.write {
		values.Set("allow_write", "1")
	}
	back := flow.form(oauth.AuthorizePath+"/consent", values)
	location, err := url.Parse(back.Header().Get("Location"))
	if back.Code != http.StatusFound || err != nil || location.Query().Get("code") == "" {
		t.Fatalf("testkit: consent = %d, Location %q", back.Code, back.Header().Get("Location"))
	}

	token := flow.form(oauth.TokenPath, url.Values{
		"grant_type": {"authorization_code"}, "code": {location.Query().Get("code")},
		"redirect_uri": {agentRedirect}, "client_id": {clientID}, "code_verifier": {verifier},
	})
	if token.Code != http.StatusOK {
		t.Fatalf("testkit: token = %d: %s", token.Code, token.Body.String())
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(token.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("testkit: decode token response: %v", err)
	}
	return &Agent{app: a, AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, Scope: tokens.Scope}
}

// agentClient registers (once per App) the public client agents connect
// with.
func (a *App) agentClient(t *testing.T) string {
	t.Helper()
	if a.mcpClient != "" {
		return a.mcpClient
	}
	if !a.Config.MCP.Enabled {
		t.Fatal("testkit: App.Agent needs testkit.New(t, testkit.WithMCP())")
	}
	created, err := a.Deps.OAuthService.CreateClient(context.Background(), 0, oauth.ClientInput{
		Name: "Testkit agent", Type: oauth.TypePublic, RedirectURIs: []string{agentRedirect}, IsActive: true,
	})
	if err != nil {
		t.Fatalf("testkit: register MCP client: %v", err)
	}
	a.mcpClient = created.ClientID
	return a.mcpClient
}

// WithApp binds an Agent built by hand (an empty or foreign token) to app,
// for asserting what /mcp does with it.
func (g *Agent) WithApp(app *App) *Agent {
	g.app = app
	return g
}

// ClientID is the client_id App.Agent connects with.
func (a *App) ClientID(t *testing.T) string {
	t.Helper()
	return a.agentClient(t)
}

// Send posts one JSON-RPC message to /mcp and returns the HTTP response:
// for asserting transport behaviour (401, 403 insufficient_scope).
func (g *Agent) Send(method string, params any) *Response {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if g.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+g.AccessToken)
	}
	rec := httptest.NewRecorder()
	g.app.Router.ServeHTTP(rec, req)
	return &Response{ResponseRecorder: rec}
}

// Tools lists the tool names the server offers this agent.
func (g *Agent) Tools(t *testing.T) []string {
	t.Helper()
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	g.Send("tools/list", map[string]any{}).Status(t, http.StatusOK).JSON(t, &out)
	names := make([]string, 0, len(out.Result.Tools))
	for _, tool := range out.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// Call calls a tool with arguments and returns its result.
func (g *Agent) Call(t *testing.T, tool string, arguments any) *ToolResult {
	t.Helper()
	if arguments == nil {
		arguments = map[string]any{}
	}
	resp := g.Send("tools/call", map[string]any{"name": tool, "arguments": arguments})
	result := &ToolResult{Tool: tool, Status: resp.Code}
	if resp.Code != http.StatusOK {
		result.Text = resp.Body.String()
		return result
	}
	var out struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
			Content           []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("testkit: decode tool result: %v; body: %s", err, resp.Body.String())
	}
	if out.Error != nil {
		t.Fatalf("testkit: %s: protocol error: %s", tool, out.Error.Message)
	}
	result.IsError = out.Result.IsError
	result.Structured = out.Result.StructuredContent
	if len(out.Result.Content) > 0 {
		result.Text = out.Result.Content[0].Text
	}
	return result
}

// ToolResult is one tool call's outcome.
type ToolResult struct {
	Tool       string
	Status     int // HTTP status of the /mcp request (403 for a missing scope)
	IsError    bool
	Text       string
	Structured json.RawMessage
}

// OK fails the test unless the call succeeded.
func (r *ToolResult) OK(t *testing.T) *ToolResult {
	t.Helper()
	if r.Status != http.StatusOK || r.IsError {
		t.Fatalf("%s: want success, got status %d: %s", r.Tool, r.Status, r.Text)
	}
	return r
}

// Refused fails the test unless the API refused the call with code (an
// error_code such as VALIDATION_FAILED or INSUFFICIENT_PERMISSIONS).
func (r *ToolResult) Refused(t *testing.T, code string) *ToolResult {
	t.Helper()
	if r.Status != http.StatusOK || !r.IsError || !strings.Contains(r.Text, `"`+code+`"`) {
		t.Fatalf("%s: want a tool error with %s, got status %d, isError %v: %s", r.Tool, code, r.Status, r.IsError, r.Text)
	}
	return r
}

// Data decodes the "data" member of the structured result into dst.
func (r *ToolResult) Data(t *testing.T, dst any) *ToolResult {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.Structured, &envelope); err != nil || len(envelope.Data) == 0 {
		t.Fatalf("%s: no data in result: %v; %s", r.Tool, err, r.Text)
	}
	if err := json.Unmarshal(envelope.Data, dst); err != nil {
		t.Fatalf("%s: decode data: %v; %s", r.Tool, err, r.Text)
	}
	return r
}

// browser carries the authorization flow's cookie between requests.
type browser struct {
	app     *App
	cookies []*http.Cookie
}

func (b *browser) do(req *http.Request) *httptest.ResponseRecorder {
	for _, cookie := range b.cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	b.app.Router.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			b.cookies = []*http.Cookie{cookie}
		}
	}
	return rec
}

func (b *browser) form(path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return b.do(req)
}

var csrfField = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)

func csrfOf(t *testing.T, html string) string {
	t.Helper()
	m := csrfField.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("testkit: no csrf field in page: %.300s", html)
	}
	return m[1]
}
