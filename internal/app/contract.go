package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	api "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/api"
)

// ValidateSpec parses and structurally validates an OpenAPI document.
func ValidateSpec(spec []byte) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI spec: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate OpenAPI spec: %w", err)
	}
	return doc, nil
}

// ContractMismatch describes a method+path present in only one of the spec or
// the live router.
type ContractMismatch struct {
	Method string
	Path   string
	In     string // "spec" or "router"
}

// RouteDrift compares the routes of a chi router against an OpenAPI document
// and returns the symmetric difference (empty when they agree). '/storage/*' is
// excluded (not part of the JSON API), and so are the static SPA routes: the
// developer dashboard ('/dev', '/dev/*', whose API surface is checked
// separately) and the client app at the site root ('/').
// All-method registrations (chi.Handle) accept whatever methods the spec
// declares for that path.
func RouteDrift(router http.Handler, doc *openapi3.T) ([]ContractMismatch, error) {
	mux, ok := router.(chi.Routes)
	if !ok {
		return nil, fmt.Errorf("router %T does not implement chi.Routes", router)
	}

	specMethods := map[string]map[string]bool{}
	for path, item := range doc.Paths.Map() {
		methods := map[string]bool{}
		for method := range item.Operations() {
			methods[strings.ToUpper(method)] = true
		}
		specMethods[path] = methods
	}

	chiMethods := map[string]map[string]bool{}
	if err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/storage/") || route == "/dev" || route == "/dev/*" || route == "/" {
			return nil
		}
		if chiMethods[route] == nil {
			chiMethods[route] = map[string]bool{}
		}
		chiMethods[route][method] = true
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk routes: %w", err)
	}

	specPairs := map[string]bool{}
	for path, methods := range specMethods {
		for method := range methods {
			specPairs[method+" "+path] = true
		}
	}
	chiPairs := map[string]bool{}
	for path, methods := range chiMethods {
		if len(methods) > 3 {
			for method := range specMethods[path] {
				chiPairs[method+" "+path] = true
			}
			continue
		}
		for method := range methods {
			switch method {
			case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
				chiPairs[method+" "+path] = true
			}
		}
	}

	var mismatches []ContractMismatch
	for pair := range specPairs {
		if !chiPairs[pair] {
			method, path := splitPair(pair)
			mismatches = append(mismatches, ContractMismatch{Method: method, Path: path, In: "spec"})
		}
	}
	for pair := range chiPairs {
		if !specPairs[pair] {
			method, path := splitPair(pair)
			mismatches = append(mismatches, ContractMismatch{Method: method, Path: path, In: "router"})
		}
	}
	return mismatches, nil
}

// CheckEmbeddedContract validates the embedded OpenAPI document and checks it
// against the live router, returning any drift.
func CheckEmbeddedContract(router http.Handler) (*openapi3.T, []ContractMismatch, error) {
	doc, err := ValidateSpec(api.Spec)
	if err != nil {
		return nil, nil, err
	}
	mismatches, err := RouteDrift(router, doc)
	if err != nil {
		return nil, nil, err
	}
	return doc, mismatches, nil
}

func splitPair(pair string) (method, path string) {
	parts := strings.SplitN(pair, " ", 2)
	if len(parts) != 2 {
		return pair, ""
	}
	return parts[0], parts[1]
}
