package privacy

import (
	"fmt"
	"sort"
	"strings"
)

// Registry holds the contributors, guards and exemptions. It is built once in
// the composition root (internal/app/platform.go).
type Registry struct {
	contributors []Contributor
	guards       []Guard
	exempt       map[string]string
	names        map[string]bool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{exempt: map[string]string{}, names: map[string]bool{}}
}

// Register adds a contributor. Contributors erase in registration order;
// register the one that tombstones the users row last. A contributor that is
// also a Guard is registered as one.
func (r *Registry) Register(c Contributor) error {
	if c == nil || c.Name() == "" {
		return fmt.Errorf("privacy: contributor needs a name")
	}
	if r.names[c.Name()] {
		return fmt.Errorf("privacy: contributor %q registered twice", c.Name())
	}
	r.names[c.Name()] = true
	r.contributors = append(r.contributors, c)
	if g, ok := c.(Guard); ok {
		r.guards = append(r.guards, g)
	}
	return nil
}

// Exempt records a user-reference column that is deliberately not exported
// or erased, with the reason the coverage report shows.
func (r *Registry) Exempt(column, reason string) {
	r.exempt[column] = reason
}

// Contributors returns the contributors in registration order.
func (r *Registry) Contributors() []Contributor { return r.contributors }

// Guards returns the guards in registration order.
func (r *Registry) Guards() []Guard { return r.guards }

// Coverage is how one user-reference column is accounted for.
type Coverage struct {
	Column      string // "table.column"
	Contributor string // set when a contributor claims it
	Exemption   string // set when the column is exempt
}

// Covered reports whether the column is accounted for.
func (c Coverage) Covered() bool { return c.Contributor != "" || c.Exemption != "" }

// Cover matches schema columns ("table.column") against the contributors'
// claims and the exemptions. "*.column" claims a column name on every table.
func (r *Registry) Cover(columns []string) []Coverage {
	claims := map[string]string{}
	for _, c := range r.contributors {
		for _, column := range c.Columns() {
			claims[column] = c.Name()
		}
	}
	out := make([]Coverage, 0, len(columns))
	for _, column := range columns {
		entry := Coverage{Column: column}
		_, name, _ := strings.Cut(column, ".")
		switch {
		case claims[column] != "":
			entry.Contributor = claims[column]
		case r.exempt[column] != "":
			entry.Exemption = r.exempt[column]
		case claims["*."+name] != "":
			entry.Contributor = claims["*."+name]
		case r.exempt["*."+name] != "":
			entry.Exemption = r.exempt["*."+name]
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Column < out[j].Column })
	return out
}
