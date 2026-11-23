package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalSpec = `openapi: 3.0.3
info: {title: test, version: "1"}
paths:
  /api/health:
    get:
      operationId: health
      responses:
        "200": {description: ok}
  # HANDWRITTEN PATH MARKER
components:
  parameters:
    PathID: {name: id, in: path, required: true, schema: {type: integer}}
    Page: {name: page, in: query, schema: {type: integer}}
    PerPage: {name: per_page, in: query, schema: {type: integer}}
    Search: {name: search, in: query, schema: {type: string}}
    ActingTenant: {name: X-Acting-Tenant, in: header, schema: {type: integer}}
    ActingBranch: {name: X-Acting-Branch, in: header, schema: {type: integer}}
    ActingUser: {name: X-Acting-User, in: header, schema: {type: integer}}
  responses:
    BadRequest: {description: bad}
    Unauthorized: {description: unauthorized}
    Forbidden: {description: forbidden}
    NotFound: {description: not found}
    ValidationFailed: {description: invalid}
    TooManyRequests: {description: slow down}
  schemas:
    Problem: {type: object}
    PaginationLinks: {type: object}
    PaginationMeta: {type: object}
`

func specWithRegions(t *testing.T) []byte {
	t.Helper()
	doc, err := InitRegions([]byte(minimalSpec))
	if err != nil {
		t.Fatalf("InitRegions() error = %v", err)
	}
	return doc
}

func mustFields(t *testing.T, spec string) []Field {
	t.Helper()
	fields, err := ParseFields(spec)
	if err != nil {
		t.Fatalf("ParseFields(%q) error = %v", spec, err)
	}
	return fields
}

func TestSpecFromFieldsMapping(t *testing.T) {
	spec := SpecFromFields("product", mustFields(t, "name:string,price:decimal,is_active:bool?,notes:text?"))

	got := map[string]string{}
	for _, f := range spec.Resource {
		got[f.Name] = fieldYAML(f)
	}
	want := map[string]string{
		"id":         "{type: integer, format: int64}",
		"tenant_id":  "{type: integer, format: int64}",
		"name":       "{type: string, maxLength: 255}",
		"price":      "{type: integer, format: int64}",
		"is_active":  "{type: boolean, nullable: true, example: false}",
		"notes":      "{type: string, maxLength: 255, nullable: true, example: \"sample\"}",
		"created_at": "{type: string, format: date-time}",
		"updated_at": "{type: string, format: date-time}",
	}
	for name, wantYAML := range want {
		if got[name] != wantYAML {
			t.Errorf("%s = %q, want %q", name, got[name], wantYAML)
		}
	}
	if spec.Schema != "Product" || spec.Tag != "Products" {
		t.Errorf("schema/tag = %q/%q", spec.Schema, spec.Tag)
	}
	if len(spec.Request) != 4 {
		t.Errorf("request fields = %d, want 4", len(spec.Request))
	}
}

func TestRenderRegionsDeterministic(t *testing.T) {
	specs := []ResourceSpec{
		SpecFromFields("widget", mustFields(t, "name:string")),
		SpecFromFields("product", mustFields(t, "name:string,price:decimal")),
	}
	first := RenderRegions(specs)
	second := RenderRegions([]ResourceSpec{specs[1], specs[0]})
	if first.Paths != second.Paths || first.Schemas != second.Schemas {
		t.Fatal("RenderRegions is not order-independent")
	}
	if strings.Index(first.Paths, "/api/v1/product") > strings.Index(first.Paths, "/api/v1/widget") {
		t.Error("resources are not sorted by name")
	}
}

func TestSpliceIsIdempotentAndPreservesHandWritten(t *testing.T) {
	data := specWithRegions(t)
	specs := []ResourceSpec{SpecFromFields("product", mustFields(t, "name:string,price:decimal"))}

	once, err := SpliceRegions(data, specs)
	if err != nil {
		t.Fatalf("SpliceRegions() error = %v", err)
	}
	twice, err := SpliceRegions(once, specs)
	if err != nil {
		t.Fatalf("second SpliceRegions() error = %v", err)
	}
	if string(once) != string(twice) {
		t.Error("splice is not idempotent")
	}

	strippedOriginal := stripRegions(string(data))
	strippedResult := stripRegions(string(once))
	if strippedOriginal != strippedResult {
		t.Errorf("hand-written content changed:\n--- before ---\n%s\n--- after ---\n%s", strippedOriginal, strippedResult)
	}
	if !strings.Contains(string(once), "/api/v1/product/{id}:") {
		t.Error("generated paths missing from spliced document")
	}
	if !strings.Contains(string(once), "ProductPageEnvelope:") {
		t.Error("generated schemas missing from spliced document")
	}
	if !strings.Contains(string(once), "resources: product") {
		t.Error("resources index was not updated")
	}
	if idx := ParseIndex(once); len(idx) != 1 || idx[0] != "product" {
		t.Errorf("ParseIndex = %v, want [product]", idx)
	}
}

func TestSpliceRejectsSchemaCollision(t *testing.T) {
	data := []byte(strings.Replace(minimalSpec,
		"    Problem: {type: object}",
		"    Problem: {type: object}\n    Product: {type: object}", 1))
	data, err := InitRegions(data)
	if err != nil {
		t.Fatalf("InitRegions() error = %v", err)
	}

	_, err = SpliceRegions(data, []ResourceSpec{SpecFromFields("product", mustFields(t, "name:string"))})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected a collision error, got %v", err)
	}
}

func TestInitRegionsInsertsAndIsIdempotent(t *testing.T) {
	init, err := InitRegions([]byte(minimalSpec))
	if err != nil {
		t.Fatalf("InitRegions() error = %v", err)
	}
	if !HasRegions(init) {
		t.Fatal("regions were not inserted")
	}
	again, err := InitRegions(init)
	if err != nil {
		t.Fatalf("second InitRegions() error = %v", err)
	}
	if string(init) != string(again) {
		t.Error("InitRegions is not idempotent")
	}
}

func TestAnalyzeModule(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "modules", "product")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "routes.go"), `package product

import "github.com/go-chi/chi/v5"

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/product", h.index)
	r.Get("/product/{id}", h.show)
	r.Post("/product", h.store)
	r.Patch("/product/{id}", h.update)
	r.Delete("/product/{id}", h.destroy)
}
`)
	writeFile(t, filepath.Join(dir, "request.go"), `package product

import "time"

type InsertUpdateRequest struct {
	Name  string `+"`"+`json:"name" validate:"required,min=1,max=255"`+"`"+`
	Notes *string `+"`"+`json:"notes" validate:"omitempty,max=1000"`+"`"+`
}

type Response struct {
	ID        int64     `+"`"+`json:"id"`+"`"+`
	TenantID  int64     `+"`"+`json:"tenant_id"`+"`"+`
	Name      string    `+"`"+`json:"name"`+"`"+`
	Notes     *string   `+"`"+`json:"notes"`+"`"+`
	CreatedAt time.Time `+"`"+`json:"created_at"`+"`"+`
	UpdatedAt time.Time `+"`"+`json:"updated_at"`+"`"+`
}
`)

	spec, err := AnalyzeModule(root, "product")
	if err != nil {
		t.Fatalf("AnalyzeModule() error = %v", err)
	}
	if spec.Schema != "Product" || len(spec.Paths) != 2 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if len(spec.Request) != 2 || !spec.Request[1].Optional {
		t.Errorf("request fields = %+v", spec.Request)
	}
	if spec.Request[0].MaxLength != 255 {
		t.Errorf("maxLength = %d, want 255", spec.Request[0].MaxLength)
	}
	if len(spec.Resource) != 6 {
		t.Errorf("resource fields = %d, want 6", len(spec.Resource))
	}
	// time.Time is not a pointer -> required, no example.
	for _, f := range spec.Resource {
		if f.Name == "created_at" && (f.Optional || f.Example != "") {
			t.Errorf("created_at = %+v, want required without example", f)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
