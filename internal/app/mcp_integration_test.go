package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/oauth"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

func TestMCPDisabledAnswers404(t *testing.T) {
	app := testkit.New(t)
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/oauth/authorize", "/.well-known/oauth-protected-resource/mcp"} {
		app.Guest().Get(path).Status(t, http.StatusNotFound)
	}
	app.Guest().Post("/mcp", map[string]any{}).Status(t, http.StatusNotFound)
}

func TestMCPConnectorFlow(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	admin := app.User(t, testkit.Role("Admin"))

	// No token: 401 pointing at the protected resource metadata.
	guest := (&testkit.Agent{}).WithApp(app)
	resp := guest.Send("tools/list", map[string]any{})
	if resp.Code != http.StatusUnauthorized || !strings.Contains(resp.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("no token: %d %q", resp.Code, resp.Header().Get("WWW-Authenticate"))
	}

	agent := app.Agent(t, admin, testkit.AllowChanges())
	if agent.Scope != "mcp:read mcp:write" {
		t.Fatalf("scope = %q", agent.Scope)
	}
	tools := agent.Tools(t)
	if !slices.Equal(tools, []string{"create_branch", "create_user", "list_branches", "list_users", "whoami"}) {
		t.Fatalf("tools = %v", tools)
	}

	var branch struct {
		ID       int64 `json:"id"`
		TenantID int64 `json:"tenant_id"`
	}
	agent.Call(t, "create_branch", map[string]any{"body": map[string]any{"name": "From MCP"}}).OK(t).Data(t, &branch)
	if branch.TenantID != admin.TenantID {
		t.Fatalf("branch in tenant %d, want %d", branch.TenantID, admin.TenantID)
	}

	// The activity entry says an agent did it.
	var via map[string]any
	if err := app.Pool.QueryRow(context.Background(),
		`SELECT changes->'_via' FROM activity_log WHERE action = 'branch.created' AND subject_id = $1`, branch.ID).Scan(&via); err != nil {
		t.Fatalf("activity: %v", err)
	}
	if via["source"] != "mcp" || via["tool"] != "create_branch" {
		t.Fatalf("activity _via = %v", via)
	}

	// Validation errors are tool errors carrying the problem.
	agent.Call(t, "create_branch", map[string]any{"body": map[string]any{}}).Refused(t, "VALIDATION_FAILED")
	// Unknown arguments never reach the API.
	if r := agent.Call(t, "list_users", map[string]any{"bogus": 1}); !r.IsError || !strings.Contains(r.Text, "unknown argument") {
		t.Fatalf("unknown argument: %+v", r)
	}

	// Token kinds stay apart.
	app.As(&testkit.User{Token: agent.AccessToken}).Get("/api/v1/user/profile").Status(t, http.StatusUnauthorized)
	session := (&testkit.Agent{AccessToken: admin.Token}).WithApp(app)
	if resp := session.Send("tools/list", map[string]any{}); resp.Code != http.StatusUnauthorized {
		t.Fatalf("session token on /mcp = %d", resp.Code)
	}
}

func TestMCPRefreshRotatesAndReuseRevokes(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	agent := app.Agent(t, app.User(t, testkit.Role("Admin")))
	clientID := app.ClientID(t)

	refresh := tokenForm(app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {agent.RefreshToken}, "client_id": {clientID}})
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh = %d %s", refresh.Code, refresh.Body.String())
	}
	var rotated struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(refresh.Body.Bytes(), &rotated)
	if rotated.RefreshToken == "" || rotated.RefreshToken == agent.RefreshToken {
		t.Fatal("refresh must rotate the refresh token")
	}

	reuse := tokenForm(app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {agent.RefreshToken}, "client_id": {clientID}})
	if reuse.Code != http.StatusBadRequest || !strings.Contains(reuse.Body.String(), "invalid_grant") {
		t.Fatalf("reuse = %d %s", reuse.Code, reuse.Body.String())
	}
	after := (&testkit.Agent{AccessToken: rotated.AccessToken}).WithApp(app)
	if resp := after.Send("tools/list", map[string]any{}); resp.Code != http.StatusUnauthorized {
		t.Fatalf("after reuse the connection must be revoked, got %d", resp.Code)
	}
}

func TestMCPReadOnlyAndPermissions(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	super := app.SuperAdmin(t)
	app.Role(t, "Nobody") // a role with no permissions
	viewer := app.User(t, testkit.Role("Nobody"))

	readOnly := app.Agent(t, viewer)
	if readOnly.Scope != "mcp:read" {
		t.Fatalf("scope = %q", readOnly.Scope)
	}
	resp := readOnly.Send("tools/call", map[string]any{"name": "create_branch", "arguments": map[string]any{"body": map[string]any{"name": "x"}}})
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`) {
		t.Fatalf("read-only write = %d %q", resp.Code, resp.Header().Get("WWW-Authenticate"))
	}
	readOnly.Call(t, "list_users", nil).Refused(t, "INSUFFICIENT_PERMISSIONS")

	// A platform account works in the tenant it picked; disconnecting stops it.
	platform := app.Agent(t, super, testkit.AllowChanges(), testkit.WorkIn(testkit.DefaultTenant))
	platform.Call(t, "create_branch", map[string]any{"body": map[string]any{"name": "Platform via MCP"}}).OK(t)

	var conns []struct {
		ID int64 `json:"id"`
	}
	app.As(super).Get("/api/v1/user/connections").Status(t, http.StatusOK).Data(t, &conns)
	if len(conns) != 1 {
		t.Fatalf("connections = %v", conns)
	}
	app.As(super).Delete("/api/v1/user/connections/"+strconv.FormatInt(conns[0].ID, 10)).Status(t, http.StatusNoContent)
	if resp := platform.Send("tools/list", map[string]any{}); resp.Code != http.StatusUnauthorized {
		t.Fatalf("after disconnect = %d", resp.Code)
	}
}

// TestMCPTenantIsolation is the isolation suite's MCP variant: an agent sees
// and changes only its own tenant, exactly like the REST calls it makes.
func TestMCPTenantIsolation(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	tenantB := app.Tenant(t, "Tenant B")
	adminA := app.User(t, testkit.Role("Admin"))
	adminB := app.User(t, testkit.Role("Admin"), testkit.InTenant(tenantB), testkit.Name("Bea in B"))

	var hidden struct {
		ID int64 `json:"id"`
	}
	app.As(adminB).Post("/api/v1/branch", map[string]any{"name": "Only in B"}).Status(t, http.StatusCreated).Data(t, &hidden)

	agentA := app.Agent(t, adminA, testkit.AllowChanges())
	var branches []struct {
		ID       int64 `json:"id"`
		TenantID int64 `json:"tenant_id"`
	}
	agentA.Call(t, "list_branches", map[string]any{"per_page": 100}).OK(t).Data(t, &branches)
	if len(branches) == 0 {
		t.Fatal("tenant A's agent should see tenant A's branches")
	}
	for _, b := range branches {
		if b.ID == hidden.ID || b.TenantID != adminA.TenantID {
			t.Fatalf("tenant A's agent sees branch %d of tenant %d", b.ID, b.TenantID)
		}
	}
	var users []struct {
		Email    string `json:"email"`
		TenantID int64  `json:"tenant_id"`
	}
	app.As(adminB).Get("/api/v1/user?search=Bea").Status(t, http.StatusOK).Data(t, &users)
	if len(users) == 0 {
		t.Fatal("tenant B's admin should find Bea (the search must match)")
	}
	agentA.Call(t, "list_users", map[string]any{"per_page": 100, "search": "Bea"}).OK(t).Data(t, &users)
	if len(users) != 0 {
		t.Fatalf("tenant A's agent found tenant B's users: %v", users)
	}

	// A platform account that picked tenant B sees B, not A.
	platform := app.Agent(t, app.SuperAdmin(t), testkit.WorkIn(tenantB))
	platform.Call(t, "list_branches", map[string]any{"per_page": 100}).OK(t).Data(t, &branches)
	if !slices.ContainsFunc(branches, func(b struct {
		ID       int64 `json:"id"`
		TenantID int64 `json:"tenant_id"`
	}) bool {
		return b.ID == hidden.ID
	}) {
		t.Fatal("an agent working in tenant B should see tenant B's branch")
	}
	for _, b := range branches {
		if b.TenantID != tenantB {
			t.Fatalf("an agent working in tenant B sees tenant %d", b.TenantID)
		}
	}
}

func TestMCPAuthorizeRefusals(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	clientID := app.ClientID(t)
	cases := map[string]url.Values{
		"unknown client":       {"client_id": {"ims_nope"}, "redirect_uri": {"http://localhost/callback"}},
		"foreign redirect":     {"client_id": {clientID}, "redirect_uri": {"https://evil.example/cb"}},
		"private CIMD address": {"client_id": {"https://127.0.0.1/client.json"}, "redirect_uri": {"http://localhost/callback"}},
	}
	for name, query := range cases {
		query.Set("response_type", "code")
		query.Set("code_challenge", strings.Repeat("c", 43))
		query.Set("code_challenge_method", "S256")
		rec := serve(app, httptest.NewRequest(http.MethodGet, oauth.AuthorizePath+"?"+query.Encode(), nil))
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
			t.Errorf("%s: %d, Location %q (must show an error, never redirect)", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	missingPKCE := url.Values{"client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:5000/callback"}, "response_type": {"code"}}
	rec := serve(app, httptest.NewRequest(http.MethodGet, oauth.AuthorizePath+"?"+missingPKCE.Encode(), nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=invalid_request") {
		t.Errorf("missing PKCE: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestMCPRegisteredConfidentialClient(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())
	super := app.SuperAdmin(t)
	var client struct {
		ID       int64  `json:"id"`
		ClientID string `json:"client_id"`
		Secret   string `json:"client_secret"`
	}
	app.As(super).Post("/api/v1/oauth-client", map[string]any{
		"name": "Ops", "type": "confidential", "redirect_uris": []string{"https://ops.example.com/cb"}, "scopes": []string{"mcp:read"},
	}).Status(t, http.StatusCreated).Data(t, &client)
	if client.Secret == "" {
		t.Fatal("a confidential client gets its secret on creation")
	}
	app.As(super).Get("/api/v1/oauth-client/"+strconv.FormatInt(client.ID, 10)).Status(t, http.StatusOK)

	wrong := tokenForm(app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "client_id": {client.ClientID}, "client_secret": {"wrong"}})
	if wrong.Code != http.StatusUnauthorized || !strings.Contains(wrong.Body.String(), "invalid_client") {
		t.Fatalf("wrong secret = %d %s", wrong.Code, wrong.Body.String())
	}

	// A tenant user without oauth_client.manage cannot register clients.
	admin := app.User(t, testkit.Role("Admin"))
	app.As(admin).Get("/api/v1/oauth-client").Status(t, http.StatusForbidden)

	app.As(super).Delete("/api/v1/oauth-client/"+strconv.FormatInt(client.ID, 10)).Status(t, http.StatusNoContent)
	app.As(super).Get("/api/v1/oauth-client/"+strconv.FormatInt(client.ID, 10)).Status(t, http.StatusNotFound)
}

func tokenForm(app *testkit.App, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, oauth.TokenPath, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serve(app, req)
}

func serve(app *testkit.App, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)
	return rec
}
