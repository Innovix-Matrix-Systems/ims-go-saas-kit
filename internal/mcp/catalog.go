// Package mcp is the API's Model Context Protocol server: the /mcp endpoint
// that AI agents (Claude, ChatGPT, Cursor, VS Code…) connect to as a
// connector. See docs/guides/mcp.md.
//
// Tools are not written in Go. An OpenAPI operation marked with `x-mcp`
// becomes a tool, and calling it serves that operation through the /api/v1
// router in-process, so permissions, tenancy, plan limits, validation, errors
// and the activity log apply exactly as for the REST call.
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Scopes an MCP access token can carry.
const (
	ScopeRead  = "mcp:read"
	ScopeWrite = "mcp:write"
)

// Extension is the OpenAPI vendor extension that turns an operation into a
// tool.
const Extension = "x-mcp"

// Tool is one tool built from an OpenAPI operation.
type Tool struct {
	Name        string
	Title       string
	Description string
	Scope       string
	Method      string
	Path        string // the OpenAPI path, e.g. /api/v1/branch/{id}
	ReadOnly    bool
	Destructive bool
	Idempotent  bool

	PathParams  []string
	QueryParams []string
	HasBody     bool

	InputSchema  map[string]any
	OutputSchema map[string]any
}

// extension is the `x-mcp` block as written in the document.
type extension struct {
	Tool        string `json:"tool"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	ReadOnly    *bool  `json:"readOnly"`
	Destructive *bool  `json:"destructive"`
	Idempotent  *bool  `json:"idempotent"`
}

var toolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Catalog builds the tool list from every operation marked `x-mcp`, sorted by
// name. It fails when a mark is malformed, names collide, an operation cannot
// be a tool (not JSON), or a DELETE is not marked destructive.
func Catalog(doc *openapi3.T) ([]Tool, error) {
	var tools []Tool
	seen := map[string]string{}
	for _, path := range doc.Paths.InMatchingOrder() {
		item := doc.Paths.Value(path)
		for method, op := range item.Operations() {
			raw, ok := op.Extensions[Extension]
			if !ok {
				continue
			}
			tool, err := buildTool(path, strings.ToUpper(method), item, op, raw)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", strings.ToUpper(method), path, err)
			}
			if other, dup := seen[tool.Name]; dup {
				return nil, fmt.Errorf("tool %q is declared twice (%s and %s %s)", tool.Name, other, tool.Method, path)
			}
			seen[tool.Name] = tool.Method + " " + path
			tools = append(tools, tool)
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func buildTool(path, method string, item *openapi3.PathItem, op *openapi3.Operation, raw any) (Tool, error) {
	var ext extension
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Tool{}, err
	}
	if err := json.Unmarshal(encoded, &ext); err != nil {
		return Tool{}, fmt.Errorf("x-mcp: %w", err)
	}

	tool := Tool{
		Name:        ext.Tool,
		Title:       firstNonEmpty(ext.Title, op.Summary),
		Description: firstNonEmpty(ext.Description, op.Description, op.Summary),
		Scope:       ext.Scope,
		Method:      method,
		Path:        path,
		ReadOnly:    method == http.MethodGet,
		Destructive: method == http.MethodDelete,
		Idempotent:  method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete,
	}
	if tool.Name == "" {
		tool.Name = snake(op.OperationID)
	}
	if !toolName.MatchString(tool.Name) {
		return Tool{}, fmt.Errorf("tool name %q must be snake_case (a-z, 0-9, _)", tool.Name)
	}
	if tool.Scope == "" {
		tool.Scope = ScopeWrite
		if tool.ReadOnly {
			tool.Scope = ScopeRead
		}
	}
	if tool.Scope != ScopeRead && tool.Scope != ScopeWrite {
		return Tool{}, fmt.Errorf("scope %q must be %s or %s", tool.Scope, ScopeRead, ScopeWrite)
	}
	if tool.Scope == ScopeRead && method != http.MethodGet {
		return Tool{}, fmt.Errorf("a %s operation changes data and needs scope %s", method, ScopeWrite)
	}
	if ext.ReadOnly != nil {
		tool.ReadOnly = *ext.ReadOnly
	}
	if ext.Destructive != nil {
		tool.Destructive = *ext.Destructive
	}
	if ext.Idempotent != nil {
		tool.Idempotent = *ext.Idempotent
	}
	if method == http.MethodDelete && !tool.Destructive {
		return Tool{}, fmt.Errorf("a DELETE tool must be marked destructive")
	}

	properties := map[string]any{}
	var required []string
	params := append(append(openapi3.Parameters{}, item.Parameters...), op.Parameters...)
	for _, ref := range params {
		if ref == nil || ref.Value == nil {
			continue
		}
		p := ref.Value
		switch p.In {
		case openapi3.ParameterInPath:
			tool.PathParams = append(tool.PathParams, p.Name)
		case openapi3.ParameterInQuery:
			tool.QueryParams = append(tool.QueryParams, p.Name)
		default:
			continue // headers (acting headers) come from the connection
		}
		schema := map[string]any{"type": "string"}
		if p.Schema != nil {
			schema = jsonSchema(p.Schema, 0)
		}
		if p.Description != "" {
			schema["description"] = p.Description
		}
		properties[p.Name] = schema
		if p.Required || p.In == openapi3.ParameterInPath {
			required = append(required, p.Name)
		}
	}

	if body := op.RequestBody; body != nil && body.Value != nil {
		media := body.Value.Content.Get("application/json")
		if media == nil {
			return Tool{}, fmt.Errorf("only JSON operations can be tools (no multipart or file uploads)")
		}
		tool.HasBody = true
		bodySchema := map[string]any{"type": "object"}
		if media.Schema != nil {
			bodySchema = jsonSchema(media.Schema, 0)
		}
		properties["body"] = bodySchema
		if body.Value.Required {
			required = append(required, "body")
		}
	}

	input := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		sort.Strings(required)
		input["required"] = required
	}
	tool.InputSchema = input

	for _, code := range []string{"200", "201"} {
		resp := op.Responses.Value(code)
		if resp == nil || resp.Value == nil {
			continue
		}
		if media := resp.Value.Content.Get("application/json"); media != nil && media.Schema != nil {
			if out := jsonSchema(media.Schema, 0); out["type"] == "object" {
				tool.OutputSchema = out
			}
		}
		break
	}
	return tool, nil
}

// maxDepth stops a self-referencing schema from recursing forever.
const maxDepth = 12

// jsonSchema turns an OpenAPI 3.0 schema into JSON Schema 2020-12, with every
// $ref resolved: `nullable: true` becomes a "null" type, examples and OpenAPI
// only keywords are dropped.
func jsonSchema(ref *openapi3.SchemaRef, depth int) map[string]any {
	out := map[string]any{}
	if ref == nil || ref.Value == nil || depth > maxDepth {
		return out
	}
	s := ref.Value

	if s.Type != nil && !s.Type.IsEmpty() {
		types := s.Type.Slice()
		if s.Nullable && !s.Type.IncludesNull() {
			types = append(types, "null")
		}
		if len(types) == 1 {
			out["type"] = types[0]
		} else {
			out["type"] = types
		}
	}
	if s.Title != "" {
		out["title"] = s.Title
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if len(s.Enum) > 0 {
		enum := append([]any{}, s.Enum...)
		if s.Nullable {
			enum = append(enum, nil)
		}
		out["enum"] = enum
	}
	if s.Default != nil {
		out["default"] = s.Default
	}
	if s.Min != nil {
		out["minimum"] = *s.Min
	}
	if s.Max != nil {
		out["maximum"] = *s.Max
	}
	if s.MinLength > 0 {
		out["minLength"] = s.MinLength
	}
	if s.MaxLength != nil {
		out["maxLength"] = *s.MaxLength
	}
	if s.Pattern != "" {
		out["pattern"] = s.Pattern
	}
	if s.MinItems > 0 {
		out["minItems"] = s.MinItems
	}
	if s.MaxItems != nil {
		out["maxItems"] = *s.MaxItems
	}
	if s.UniqueItems {
		out["uniqueItems"] = true
	}
	if s.Items != nil {
		out["items"] = jsonSchema(s.Items, depth+1)
	}
	if len(s.Properties) > 0 {
		props := map[string]any{}
		for name, prop := range s.Properties {
			props[name] = jsonSchema(prop, depth+1)
		}
		out["properties"] = props
	}
	if len(s.Required) > 0 {
		out["required"] = append([]string{}, s.Required...)
	}
	if s.AdditionalProperties.Has != nil {
		out["additionalProperties"] = *s.AdditionalProperties.Has
	} else if s.AdditionalProperties.Schema != nil {
		out["additionalProperties"] = jsonSchema(s.AdditionalProperties.Schema, depth+1)
	}
	for key, refs := range map[string]openapi3.SchemaRefs{"allOf": s.AllOf, "oneOf": s.OneOf, "anyOf": s.AnyOf} {
		if len(refs) == 0 {
			continue
		}
		list := make([]any, 0, len(refs))
		for _, r := range refs {
			list = append(list, jsonSchema(r, depth+1))
		}
		out[key] = list
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// snake turns an operationId (createBranch) into a tool name (create_branch).
func snake(id string) string {
	var b strings.Builder
	for i, r := range id {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		if r == '-' || r == ' ' {
			r = '_'
		}
		b.WriteRune(r)
	}
	return b.String()
}
