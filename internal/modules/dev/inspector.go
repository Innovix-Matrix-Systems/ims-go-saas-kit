package dev

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	project "github.com/Innovix-Matrix-Systems/ims-go-saas-kit"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	webdev "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/web/dev"
)

// InspectorService answers "what is this app made of": its routes, roles and
// permissions, effective configuration (secrets masked) and dependencies —
// the Laravel `route:list` / `about` / `config:show` views in one place.
type InspectorService struct {
	queries *dbgen.Queries
	cfg     *config.Config
	routes  func() chi.Routes
}

// NewInspectorService creates the inspector. routes is called lazily because
// the router is built after the dashboard handler.
func NewInspectorService(queries *dbgen.Queries, cfg *config.Config, routes func() chi.Routes) *InspectorService {
	return &InspectorService{queries: queries, cfg: cfg, routes: routes}
}

// ── routes ────────────────────────────────────────────────────────────────

// RouteInfo is one registered route.
type RouteInfo struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Handler     string   `json:"handler"`
	Permissions []string `json:"permissions"`
	Middleware  []string `json:"middleware"` // route-specific, after the global stack
}

// RouteReport is the route table plus the middleware every route runs.
type RouteReport struct {
	Global []string    `json:"global"`
	Routes []RouteInfo `json:"routes"`
}

const modulePrefix = "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/"

var funcSuffix = regexp.MustCompile(`(\.func\d+)+$|-fm$`)

// middlewareAliases gives middleware the short names Laravel uses.
var middlewareAliases = map[string]string{
	"Authenticate":    "auth",
	"Resolve":         "tenant.resolve",
	"Exempt":          "tenant.exempt",
	"InternalOnly":    "tenant.internal",
	"Gate":            "dev.enabled",
	"RequestID":       "request-id",
	"RealIP":          "real-ip",
	"PublicOrigin":    "public-origin",
	"SecurityHeaders": "security-headers",
	"CORS":            "cors",
	"AccessLog":       "access-log",
	"Recover":         "recover",
	"HTTPMiddleware":  "telescope",
	"Guard":           "maintenance",
	"Timeout":         "timeout",
	"Middleware":      "throttle",
	"ProtectOps":      "ops",
}

// Routes lists every route of the API router.
func (s *InspectorService) Routes() (*RouteReport, error) {
	report := &RouteReport{Global: []string{}, Routes: []RouteInfo{}}
	if s.routes == nil || s.routes() == nil {
		return report, nil
	}
	byPath := map[string][]RouteInfo{}
	var paths []string
	err := chi.Walk(s.routes(), func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		route = strings.ReplaceAll(route, "/*/", "/")
		info := RouteInfo{Method: method, Path: route, Handler: handlerName(handler),
			Permissions: []string{}, Middleware: []string{}}
		for _, mw := range middlewares {
			if permission, ok := gatePermission(mw); ok {
				info.Permissions = append(info.Permissions, permission)
				info.Middleware = append(info.Middleware, "can:"+permission)
				continue
			}
			info.Middleware = append(info.Middleware, middlewareName(mw))
		}
		if _, seen := byPath[route]; !seen {
			paths = append(paths, route)
		}
		byPath[route] = append(byPath[route], info)
		return nil
	})
	for _, path := range paths {
		routes := byPath[path]
		// Handle()/Mount() register every method: show one ANY row.
		if len(routes) >= 8 {
			routes[0].Method = "ANY"
			routes = routes[:1]
		}
		report.Routes = append(report.Routes, routes...)
	}
	report.Global = commonPrefix(report.Routes)
	for i := range report.Routes {
		report.Routes[i].Middleware = report.Routes[i].Middleware[len(report.Global):]
	}
	sort.SliceStable(report.Routes, func(i, j int) bool {
		if report.Routes[i].Path == report.Routes[j].Path {
			return methodOrder(report.Routes[i].Method) < methodOrder(report.Routes[j].Method)
		}
		return report.Routes[i].Path < report.Routes[j].Path
	})
	return report, err
}

func commonPrefix(routes []RouteInfo) []string {
	if len(routes) == 0 {
		return []string{}
	}
	prefix := append([]string{}, routes[0].Middleware...)
	for _, route := range routes[1:] {
		n := 0
		for n < len(prefix) && n < len(route.Middleware) && prefix[n] == route.Middleware[n] {
			n++
		}
		prefix = prefix[:n]
	}
	return prefix
}

func middlewareName(mw func(http.Handler) http.Handler) string {
	full := funcName(mw)
	if strings.Contains(full, "Translator") {
		return "locale"
	}
	parts := strings.Split(full, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.Trim(parts[i], "()")
		if part == "" || strings.HasPrefix(part, "func") || strings.Trim(part, "0123456789") == "" {
			continue
		}
		if alias, ok := middlewareAliases[part]; ok {
			return alias
		}
		return part
	}
	return full
}

func methodOrder(method string) int {
	return strings.Index("GET HEAD POST PUT PATCH DELETE OPTIONS", method)
}

// gatePermission wraps a probe handler with the middleware and reports the
// permission when it is an authz.Require gate.
func gatePermission(mw func(http.Handler) http.Handler) (permission string, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if gate, isGate := mw(http.NotFoundHandler()).(*authz.Gate); isGate {
		return gate.Permission(), true
	}
	return "", false
}

func handlerName(handler http.Handler) string {
	if fn, ok := handler.(http.HandlerFunc); ok {
		name := funcName(fn)
		if strings.HasSuffix(name, "StripPrefix") {
			return "storage.StaticHandler"
		}
		return name
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", handler), "*")
}

func funcName(fn any) string {
	value := reflect.ValueOf(fn)
	if value.Kind() != reflect.Func {
		return fmt.Sprintf("%T", fn)
	}
	f := runtime.FuncForPC(value.Pointer())
	if f == nil {
		return "func"
	}
	name := strings.TrimPrefix(f.Name(), modulePrefix)
	name = strings.TrimPrefix(name, "internal/")
	name = strings.TrimPrefix(name, "modules/")
	name = strings.TrimPrefix(name, "platform/")
	name = funcSuffix.ReplaceAllString(name, "")
	return strings.ReplaceAll(name, "(*", "(")
}

// ── roles & permissions ───────────────────────────────────────────────────

// RoleInfo is one platform role.
type RoleInfo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Guard       string `json:"guard"`
	IsSystem    bool   `json:"is_system"`
	Permissions int64  `json:"permissions"`
	Users       int64  `json:"users"`
}

// PermissionInfo is one catalog or stored permission.
type PermissionInfo struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
	Declared    bool   `json:"declared"` // in internal/authz/catalog.go
	Stored      bool   `json:"stored"`   // in the permissions table
}

// AccessReport compares the permission catalog with the database.
type AccessReport struct {
	Roles              []RoleInfo       `json:"roles"`
	TenantRoles        int64            `json:"tenant_roles"`
	Permissions        []PermissionInfo `json:"permissions"`
	DeclaredNotStored  []string         `json:"declared_not_stored"`
	StoredNotDeclared  []string         `json:"stored_not_declared"`
	DeveloperBaseline  []string         `json:"developer_permissions"`
	AdminTenantDefault []string         `json:"admin_permissions"`
}

// Access lists roles and permissions and how the catalog and DB differ.
func (s *InspectorService) Access(ctx context.Context) (*AccessReport, error) {
	roles, err := s.queries.DevInspectorRoles(ctx)
	if err != nil {
		return nil, database.MapError("dev.inspector.roles", err)
	}
	tenantRoles, err := s.queries.DevInspectorTenantRoleCount(ctx)
	if err != nil {
		return nil, database.MapError("dev.inspector.tenant_roles", err)
	}
	stored, err := s.queries.DevInspectorPermissions(ctx)
	if err != nil {
		return nil, database.MapError("dev.inspector.permissions", err)
	}

	report := &AccessReport{TenantRoles: tenantRoles, DeclaredNotStored: []string{}, StoredNotDeclared: []string{}}
	for _, role := range roles {
		report.Roles = append(report.Roles, RoleInfo{ID: role.ID, Name: role.Name, Guard: role.GuardName,
			IsSystem: role.IsSystem, Permissions: role.PermissionCount, Users: role.UserCount})
	}
	byName := map[string]*PermissionInfo{}
	for _, permission := range authz.Catalog {
		byName[permission.Name] = &PermissionInfo{Name: permission.Name, Group: permission.Group,
			Description: permission.Description, IsSystem: permission.IsSystem, Declared: true}
	}
	for _, row := range stored {
		info, ok := byName[row.Name]
		if !ok {
			description := ""
			if row.Description != nil {
				description = *row.Description
			}
			info = &PermissionInfo{Name: row.Name, Group: row.Group, Description: description, IsSystem: row.IsSystem}
			byName[row.Name] = info
		}
		info.Stored = true
	}
	for _, info := range byName {
		report.Permissions = append(report.Permissions, *info)
		switch {
		case info.Declared && !info.Stored:
			report.DeclaredNotStored = append(report.DeclaredNotStored, info.Name)
		case info.Stored && !info.Declared:
			report.StoredNotDeclared = append(report.StoredNotDeclared, info.Name)
		}
	}
	sort.Slice(report.Permissions, func(i, j int) bool {
		if report.Permissions[i].Group == report.Permissions[j].Group {
			return report.Permissions[i].Name < report.Permissions[j].Name
		}
		return report.Permissions[i].Group < report.Permissions[j].Group
	})
	sort.Strings(report.DeclaredNotStored)
	sort.Strings(report.StoredNotDeclared)
	for _, permission := range authz.DeveloperPermissions() {
		report.DeveloperBaseline = append(report.DeveloperBaseline, permission.Name)
	}
	for _, permission := range authz.AdminTenantPermissions() {
		report.AdminTenantDefault = append(report.AdminTenantDefault, permission.Name)
	}
	return report, nil
}

// ── config ────────────────────────────────────────────────────────────────

// ConfigEntry is one effective configuration value.
type ConfigEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Masked bool   `json:"masked"`
}

// ConfigSection groups entries by top-level section (app, http, auth, ...).
type ConfigSection struct {
	Name    string        `json:"name"`
	Entries []ConfigEntry `json:"entries"`
}

var secretKey = regexp.MustCompile(`(?i)(password|secret|token|apikey|api_key|accesskey|access_key|secretkey|private|dsn|appkey|app_key|credential)`)

// Config flattens the loaded configuration. Values whose key looks secret
// are masked, and so are credentials inside URLs.
func (s *InspectorService) Config() []ConfigSection {
	root := reflect.ValueOf(s.cfg).Elem()
	sections := []ConfigSection{}
	for i := 0; i < root.NumField(); i++ {
		field := root.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		section := ConfigSection{Name: snake(field.Name), Entries: []ConfigEntry{}}
		flattenConfig(snake(field.Name), root.Field(i), &section.Entries)
		sections = append(sections, section)
	}
	return sections
}

func flattenConfig(prefix string, value reflect.Value, out *[]ConfigEntry) {
	switch value.Kind() {
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(time.Time{}) {
			*out = append(*out, configEntry(prefix, value.Interface()))
			return
		}
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			flattenConfig(prefix+"."+snake(field.Name), value.Field(i), out)
		}
	case reflect.Map:
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
		for _, key := range keys {
			flattenConfig(prefix+"."+fmt.Sprint(key.Interface()), value.MapIndex(key), out)
		}
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Struct {
			for i := 0; i < value.Len(); i++ {
				flattenConfig(fmt.Sprintf("%s.%d", prefix, i), value.Index(i), out)
			}
			return
		}
		items := make([]string, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			items = append(items, fmt.Sprint(value.Index(i).Interface()))
		}
		*out = append(*out, configEntry(prefix, strings.Join(items, ", ")))
	case reflect.Pointer:
		if value.IsNil() {
			*out = append(*out, configEntry(prefix, "null"))
			return
		}
		flattenConfig(prefix, value.Elem(), out)
	default:
		*out = append(*out, configEntry(prefix, value.Interface()))
	}
}

func configEntry(key string, value any) ConfigEntry {
	text := fmt.Sprint(value)
	if duration, ok := value.(time.Duration); ok {
		text = duration.String()
	}
	last := key[strings.LastIndex(key, ".")+1:]
	if secretKey.MatchString(strings.ReplaceAll(last, "_", "")) || secretKey.MatchString(last) {
		if text == "" {
			return ConfigEntry{Key: key, Value: ""}
		}
		return ConfigEntry{Key: key, Value: "********", Masked: true}
	}
	if parsed, err := url.Parse(text); err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "xxxxx")
			return ConfigEntry{Key: key, Value: strings.Replace(parsed.String(), "xxxxx", "********", 1), Masked: true}
		}
	}
	return ConfigEntry{Key: key, Value: text}
}

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var acronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)

func snake(name string) string {
	name = acronymBoundary.ReplaceAllString(name, "${1}_${2}")
	return strings.ToLower(camelBoundary.ReplaceAllString(name, "${1}_${2}"))
}

// ── packages ──────────────────────────────────────────────────────────────

// PackageInfo is one dependency.
type PackageInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"`    // require | indirect | dependency | devDependency
	Runtime string `json:"runtime"` // go | npm
}

// PackagesReport lists the Go modules and dashboard npm packages.
type PackagesReport struct {
	GoVersion string        `json:"go_version"`
	Module    string        `json:"module"`
	Packages  []PackageInfo `json:"packages"`
}

// Packages reads go.mod and the dashboard package.json baked into the binary.
func (s *InspectorService) Packages() PackagesReport {
	report := PackagesReport{GoVersion: runtime.Version(), Packages: []PackageInfo{}}
	if info, ok := debug.ReadBuildInfo(); ok {
		report.Module = info.Main.Path
	}
	report.Packages = append(report.Packages, parseGoMod(project.GoMod)...)

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(webdev.PackageJSON, &pkg); err == nil {
		for name, version := range pkg.Dependencies {
			report.Packages = append(report.Packages, PackageInfo{Name: name, Version: version, Kind: "dependency", Runtime: "npm"})
		}
		for name, version := range pkg.DevDependencies {
			report.Packages = append(report.Packages, PackageInfo{Name: name, Version: version, Kind: "devDependency", Runtime: "npm"})
		}
	}
	sort.SliceStable(report.Packages, func(i, j int) bool {
		a, b := report.Packages[i], report.Packages[j]
		if a.Runtime != b.Runtime {
			return a.Runtime < b.Runtime
		}
		if a.Kind != b.Kind {
			return kindOrder(a.Kind) < kindOrder(b.Kind)
		}
		return a.Name < b.Name
	})
	return report
}

func kindOrder(kind string) int {
	return map[string]int{"require": 0, "dependency": 0, "indirect": 1, "devDependency": 1}[kind]
}

func parseGoMod(data []byte) []PackageInfo {
	out := []PackageInfo{}
	inBlock := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require ") && !strings.HasSuffix(line, "("):
			line = strings.TrimPrefix(line, "require ")
		case !inBlock:
			continue
		}
		kind := "require"
		if comment := strings.Index(line, "//"); comment >= 0 {
			if strings.Contains(line[comment:], "indirect") {
				kind = "indirect"
			}
			line = strings.TrimSpace(line[:comment])
		}
		fields := strings.Fields(line)
		if len(fields) == 2 {
			out = append(out, PackageInfo{Name: fields[0], Version: fields[1], Kind: kind, Runtime: "go"})
		}
	}
	return out
}
