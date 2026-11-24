package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	api "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/api"
)

func load(t *testing.T, spec string) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData([]byte(spec))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return doc
}

func TestCatalogFromTheContract(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(api.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := Catalog(doc)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	want := map[string]string{
		"create_branch": ScopeWrite, "create_user": ScopeWrite,
		"list_branches": ScopeRead, "list_users": ScopeRead, "whoami": ScopeRead,
	}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(tools), len(want))
	}
	for i, tool := range tools {
		if i > 0 && tools[i-1].Name >= tool.Name {
			t.Errorf("tools not sorted: %s before %s", tools[i-1].Name, tool.Name)
		}
		if want[tool.Name] != tool.Scope {
			t.Errorf("%s scope = %s, want %s", tool.Name, tool.Scope, want[tool.Name])
		}
		if tool.InputSchema["type"] != "object" {
			t.Errorf("%s input schema is not an object", tool.Name)
		}
		props := tool.InputSchema["properties"].(map[string]any)
		for _, header := range []string{"X-Acting-Tenant", "X-Acting-Branch", "X-Acting-User"} {
			if _, ok := props[header]; ok {
				t.Errorf("%s exposes header %s as an argument", tool.Name, header)
			}
		}
	}
	create := tools[0]
	if !create.HasBody || create.ReadOnly || create.Method != http.MethodPost {
		t.Errorf("create_branch = %+v", create)
	}
}

const specHead = `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
`

func TestCatalogRejectsBadMarks(t *testing.T) {
	cases := map[string]string{
		"read scope on a write": `  /a:
    post:
      operationId: makeA
      x-mcp: {scope: "mcp:read"}
      responses: {"200": {description: ok}}
`,
		"delete not destructive": `  /a/{id}:
    delete:
      operationId: dropA
      parameters: [{name: id, in: path, required: true, schema: {type: integer}}]
      x-mcp: {destructive: false}
      responses: {"204": {description: ok}}
`,
		"multipart": `  /a:
    post:
      operationId: uploadA
      x-mcp: {}
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object}
      responses: {"200": {description: ok}}
`,
		"duplicate names": `  /a:
    get:
      operationId: listA
      x-mcp: {tool: same}
      responses: {"200": {description: ok}}
  /b:
    get:
      operationId: listB
      x-mcp: {tool: same}
      responses: {"200": {description: ok}}
`,
		"bad name": `  /a:
    get:
      operationId: listA
      x-mcp: {tool: "List-A"}
      responses: {"200": {description: ok}}
`,
	}
	for name, paths := range cases {
		if _, err := Catalog(load(t, specHead+paths)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestJSONSchemaConvertsNullable(t *testing.T) {
	doc := load(t, specHead+`  /a:
    get:
      operationId: getA
      x-mcp: {}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  name: {type: string, nullable: true, example: x}
                  kind: {type: string, enum: [a, b], nullable: true}
`)
	tools, err := Catalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	props := tools[0].OutputSchema["properties"].(map[string]any)
	name := props["name"].(map[string]any)
	if types, ok := name["type"].([]string); !ok || len(types) != 2 || types[1] != "null" {
		t.Errorf("nullable string = %v", name["type"])
	}
	if _, ok := name["example"]; ok {
		t.Error("example must be dropped")
	}
	if enum := props["kind"].(map[string]any)["enum"].([]any); enum[len(enum)-1] != nil {
		t.Errorf("nullable enum = %v", enum)
	}
	if tools[0].Name != "get_a" {
		t.Errorf("default name = %s", tools[0].Name)
	}
}

func TestCalledTools(t *testing.T) {
	one := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_branch"}}`
	batch := `[{"method":"tools/list"},{"method":"tools/call","params":{"name":"whoami"}}]`
	if got := calledTools([]byte(one)); len(got) != 1 || got[0] != "create_branch" {
		t.Errorf("one = %v", got)
	}
	if got := calledTools([]byte(batch)); len(got) != 1 || got[0] != "whoami" {
		t.Errorf("batch = %v", got)
	}
}

func TestOriginGate(t *testing.T) {
	h := originGate([]string{"https://app.example.com"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for origin, want := range map[string]int{"": 204, "https://app.example.com": 204, "https://evil.example": 403} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Origin %q = %d, want %d", origin, rec.Code, want)
		}
	}
}

func TestRecorderCapsTheResult(t *testing.T) {
	rec := newRecorder(10)
	_, _ = rec.Write([]byte("0123456789abc"))
	if !rec.overflow || rec.body.Len() != 0 {
		t.Fatalf("overflow = %v, kept %d bytes", rec.overflow, rec.body.Len())
	}
	if res := result(rec, Tool{Name: "list_users"}); !res.IsError {
		t.Fatal("an oversized result must be a tool error")
	}
}
