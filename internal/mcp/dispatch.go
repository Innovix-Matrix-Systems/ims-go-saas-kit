package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

// APIPrefix is where the router the dispatcher serves is mounted.
const APIPrefix = "/api/v1"

// Connection is who a tool call runs as: the identity of the MCP token and
// the tenant chosen on the consent page (platform accounts only).
type Connection struct {
	Identity *authn.Identity
	TenantID int64
	BranchID int64
	Client   string
	Locale   string
}

// Dispatcher serves a tool call as an in-process request to the /api/v1
// router. Global middleware (rate limit, access log, telemetry, timeout) ran
// once already, on the /mcp request.
type Dispatcher struct {
	// API is the router mounted at /api/v1, with every authentication,
	// tenancy and permission gate.
	API http.Handler
	// MaxResult caps a response body; bigger ones are refused.
	MaxResult int
	// Host is the public host (from APP_URL) sub-requests carry, so links in
	// paginated results point at the API.
	Host string
}

// Call runs tool with the JSON arguments and maps the response to a tool
// result. Errors the model can act on (validation, permissions, not found)
// come back as an isError result, never as a protocol error.
func (d *Dispatcher) Call(ctx context.Context, conn Connection, tool Tool, arguments json.RawMessage) *sdk.CallToolResult {
	if tool.Scope == ScopeWrite && !conn.Identity.HasScope(ScopeWrite) {
		return failure(readOnlyRefusal)
	}

	req, err := d.request(ctx, conn, tool, arguments)
	if err != nil {
		return failure("The arguments are not valid for " + tool.Name + ": " + err.Error())
	}

	rec := newRecorder(d.MaxResult)
	d.API.ServeHTTP(rec, req)
	return result(rec, tool)
}

const readOnlyRefusal = "This connection can read but not change anything. To allow changes, " +
	"remove the connector and connect it again, and tick \"Allow changes\" on the consent page."

func (d *Dispatcher) request(ctx context.Context, conn Connection, tool Tool, arguments json.RawMessage) (*http.Request, error) {
	args := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(arguments)) > 0 && string(bytes.TrimSpace(arguments)) != "null" {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return nil, fmt.Errorf("arguments must be a JSON object")
		}
	}
	known := map[string]bool{"body": tool.HasBody}
	for _, p := range tool.PathParams {
		known[p] = true
	}
	for _, p := range tool.QueryParams {
		known[p] = true
	}
	for name := range args {
		if !known[name] {
			return nil, fmt.Errorf("unknown argument %q", name)
		}
	}

	path := tool.Path
	for _, name := range tool.PathParams {
		raw, ok := args[name]
		if !ok {
			return nil, fmt.Errorf("%q is required", name)
		}
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(scalar(raw)))
	}
	query := url.Values{}
	for _, name := range tool.QueryParams {
		if raw, ok := args[name]; ok && string(raw) != "null" {
			query.Set(name, scalar(raw))
		}
	}

	var body []byte
	if tool.HasBody {
		body = args["body"]
		if len(body) == 0 {
			body = []byte("{}")
		}
	}

	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	// The sub-request carries the identity, not the token, and a fresh chi
	// routing context positioned inside /api/v1.
	routing := chi.NewRouteContext()
	routing.RoutePath = strings.TrimPrefix(path, APIPrefix)
	sub := authn.Dispatched(ctx, conn.Identity)
	sub = httpx.WithMCPCall(sub, httpx.MCPCall{Tool: tool.Name, Client: conn.Client})
	sub = context.WithValue(sub, chi.RouteCtxKey, routing)

	req, err := http.NewRequestWithContext(sub, tool.Method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Host = d.Host
	req.Header.Set("Accept", "application/json")
	if tool.HasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if conn.Locale != "" {
		req.Header.Set("Accept-Language", conn.Locale)
	}
	// A platform account works in the tenant picked on the consent page,
	// exactly as with the acting headers.
	if conn.Identity.TenantID == 0 && conn.TenantID != 0 {
		req.Header.Set(tenancy.ActingTenantHeader, strconv.FormatInt(conn.TenantID, 10))
		if conn.BranchID != 0 {
			req.Header.Set(tenancy.ActingBranchHeader, strconv.FormatInt(conn.BranchID, 10))
		}
	}
	return req, nil
}

// scalar is a path or query value from a JSON argument: strings unquoted,
// numbers and booleans as written.
func scalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func result(rec *recorder, tool Tool) *sdk.CallToolResult {
	if rec.overflow {
		return failure(fmt.Sprintf("The result of %s is larger than this server returns to agents. "+
			"Ask for a smaller page (per_page) or narrow the search.", tool.Name))
	}
	status := rec.status
	body := bytes.TrimSpace(rec.body.Bytes())

	switch {
	case status >= 200 && status < 300:
		if len(body) == 0 {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "Done."}}}
		}
		var structured any
		if json.Unmarshal(body, &structured) != nil {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(body)}}}
		}
		out := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(body)}}}
		if _, isObject := structured.(map[string]any); isObject {
			out.StructuredContent = json.RawMessage(body)
		}
		return out
	case status >= 500:
		return failure(serverError(body, status))
	default:
		return failure(clientError(body, status))
	}
}

// problem is the part of an RFC 9457 document a model needs.
type problem struct {
	Title     string         `json:"title"`
	Detail    string         `json:"detail"`
	Code      string         `json:"error_code"`
	Errors    map[string]any `json:"errors,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

func clientError(body []byte, status int) string {
	var p problem
	_ = json.Unmarshal(body, &p)
	lead := fmt.Sprintf("The API refused the call (%d", status)
	if p.Code != "" {
		lead += " " + p.Code
	}
	lead += ")"
	if msg := firstNonEmpty(p.Detail, p.Title); msg != "" {
		lead += ": " + msg
	}
	switch status {
	case http.StatusForbidden:
		lead += " The signed-in user is not allowed to do this; an admin can grant the permission."
	case http.StatusPaymentRequired:
		lead += " The tenant's plan does not allow more of this."
	case http.StatusUnprocessableEntity:
		lead += " Fix the fields listed in errors and call again."
	}
	if len(body) == 0 {
		return lead
	}
	return lead + "\n\n" + string(body)
}

func serverError(body []byte, status int) string {
	var p problem
	_ = json.Unmarshal(body, &p)
	safe, _ := json.Marshal(map[string]any{
		"status":     status,
		"title":      firstNonEmpty(p.Title, http.StatusText(status)),
		"error_code": p.Code,
		"request_id": p.RequestID,
	})
	return "The server failed to handle the call. Try again later; if it keeps failing, " +
		"report the request_id.\n\n" + string(safe)
}

func failure(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: true}
}

// recorder is a response writer that keeps at most limit bytes.
type recorder struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	limit    int
	overflow bool
}

func newRecorder(limit int) *recorder {
	return &recorder{header: http.Header{}, status: http.StatusOK, limit: limit}
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) { r.status = status }

func (r *recorder) Write(p []byte) (int, error) {
	if r.overflow {
		return len(p), nil
	}
	if r.limit > 0 && r.body.Len()+len(p) > r.limit {
		r.overflow = true
		r.body.Reset()
		return len(p), nil
	}
	return r.body.Write(p)
}
