package scaffold

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Markers delimiting the two managed regions of api/openapi.yaml. Everything
// between a BEGIN and its matching END is owned by the generator and is
// overwritten on every run; everything outside is hand-written and never
// touched. Detection is substring-based so the exact decoration is cosmetic.
const (
	markerPathsBegin   = "BEGIN GENERATED PATHS"
	markerPathsEnd     = "END GENERATED PATHS"
	markerSchemasBegin = "BEGIN GENERATED SCHEMAS"
	markerSchemasEnd   = "END GENERATED SCHEMAS"
)

// APIBase is the versioned prefix every generated path is mounted under.
const APIBase = "/api/v1"

// SpecField is one property in a generated schema.
type SpecField struct {
	Name            string // json/column name
	Type            string // string | integer | boolean | object
	Format          string // int32 | int64 | date-time
	MaxLength       int
	AdditionalProps bool
	Optional        bool
	Example         string // pre-rendered YAML scalar (e.g. `"sample"`), or ""
}

// PathSpec is one generated path item, relative to APIBase.
type PathSpec struct {
	Suffix  string   // "" for the collection, "/{id}" for the item
	Methods []string // lower-case HTTP methods
}

// ResourceSpec is everything the renderer needs to document one CRUD resource.
// It is produced either from a declarative field spec (`ims gen module`) or by
// analyzing an existing module (`ims gen openapi`).
type ResourceSpec struct {
	Name     string // singular, lower_snake (e.g. product)
	Tag      string // OpenAPI tag (e.g. Products)
	Schema   string // resource schema name (e.g. Product)
	Resource []SpecField
	Request  []SpecField
	Paths    []PathSpec
}

// Regions holds the rendered bodies for the two managed regions.
type Regions struct {
	Paths   string
	Schemas string
}

// SpecFromFields builds a ResourceSpec for a freshly scaffolded module. The
// resource schema is `id, tenant_id, <fields>, created_at, updated_at`; the
// request schema is the declared fields only.
func SpecFromFields(name string, fields []Field) ResourceSpec {
	rs := ResourceSpec{
		Name:   name,
		Tag:    pascal(pluralize(name)),
		Schema: pascal(name),
	}
	for _, f := range fields {
		rs.Request = append(rs.Request, specFieldFromType(f))
	}
	rs.Resource = append(rs.Resource,
		SpecField{Name: "id", Type: "integer", Format: "int64"},
		SpecField{Name: "tenant_id", Type: "integer", Format: "int64"},
	)
	rs.Resource = append(rs.Resource, rs.Request...)
	rs.Resource = append(rs.Resource,
		SpecField{Name: "created_at", Type: "string", Format: "date-time"},
		SpecField{Name: "updated_at", Type: "string", Format: "date-time"},
	)
	rs.Paths = []PathSpec{
		{Suffix: "", Methods: []string{"get", "post"}},
		{Suffix: "/{id}", Methods: []string{"get", "patch", "delete"}},
	}
	return rs
}

func specFieldFromType(f Field) SpecField {
	sf := SpecField{Name: f.Name, Optional: f.Optional}
	switch f.Type {
	case "string", "text":
		sf.Type, sf.MaxLength = "string", 255
	case "int":
		sf.Type, sf.Format = "integer", "int32"
	case "bigint", "decimal":
		sf.Type, sf.Format = "integer", "int64"
	case "bool":
		sf.Type = "boolean"
	case "date", "datetime":
		sf.Type, sf.Format, sf.Optional = "string", "date-time", true
	case "json":
		sf.Type, sf.AdditionalProps, sf.Optional = "object", true, true
	}
	if sf.Optional {
		sf.Example = exampleFor(sf)
	}
	return sf
}

func exampleFor(f SpecField) string {
	if f.Format == "date-time" {
		return `"2026-01-01T00:00:00Z"`
	}
	switch f.Type {
	case "integer":
		return "0"
	case "boolean":
		return "false"
	case "object":
		return "{}"
	default:
		return `"sample"`
	}
}

// SortSpecs returns the specs ordered by resource name (deterministic output).
func SortSpecs(specs []ResourceSpec) []ResourceSpec {
	out := make([]ResourceSpec, len(specs))
	copy(out, specs)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RenderRegions renders both region bodies for the given specs.
func RenderRegions(specs []ResourceSpec) Regions {
	sorted := SortSpecs(specs)
	return Regions{Paths: renderPaths(sorted), Schemas: renderSchemas(sorted)}
}

func renderSchemas(specs []ResourceSpec) string {
	var b strings.Builder
	for _, rs := range specs {
		b.WriteString("    " + rs.Schema + ":\n")
		b.WriteString("      type: object\n")
		writeRequired(&b, "      ", rs.Resource)
		b.WriteString("      properties:\n")
		for _, f := range rs.Resource {
			b.WriteString("        " + f.Name + ": " + fieldYAML(f) + "\n")
		}

		b.WriteString("    " + rs.Schema + "UpsertRequest:\n")
		b.WriteString("      type: object\n")
		writeRequired(&b, "      ", rs.Request)
		b.WriteString("      properties:\n")
		for _, f := range rs.Request {
			b.WriteString("        " + f.Name + ": " + fieldYAML(f) + "\n")
		}

		b.WriteString("    " + rs.Schema + "Envelope:\n")
		b.WriteString("      type: object\n")
		b.WriteString("      required: [data]\n")
		b.WriteString("      properties:\n")
		b.WriteString("        data: {$ref: \"#/components/schemas/" + rs.Schema + "\"}\n")

		b.WriteString("    " + rs.Schema + "PageEnvelope:\n")
		b.WriteString("      type: object\n")
		b.WriteString("      required: [data, links, meta]\n")
		b.WriteString("      properties:\n")
		b.WriteString("        data:\n")
		b.WriteString("          type: array\n")
		b.WriteString("          items: {$ref: \"#/components/schemas/" + rs.Schema + "\"}\n")
		b.WriteString("        links: {$ref: \"#/components/schemas/PaginationLinks\"}\n")
		b.WriteString("        meta: {$ref: \"#/components/schemas/PaginationMeta\"}\n")
	}
	return b.String()
}

func writeRequired(b *strings.Builder, indent string, fields []SpecField) {
	var required []string
	for _, f := range fields {
		if !f.Optional {
			required = append(required, f.Name)
		}
	}
	if len(required) == 0 {
		return
	}
	b.WriteString(indent + "required: [" + strings.Join(required, ", ") + "]\n")
}

func fieldYAML(f SpecField) string {
	parts := []string{"type: " + f.Type}
	if f.Format != "" {
		parts = append(parts, "format: "+f.Format)
	}
	if f.MaxLength > 0 {
		parts = append(parts, "maxLength: "+strconv.Itoa(f.MaxLength))
	}
	if f.AdditionalProps {
		parts = append(parts, "additionalProperties: true")
	}
	if f.Optional {
		parts = append(parts, "nullable: true")
	}
	if f.Example != "" {
		parts = append(parts, "example: "+f.Example)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func renderPaths(specs []ResourceSpec) string {
	var b strings.Builder
	for _, rs := range specs {
		for _, p := range rs.Paths {
			path := APIBase + "/" + rs.Name + p.Suffix
			b.WriteString("  " + path + ":\n")
			b.WriteString("    parameters:\n")
			if p.Suffix != "" {
				b.WriteString("      - {$ref: \"#/components/parameters/PathID\"}\n")
			}
			b.WriteString("      - {$ref: \"#/components/parameters/ActingTenant\"}\n")
			b.WriteString("      - {$ref: \"#/components/parameters/ActingBranch\"}\n")
			b.WriteString("      - {$ref: \"#/components/parameters/ActingUser\"}\n")
			for _, m := range orderMethods(p.Methods) {
				renderOperation(&b, rs, p.Suffix, m)
			}
		}
	}
	return b.String()
}

func orderMethods(methods []string) []string {
	rank := map[string]int{"get": 0, "post": 1, "put": 2, "patch": 3, "delete": 4}
	out := make([]string, len(methods))
	copy(out, methods)
	sort.SliceStable(out, func(i, j int) bool {
		ri, oki := rank[out[i]]
		rj, okj := rank[out[j]]
		if !oki {
			ri = 99
		}
		if !okj {
			rj = 99
		}
		return ri < rj
	})
	return out
}

func renderOperation(b *strings.Builder, rs ResourceSpec, suffix, method string) {
	item := suffix != ""
	switch method {
	case "get":
		if item {
			b.WriteString("    get:\n")
			b.WriteString("      tags: [" + rs.Tag + "]\n")
			b.WriteString("      summary: Get a " + rs.Name + "\n")
			b.WriteString("      operationId: get" + rs.Schema + "\n")
			b.WriteString("      responses:\n")
			writeOK(b, "The "+rs.Name+".", rs.Schema+"Envelope")
			writeRef(b, "400", "BadRequest")
			writeRef(b, "401", "Unauthorized")
			writeRef(b, "403", "Forbidden")
			writeRef(b, "404", "NotFound")
			return
		}
		b.WriteString("    get:\n")
		b.WriteString("      tags: [" + rs.Tag + "]\n")
		b.WriteString("      summary: List " + strings.ToLower(rs.Tag) + " in the resolved tenant\n")
		b.WriteString("      operationId: list" + rs.Tag + "\n")
		b.WriteString("      " + mcpHint + "{tool: list_" + snakeCase(rs.Tag) + "}\n")
		b.WriteString("      parameters:\n")
		b.WriteString("        - {$ref: \"#/components/parameters/Page\"}\n")
		b.WriteString("        - {$ref: \"#/components/parameters/PerPage\"}\n")
		if hasStringField(rs.Request) {
			b.WriteString("        - {$ref: \"#/components/parameters/Search\"}\n")
		}
		b.WriteString("      responses:\n")
		writeOK(b, "Paginated "+strings.ToLower(rs.Tag)+".", rs.Schema+"PageEnvelope")
		writeRef(b, "401", "Unauthorized")
		writeRef(b, "403", "Forbidden")
		writeRef(b, "429", "TooManyRequests")
	case "post":
		b.WriteString("    post:\n")
		b.WriteString("      tags: [" + rs.Tag + "]\n")
		b.WriteString("      summary: Create a " + rs.Name + "\n")
		b.WriteString("      operationId: create" + rs.Schema + "\n")
		b.WriteString("      requestBody:\n")
		b.WriteString("        required: true\n")
		b.WriteString("        content:\n")
		b.WriteString("          application/json:\n")
		b.WriteString("            schema: {$ref: \"#/components/schemas/" + rs.Schema + "UpsertRequest\"}\n")
		b.WriteString("      responses:\n")
		writeCreated(b, "Created "+rs.Name+".", rs.Schema+"Envelope")
		writeRef(b, "400", "BadRequest")
		writeRef(b, "401", "Unauthorized")
		writeRef(b, "403", "Forbidden")
		writeRef(b, "422", "ValidationFailed")
	case "put", "patch":
		b.WriteString("    " + method + ":\n")
		b.WriteString("      tags: [" + rs.Tag + "]\n")
		b.WriteString("      summary: Update a " + rs.Name + "\n")
		b.WriteString("      operationId: update" + rs.Schema + "\n")
		b.WriteString("      requestBody:\n")
		b.WriteString("        required: true\n")
		b.WriteString("        content:\n")
		b.WriteString("          application/json:\n")
		b.WriteString("            schema: {$ref: \"#/components/schemas/" + rs.Schema + "UpsertRequest\"}\n")
		b.WriteString("      responses:\n")
		writeOK(b, "The updated "+rs.Name+".", rs.Schema+"Envelope")
		writeRef(b, "400", "BadRequest")
		writeRef(b, "401", "Unauthorized")
		writeRef(b, "403", "Forbidden")
		writeRef(b, "404", "NotFound")
		writeRef(b, "422", "ValidationFailed")
	case "delete":
		b.WriteString("    delete:\n")
		b.WriteString("      tags: [" + rs.Tag + "]\n")
		b.WriteString("      summary: Delete (soft) a " + rs.Name + "\n")
		b.WriteString("      operationId: delete" + rs.Schema + "\n")
		b.WriteString("      responses:\n")
		b.WriteString("        \"204\": {description: Deleted.}\n")
		writeRef(b, "400", "BadRequest")
		writeRef(b, "401", "Unauthorized")
		writeRef(b, "403", "Forbidden")
		writeRef(b, "404", "NotFound")
	}
}

func writeOK(b *strings.Builder, description, schema string) {
	b.WriteString("        \"200\":\n")
	b.WriteString("          description: " + description + "\n")
	b.WriteString("          content:\n")
	b.WriteString("            application/json:\n")
	b.WriteString("              schema: {$ref: \"#/components/schemas/" + schema + "\"}\n")
}

func writeCreated(b *strings.Builder, description, schema string) {
	b.WriteString("        \"201\":\n")
	b.WriteString("          description: " + description + "\n")
	b.WriteString("          content:\n")
	b.WriteString("            application/json:\n")
	b.WriteString("              schema: {$ref: \"#/components/schemas/" + schema + "\"}\n")
}

func writeRef(b *strings.Builder, status, response string) {
	b.WriteString("        \"" + status + "\": {$ref: \"#/components/responses/" + response + "\"}\n")
}

func hasStringField(fields []SpecField) bool {
	for _, f := range fields {
		if f.Type == "string" && f.Format == "" {
			return true
		}
	}
	return false
}

// HasRegions reports whether both managed regions are present.
func HasRegions(data []byte) bool {
	s := string(data)
	return strings.Contains(s, markerPathsBegin) && strings.Contains(s, markerSchemasBegin)
}

// ParseIndex extracts the owned resource names from the paths marker line.
func ParseIndex(data []byte) []string {
	s := string(data)
	start, after, ok := locateLine(s, markerPathsBegin)
	if !ok {
		return nil
	}
	line := s[start:after]
	idx := strings.Index(line, "resources:")
	if idx < 0 {
		return nil
	}
	rest := line[idx+len("resources:"):]
	if cut := strings.Index(rest, "\u2014 do not edit"); cut >= 0 {
		rest = rest[:cut]
	}
	var names []string
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			names = append(names, part)
		}
	}
	return names
}

// SpliceRegions replaces the bodies of both managed regions with the rendered
// content for specs, updates the resources index, and validates the result
// with kin-openapi. It never touches text outside the markers and aborts (with
// no output) on a schema or path collision.
func SpliceRegions(data []byte, specs []ResourceSpec) ([]byte, error) {
	if !HasRegions(data) {
		return nil, fmt.Errorf("managed regions missing: run `ims gen openapi --init` once")
	}
	s := string(data)
	nl := "\n"
	if strings.Contains(s, "\r\n") {
		nl = "\r\n"
	}
	sorted := SortSpecs(specs)

	stripped := stripRegions(strings.ReplaceAll(s, "\r\n", "\n"))
	for _, rs := range sorted {
		for _, name := range []string{
			rs.Schema, rs.Schema + "UpsertRequest", rs.Schema + "Envelope", rs.Schema + "PageEnvelope",
		} {
			if containsSchema(stripped, name) {
				return nil, fmt.Errorf("schema %q already exists outside the generated region; rename the resource or move the hand-written schema", name)
			}
		}
		for _, p := range rs.Paths {
			path := APIBase + "/" + rs.Name + p.Suffix
			if containsPath(stripped, path) {
				return nil, fmt.Errorf("path %q already exists outside the generated region; remove the hand-written path first", path)
			}
		}
	}

	res := RenderRegions(sorted)
	// An operation someone exposed as an MCP tool keeps its x-mcp line.
	res.Paths = keepMCPMarks(res.Paths, mcpMarks(strings.ReplaceAll(s, "\r\n", "\n")))
	result := spliceRegion(s, markerPathsBegin, markerPathsEnd, pathsBeginLine(sorted), toNewlines(res.Paths, nl), nl)
	result = spliceRegion(result, markerSchemasBegin, markerSchemasEnd, schemasBeginLine, toNewlines(res.Schemas, nl), nl)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData([]byte(result))
	if err != nil {
		return nil, fmt.Errorf("generated OpenAPI document does not parse: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("generated OpenAPI document is invalid: %w", err)
	}
	return []byte(result), nil
}

func pathsBeginLine(specs []ResourceSpec) string {
	names := make([]string, 0, len(specs))
	for _, rs := range specs {
		names = append(names, rs.Name)
	}
	return "  # \u2500\u2500\u2500 BEGIN GENERATED PATHS (ims gen openapi) \u2014 resources: " +
		strings.Join(names, ", ") + " \u2014 do not edit \u2500\u2500\u2500"
}

const (
	pathsEndLine     = "  # \u2500\u2500\u2500 END GENERATED PATHS \u2500\u2500\u2500"
	schemasBeginLine = "    # \u2500\u2500\u2500 BEGIN GENERATED SCHEMAS (ims gen openapi) \u2014 do not edit \u2500\u2500\u2500"
	schemasEndLine   = "    # \u2500\u2500\u2500 END GENERATED SCHEMAS \u2500\u2500\u2500"
)

func toNewlines(body, nl string) string {
	if body == "" || nl == "\n" {
		return body
	}
	return strings.ReplaceAll(body, "\n", nl)
}

// spliceRegion replaces the begin marker line and everything up to (but not
// including) the end marker line with newBegin + content.
func spliceRegion(s, beginMarker, endMarker, newBegin, content, nl string) string {
	bStart, _, ok := locateLine(s, beginMarker)
	if !ok {
		return s
	}
	eStart, _, ok := locateLine(s, endMarker)
	if !ok {
		return s
	}
	var b strings.Builder
	b.WriteString(newBegin)
	b.WriteString(nl)
	if content != "" {
		b.WriteString(content)
		if !strings.HasSuffix(content, nl) {
			b.WriteString(nl)
		}
	}
	return s[:bStart] + b.String() + s[eStart:]
}

// stripRegions removes both marker regions (markers included) so collision
// checks only see hand-written content.
func stripRegions(s string) string {
	for _, pair := range [][2]string{
		{markerPathsBegin, markerPathsEnd},
		{markerSchemasBegin, markerSchemasEnd},
	} {
		bStart, _, ok := locateLine(s, pair[0])
		if !ok {
			continue
		}
		_, eAfter, ok := locateLine(s, pair[1])
		if !ok {
			continue
		}
		s = s[:bStart] + s[eAfter:]
	}
	return s
}

func containsSchema(stripped, name string) bool {
	return strings.Contains(stripped, "\n    "+name+":") ||
		strings.HasPrefix(stripped, "    "+name+":")
}

func containsPath(stripped, path string) bool {
	return strings.Contains(stripped, "\n  "+path+":") ||
		strings.HasPrefix(stripped, "  "+path+":")
}

// InitRegions inserts the two empty managed regions if they are absent. It is
// idempotent: a document that already has them is returned unchanged.
func InitRegions(data []byte) ([]byte, error) {
	if HasRegions(data) {
		return data, nil
	}
	s := string(data)
	nl := "\n"
	if strings.Contains(s, "\r\n") {
		nl = "\r\n"
	}

	pathsRegion := pathsBeginLine(nil) + nl + pathsEndLine + nl
	compStart := strings.Index(s, "\ncomponents:")
	if compStart < 0 {
		return nil, fmt.Errorf("could not find a top-level `components:` mapping to anchor the paths region")
	}
	s = s[:compStart+1] + pathsRegion + s[compStart+1:]

	_, scAfter, ok := findExactLine(s, "  schemas:")
	if !ok {
		return nil, fmt.Errorf("could not find `components.schemas` to anchor the schemas region")
	}
	schemasRegion := schemasBeginLine + nl + schemasEndLine + nl
	insertAt := endOfMapping(s, scAfter)
	if insertAt >= len(s) {
		if !strings.HasSuffix(s, nl) {
			s += nl
		}
		s += schemasRegion
	} else {
		s = s[:insertAt] + schemasRegion + s[insertAt:]
	}
	return []byte(s), nil
}

// endOfMapping returns the byte offset where the block starting at start ends
// (the first following line indented less than two spaces), or len(s) at EOF.
func endOfMapping(s string, start int) int {
	i := start
	for i < len(s) {
		nl := strings.IndexByte(s[i:], '\n')
		var end int
		if nl < 0 {
			end = len(s)
		} else {
			end = i + nl
		}
		line := strings.TrimRight(s[i:end], "\r")
		if strings.TrimSpace(line) != "" && leadingSpaces(line) < 4 {
			return i
		}
		if nl < 0 {
			return len(s)
		}
		i = end + 1
	}
	return len(s)
}

func leadingSpaces(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

// locateLine returns the byte offsets of the first line containing marker:
// start (line start) and after (start of the following line).
func locateLine(s, marker string) (start, after int, ok bool) {
	i := 0
	for {
		nl := strings.IndexByte(s[i:], '\n')
		var end int
		if nl < 0 {
			end = len(s)
		} else {
			end = i + nl
		}
		if strings.Contains(s[i:end], marker) {
			after = end
			if nl >= 0 {
				after = end + 1
			}
			return i, after, true
		}
		if nl < 0 {
			return 0, 0, false
		}
		i = end + 1
	}
}

// findExactLine returns the offsets of the first line exactly equal to want
// (ignoring a trailing CR).
func findExactLine(s, want string) (start, after int, ok bool) {
	i := 0
	for {
		nl := strings.IndexByte(s[i:], '\n')
		var end int
		if nl < 0 {
			end = len(s)
		} else {
			end = i + nl
		}
		if strings.TrimRight(s[i:end], "\r") == want {
			after = end
			if nl >= 0 {
				after = end + 1
			}
			return i, after, true
		}
		if nl < 0 {
			return 0, 0, false
		}
		i = end + 1
	}
}
