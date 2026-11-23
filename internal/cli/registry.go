package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Registry collects commands from all modules and rejects duplicate or
// unnamed commands during startup.
type Registry struct {
	commands []*cobra.Command
	seen     map[string]struct{}
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{seen: map[string]struct{}{}}
}

// Add registers a command, failing on duplicates.
func (r *Registry) Add(cmd *cobra.Command) error {
	if cmd == nil || cmd.Use == "" {
		return fmt.Errorf("cli: every command must have a name")
	}
	name := cmd.Name()
	if _, exists := r.seen[name]; exists {
		return fmt.Errorf("cli: duplicate command %q", name)
	}
	r.seen[name] = struct{}{}
	r.commands = append(r.commands, cmd)
	return nil
}

// Commands returns the registered commands in registration order.
func (r *Registry) Commands() []*cobra.Command {
	return r.commands
}
