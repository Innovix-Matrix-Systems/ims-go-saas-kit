package scaffold

import (
	"strings"
	"testing"
)

func TestParseFields(t *testing.T) {
	fields, err := ParseFields("name:string,notes:text?,amount:decimal,active:bool,due:date,meta:json")
	if err != nil {
		t.Fatalf("ParseFields() error = %v", err)
	}
	if len(fields) != 6 {
		t.Fatalf("len = %d, want 6", len(fields))
	}
	if fields[0].Name != "name" || fields[0].Type != "string" || fields[0].Optional {
		t.Errorf("field 0 = %+v", fields[0])
	}
	if !fields[1].Optional || fields[1].Type != "text" {
		t.Errorf("field 1 = %+v", fields[1])
	}
}

func TestParseFieldsErrors(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"reserved":  "id:string",
		"bad type":  "name:wizard",
		"no type":   "name",
		"duplicate": "name:string,name:string",
		"not snake": "Name:string",
	}
	for name, spec := range cases {
		if _, err := ParseFields(spec); err == nil {
			t.Errorf("%s: expected error for %q", name, spec)
		}
	}
}

func TestGenerateProducesExpectedFiles(t *testing.T) {
	fields, err := ParseFields("name:string,notes:text?,amount:decimal,active:bool,due:date,meta:json")
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(Options{
		ModulePath: "example.com/app",
		Name:       "product",
		NextNumber: 13,
		Fields:     fields,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
	}

	want := []string{
		"migrations/00013_create_products.sql",
		"sql/queries/products.sql",
		"internal/modules/product/model.go",
		"internal/modules/product/repository.go",
		"internal/modules/product/service.go",
		"internal/modules/product/request.go",
		"internal/modules/product/handler.go",
		"internal/modules/product/routes.go",
		"internal/modules/product/service_test.go",
		"internal/modules/product/locales.go",
		"internal/modules/product/locales/product/en.json",
	}
	for _, path := range want {
		if _, ok := byPath[path]; !ok {
			t.Errorf("missing generated file %s", path)
		}
	}

	checks := map[string]string{
		"migrations/00013_create_products.sql":             "CREATE TABLE products",
		"sql/queries/products.sql":                         "-- name: GetProduct :one",
		"internal/modules/product/model.go":                "type Product struct",
		"internal/modules/product/repository.go":           "func NewRepository(queries *dbgen.Queries) Repository",
		"internal/modules/product/service.go":              "func NewService(repo Repository) *Service",
		"internal/modules/product/handler.go":              "PermView",
		"internal/modules/product/routes.go":               "r.With(h.can(PermView)).Get(",
		"internal/modules/product/locales.go":              `i18n.Bundle{Owner: "product", Namespaces: []string{"product"}`,
		"internal/modules/product/locales/product/en.json": `"product.not_found": "Product with ID '{{.ID}}' was not found."`,
	}
	for path, needle := range checks {
		if !strings.Contains(byPath[path], needle) {
			t.Errorf("%s missing %q", path, needle)
		}
	}
	// Permission checks belong to the HTTP edge: the generated service is
	// callable from CLI/jobs and must not depend on authn.
	if strings.Contains(byPath["internal/modules/product/service.go"], "/internal/authn") {
		t.Error("generated service imports authn")
	}
}

func TestGenerateRejectsBadName(t *testing.T) {
	if _, err := Generate(Options{Name: "", Fields: []Field{{Name: "x", Type: "string"}}}); err == nil {
		t.Error("expected error for empty name")
	}
	for _, opts := range []Options{
		{Name: "../evil"},
		{Name: "product", Table: "products; DROP TABLE users"},
		{Name: "product", Package: "Product"},
	} {
		opts.Fields = []Field{{Name: "x", Type: "string"}}
		if _, err := Generate(opts); err == nil {
			t.Errorf("Generate(%+v) accepted an unsafe identifier", opts)
		}
	}
}

func TestGeneratedNotFoundIsTranslatable(t *testing.T) {
	files, err := Generate(Options{ModulePath: "example.com/x", Name: "product", Fields: []Field{{Name: "name", Type: "string"}}, NextNumber: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "internal/modules/product/repository.go" && !strings.Contains(string(f.Content), `WithMessageKey("product.not_found"`) {
			t.Error("the generated not-found error has no message key")
		}
	}
}
