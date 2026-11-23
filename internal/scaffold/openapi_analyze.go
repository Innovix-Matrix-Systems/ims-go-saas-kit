package scaffold

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// AnalyzeModule derives a ResourceSpec from an existing CRUD module. It reads
// only the declarative files (`routes.go` for paths, `request.go` for the
// `Response` and `InsertUpdateRequest` structs) and never inspects handler
// bodies. Anything it cannot find is an error the caller may downgrade to a
// warning (skip the resource).
func AnalyzeModule(root, name string) (ResourceSpec, error) {
	dir, err := moduleDir(root, name)
	if err != nil {
		return ResourceSpec{}, err
	}
	fset := token.NewFileSet()

	reqFile, err := parser.ParseFile(fset, filepath.Join(dir, "request.go"), nil, 0)
	if err != nil {
		return ResourceSpec{}, fmt.Errorf("parse request.go: %w", err)
	}
	resource, ok := structFields(reqFile, "Response")
	if !ok {
		return ResourceSpec{}, fmt.Errorf("module %q has no `Response` struct", name)
	}
	request, ok := structFields(reqFile, "InsertUpdateRequest")
	if !ok {
		return ResourceSpec{}, fmt.Errorf("module %q has no `InsertUpdateRequest` struct", name)
	}

	routesFile, err := parser.ParseFile(fset, filepath.Join(dir, "routes.go"), nil, 0)
	if err != nil {
		return ResourceSpec{}, fmt.Errorf("parse routes.go: %w", err)
	}
	paths := routePaths(routesFile)
	if len(paths) == 0 {
		return ResourceSpec{}, fmt.Errorf("module %q declares no CRUD routes", name)
	}

	return ResourceSpec{
		Name:     name,
		Tag:      pascal(pluralize(name)),
		Schema:   pascal(name),
		Resource: resource,
		Request:  request,
		Paths:    paths,
	}, nil
}

// moduleDir locates an existing module package, tolerating the plural directory
// names used by hand-written modules (e.g. resource "branch" in "branches").
func moduleDir(root, name string) (string, error) {
	base := filepath.Join(root, "internal", "modules")
	for _, candidate := range []string{name, pluralize(name)} {
		dir := filepath.Join(base, candidate)
		if _, err := os.Stat(filepath.Join(dir, "request.go")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("module %q not found under internal/modules", name)
}

// structFields returns the fields of the named struct as SpecFields, or false
// when the struct is not declared in the file.
func structFields(file *ast.File, structName string) ([]SpecField, bool) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != structName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			return fieldsFromStruct(st), true
		}
	}
	return nil, false
}

func fieldsFromStruct(st *ast.StructType) []SpecField {
	var out []SpecField
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		tag := structTag(field)
		jsonName := strings.Split(tag.Get("json"), ",")[0]
		if jsonName == "" || jsonName == "-" {
			continue
		}
		out = append(out, specFieldFromGo(jsonName, exprString(field.Type), tag.Get("validate")))
	}
	return out
}

func structTag(field *ast.Field) reflect.StructTag {
	if field.Tag == nil {
		return ""
	}
	value, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(value)
}

// specFieldFromGo maps a Go field type + validate tag to an OpenAPI property.
func specFieldFromGo(name, goType, validate string) SpecField {
	pointer := strings.HasPrefix(goType, "*")
	// A validate tag is only present on request DTOs; response fields are
	// required unless they are pointers.
	optional := pointer || (validate != "" && !strings.Contains(validate, "required"))
	base := strings.TrimPrefix(goType, "*")

	sf := SpecField{Name: name, Optional: optional}
	switch base {
	case "string":
		sf.Type, sf.MaxLength = "string", maxLength(validate)
	case "int", "int32":
		sf.Type, sf.Format = "integer", "int32"
	case "int64":
		sf.Type, sf.Format = "integer", "int64"
	case "bool":
		sf.Type = "boolean"
	case "time.Time":
		sf.Type, sf.Format = "string", "date-time"
	case "json.RawMessage", "[]byte":
		sf.Type, sf.AdditionalProps, sf.Optional = "object", true, true
	default:
		sf.Type = "string"
		sf.MaxLength = 255
	}
	if sf.Optional {
		sf.Example = exampleFor(sf)
	}
	return sf
}

func maxLength(validate string) int {
	for _, part := range strings.Split(validate, ",") {
		if strings.HasPrefix(part, "max=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(part, "max=")); err == nil && n > 0 {
				return n
			}
		}
	}
	return 255
}

func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(e.Elt)
	default:
		return ""
	}
}

var routeMethods = map[string]bool{
	"Get": true, "Post": true, "Patch": true, "Put": true, "Delete": true,
}

// routePaths extracts the chi route registrations from routes.go, grouped by
// path suffix ("" for the collection, "/{id}" for the item).
func routePaths(file *ast.File) []PathSpec {
	collection := map[string]bool{}
	item := map[string]bool{}
	var collectionOrder, itemOrder []string

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !routeMethods[sel.Sel.Name] || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		method := strings.ToLower(sel.Sel.Name)
		segments := strings.Split(strings.Trim(path, "/"), "/")
		switch {
		case len(segments) == 1 && segments[0] != "" && !strings.HasPrefix(segments[0], "{"):
			if !collection[method] {
				collection[method] = true
				collectionOrder = append(collectionOrder, method)
			}
		case len(segments) == 2 && segments[0] != "" && strings.HasPrefix(segments[1], "{"):
			if !item[method] {
				item[method] = true
				itemOrder = append(itemOrder, method)
			}
		default:
			// Nested or non-item routes are not part of a CRUD resource.
		}
		return true
	})

	var paths []PathSpec
	if len(collectionOrder) > 0 {
		paths = append(paths, PathSpec{Suffix: "", Methods: collectionOrder})
	}
	if len(itemOrder) > 0 {
		paths = append(paths, PathSpec{Suffix: "/{id}", Methods: itemOrder})
	}
	return paths
}
