package scaffold

import (
	"fmt"
	"regexp"
)

type templateData struct {
	ModulePath string
	Pkg        string
	Name       string // Product
	NameLower  string // product
	Plural     string // Products
	Table      string // products
	Fields     []fieldData
	Search     string // search column ("" when none)
	HasJSON    bool
	HasTime    bool
}

type fieldData struct {
	GoName      string
	Column      string
	Optional    bool
	SQLType     string
	DomainType  string // model / input / response type
	RequestType string // request DTO type
	ValidateTag string
	MapFromRow  string // dbgen row -> domain expression
	ToInput     string // request -> input expression
	FromInput   string // input -> dbgen param expression
}

// identifierRe accepts lower_snake_case identifiers (max 63 chars, the
// Postgres identifier limit).
var identifierRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func buildData(opts Options) (*templateData, error) {
	name := opts.Name
	if name == "" {
		return nil, fmt.Errorf("module name is required")
	}
	pkg := opts.Package
	if pkg == "" {
		pkg = name
	}
	table := opts.Table
	if table == "" {
		table = pluralize(name)
	}
	// Names land in file paths, Go identifiers and SQL: identifiers only (M55).
	for label, value := range map[string]string{"--name": name, "--package": pkg, "--table": table} {
		if !identifierRe.MatchString(value) {
			return nil, fmt.Errorf("%s %q must be lower_snake_case (a-z, 0-9, _; starting with a letter)", label, value)
		}
	}

	data := &templateData{
		ModulePath: opts.ModulePath,
		Pkg:        pkg,
		Name:       pascal(name),
		NameLower:  name,
		Plural:     pascal(table),
		Table:      table,
	}

	for _, f := range opts.Fields {
		fd, err := buildField(f)
		if err != nil {
			return nil, err
		}
		data.Fields = append(data.Fields, fd)
		if (fd.SQLType == "text") && data.Search == "" {
			data.Search = fd.Column
		}
		if fd.SQLType == "jsonb" {
			data.HasJSON = true
		}
		if fd.SQLType == "timestamptz" {
			data.HasTime = true
		}
	}
	return data, nil
}

func buildField(f Field) (fieldData, error) {
	pascalName := pascal(f.Name)
	fd := fieldData{GoName: pascalName, Column: f.Name, Optional: f.Optional}

	switch f.Type {
	case "string", "text":
		fd.SQLType = "text"
		fd.DomainType = stringType(f.Optional)
		fd.RequestType = stringType(f.Optional)
		if f.Optional {
			fd.ValidateTag = "omitempty,max=255"
		} else {
			fd.ValidateTag = "required,min=1,max=255"
		}
	case "int":
		fd.SQLType = "integer"
		fd.DomainType = int32Type(f.Optional)
		fd.RequestType = fd.DomainType
		fd.ValidateTag = requiredTag(f.Optional)
	case "bigint":
		fd.SQLType = "bigint"
		fd.DomainType = int64Type(f.Optional)
		fd.RequestType = fd.DomainType
		fd.ValidateTag = requiredTag(f.Optional)
	case "decimal":
		// Money/quantity in minor units.
		fd.SQLType = "bigint"
		fd.DomainType = int64Type(f.Optional)
		fd.RequestType = fd.DomainType
		fd.ValidateTag = requiredTag(f.Optional)
	case "bool":
		fd.SQLType = "boolean"
		fd.DomainType = boolType(f.Optional)
		fd.RequestType = fd.DomainType
		fd.ValidateTag = "omitempty"
	case "date", "datetime":
		fd.SQLType = "timestamptz"
		fd.DomainType = "*time.Time"
		fd.RequestType = "*time.Time"
		fd.Optional = true
		fd.ValidateTag = "omitempty"
		fd.MapFromRow = fmt.Sprintf("timePtr(row.%s)", pascalName)
		fd.ToInput = fmt.Sprintf("req.%s", pascalName)
		fd.FromInput = fmt.Sprintf("timestamptzPtr(input.%s)", pascalName)
		return fd, nil
	case "json":
		fd.SQLType = "jsonb"
		fd.DomainType = "[]byte"
		fd.RequestType = "json.RawMessage"
		fd.Optional = true
		fd.ValidateTag = "omitempty"
		fd.MapFromRow = fmt.Sprintf("row.%s", pascalName)
		fd.ToInput = fmt.Sprintf("[]byte(req.%s)", pascalName)
		fd.FromInput = fmt.Sprintf("input.%s", pascalName)
		return fd, nil
	default:
		return fd, fmt.Errorf("field %q has unsupported type %q", f.Name, f.Type)
	}

	fd.MapFromRow = fmt.Sprintf("row.%s", pascalName)
	fd.ToInput = fmt.Sprintf("req.%s", pascalName)
	fd.FromInput = fmt.Sprintf("input.%s", pascalName)
	return fd, nil
}

func stringType(optional bool) string {
	if optional {
		return "*string"
	}
	return "string"
}

func int32Type(optional bool) string {
	if optional {
		return "*int32"
	}
	return "int32"
}

func boolType(optional bool) string {
	if optional {
		return "*bool"
	}
	return "bool"
}

func int64Type(optional bool) string {
	if optional {
		return "*int64"
	}
	return "int64"
}

func requiredTag(optional bool) string {
	if optional {
		return "omitempty"
	}
	return "required"
}
