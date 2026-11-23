package queue

import (
	"encoding/json"
	"fmt"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

// Registry maps job kinds to their definitions. Duplicate or empty kinds are
// rejected at startup.
type Registry struct {
	defs  map[string]Definition
	order []string
}

// NewRegistry creates an empty job registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}}
}

// Register adds a job definition.
func (r *Registry) Register(def Definition) error {
	if def.Kind == "" {
		return fmt.Errorf("queue: job kind must not be empty")
	}
	if _, exists := r.defs[def.Kind]; exists {
		return fmt.Errorf("queue: duplicate job kind %q", def.Kind)
	}
	if def.Handle == nil {
		return fmt.Errorf("queue: job %q must define a handler", def.Kind)
	}
	if def.Queue == "" {
		def.Queue = QueueDefault
	}
	if def.MaxRetries < 0 {
		return fmt.Errorf("queue: job %q: max retries must not be negative", def.Kind)
	}
	r.defs[def.Kind] = def
	r.order = append(r.order, def.Kind)
	return nil
}

// Definition looks up a job kind.
func (r *Registry) Definition(kind string) (Definition, bool) {
	def, ok := r.defs[kind]
	return def, ok
}

// Definitions returns all definitions in registration order.
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.order))
	for _, kind := range r.order {
		out = append(out, r.defs[kind])
	}
	return out
}

// Kinds returns all registered job kinds.
func (r *Registry) Kinds() []string {
	return append([]string(nil), r.order...)
}

// encodePayload marshals a job payload and resolves its definition.
func encodePayload(registry *Registry, kind string, payload any) ([]byte, Definition, error) {
	def, ok := registry.Definition(kind)
	if !ok {
		return nil, Definition{}, apperr.Newf(apperr.InvalidInput, "Unknown job kind %q.", kind)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, def, apperr.Wrap(apperr.InternalServerError, "encode job payload", err)
	}
	return data, def, nil
}

// checkScope refuses tenant-less envelopes for jobs that are not marked as
// system jobs: the worker restores the envelope scope verbatim, and tenant 0
// means "no filter" — an unscoped execution across all tenants.
func checkScope(def Definition, tenantID int64) error {
	if tenantID > 0 || def.SystemJob {
		return nil
	}
	return apperr.Newf(apperr.InvalidInput,
		"Job %q requires a tenant scope: enqueue under a tenant context or mark the kind as a system job.", def.Kind)
}
