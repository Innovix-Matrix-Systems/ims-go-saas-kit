// Package scaffold generates CRUD module source (migration, sqlc queries and a
// feature-first module package) from a declarative field spec.
package scaffold

import (
	"bytes"
	"fmt"
	"go/format"
	"strconv"
	"strings"
	"text/template"
)

// Supported field types.
var supportedTypes = map[string]struct{}{
	"string": {}, "text": {}, "int": {}, "bigint": {}, "bool": {},
	"decimal": {}, "date": {}, "datetime": {}, "json": {},
}

// reserved column names that the generator manages itself.
var reservedColumns = map[string]struct{}{
	"id": {}, "tenant_id": {}, "created_by": {}, "updated_by": {},
	"created_at": {}, "updated_at": {}, "deleted_at": {},
}

// Field is one user-declared column.
type Field struct {
	Name     string // snake_case column
	Type     string // see supportedTypes
	Optional bool
}

// Options configures a generation run.
type Options struct {
	ModulePath string // e.g. github.com/you/ims-go-admin-starter
	Package    string // go package name (defaults to Name)
	Name       string // singular, lower_snake (e.g. product)
	Table      string // table name (defaults to a naive plural of Name)
	NextNumber int    // next migration sequence number
	Fields     []Field
}

// File is one generated file.
type File struct {
	Path    string // relative to the repo root, forward slashes
	Content []byte
}

// ParseFields parses a "name:type[,name:type?]" spec.
func ParseFields(spec string) ([]Field, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("fields spec is empty")
	}
	var fields []Field
	seen := map[string]struct{}{}
	for _, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		optional := strings.HasSuffix(raw, "?")
		raw = strings.TrimSuffix(raw, "?")
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid field %q (want name:type)", raw)
		}
		name := strings.TrimSpace(parts[0])
		typ := strings.ToLower(strings.TrimSpace(parts[1]))
		if name == "" {
			return nil, fmt.Errorf("field %q has an empty name", raw)
		}
		if !isSnake(name) {
			return nil, fmt.Errorf("field %q must be lower_snake_case", name)
		}
		if _, ok := reservedColumns[name]; ok {
			return nil, fmt.Errorf("field %q is reserved by the generator", name)
		}
		if _, ok := supportedTypes[typ]; ok == false {
			return nil, fmt.Errorf("field %q has unsupported type %q", name, typ)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("duplicate field %q", name)
		}
		seen[name] = struct{}{}
		fields = append(fields, Field{Name: name, Type: typ, Optional: optional})
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no fields parsed from spec")
	}
	return fields, nil
}

// Generate returns the files for a module.
func Generate(opts Options) ([]File, error) {
	data, err := buildData(opts)
	if err != nil {
		return nil, err
	}

	specs := []struct {
		path     string
		tmpl     string
		format   bool
		override string // when set, use verbatim (non-templated)
	}{
		{path: fmt.Sprintf("migrations/%05d_create_%s.sql", opts.NextNumber, data.Table), tmpl: migrationTmpl},
		{path: "sql/queries/" + data.Table + ".sql", tmpl: queriesTmpl},
		{path: "internal/modules/" + data.NameLower + "/model.go", tmpl: modelTmpl, format: true},
		{path: "internal/modules/" + data.NameLower + "/repository.go", tmpl: repositoryTmpl, format: true},
		{path: "internal/modules/" + data.NameLower + "/service.go", tmpl: serviceTmpl, format: true},
		{path: "internal/modules/" + data.NameLower + "/request.go", tmpl: requestTmpl, format: true},
		{path: "internal/modules/" + data.NameLower + "/handler.go", tmpl: handlerTmpl, format: true},
		{path: "internal/modules/" + data.NameLower + "/routes.go", tmpl: routesTmpl, format: true},
		{path: "internal/modules/" + data.NameLower + "/service_test.go", tmpl: serviceTestTmpl, format: true},
	}

	files := make([]File, 0, len(specs))
	for _, spec := range specs {
		content, err := render(spec.tmpl, data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", spec.path, err)
		}
		if spec.format {
			formatted, err := format.Source(content)
			if err != nil {
				return nil, fmt.Errorf("%s: generated Go does not compile: %w\n%s", spec.path, err, content)
			}
			content = formatted
		}
		files = append(files, File{Path: spec.path, Content: content})
	}
	return files, nil
}

func render(tmpl string, data *templateData) ([]byte, error) {
	t, err := template.New("t").Funcs(template.FuncMap{}).Parse(tmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isSnake(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return s != ""
}

func pluralize(name string) string {
	switch {
	case strings.HasSuffix(name, "s"), strings.HasSuffix(name, "x"), strings.HasSuffix(name, "ch"), strings.HasSuffix(name, "sh"):
		return name + "es"
	case strings.HasSuffix(name, "y") && len(name) > 1:
		return name[:len(name)-1] + "ies"
	default:
		return name + "s"
	}
}

func pascal(snake string) string {
	var b strings.Builder
	for _, part := range strings.Split(snake, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

var _ = strconv.Itoa
