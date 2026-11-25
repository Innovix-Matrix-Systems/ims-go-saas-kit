package app

import (
	"fmt"
	"io"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/maintenance"
)

// Output shared by `ims down|up|maintenance:status` and the matching
// dashboard commands, so both read the same.

// WriteMaintenanceDown prints the result of a down (and the secret, once).
func WriteMaintenanceDown(out io.Writer, state maintenance.State, secret string) {
	fmt.Fprintln(out, "The application is now in maintenance mode.")
	fmt.Fprintf(out, "Message:     %s\n", state.Message)
	fmt.Fprintf(out, "Retry-After: %ds\n", state.RetryAfter)
	if secret != "" {
		fmt.Fprintf(out, "Bypass:      send the header %s: %s\n", maintenance.BypassHeader, secret)
		fmt.Fprintln(out, "             (shown once; only its hash is stored)")
	}
}

// WriteMaintenanceUp prints the result of an up.
func WriteMaintenanceUp(out io.Writer, changed bool) {
	if changed {
		fmt.Fprintln(out, "The application is now live.")
		return
	}
	fmt.Fprintln(out, "The application was not in maintenance mode.")
}

// WriteMaintenanceStatus prints the state.
func WriteMaintenanceStatus(out io.Writer, state maintenance.State) {
	if !state.Down {
		fmt.Fprintln(out, "The application is live (not in maintenance mode).")
		return
	}
	fmt.Fprintln(out, "The application is DOWN for maintenance.")
	if state.Since != nil {
		fmt.Fprintf(out, "Since:       %s (%s ago)\n", state.Since.Format(time.RFC3339), time.Since(*state.Since).Round(time.Second))
	}
	fmt.Fprintf(out, "Message:     %s\n", state.Message)
	fmt.Fprintf(out, "Retry-After: %ds\n", state.RetryAfter)
	fmt.Fprintf(out, "Bypass:      %s\n", map[bool]string{true: "secret set (" + maintenance.BypassHeader + ")", false: "none"}[state.HasSecret])
}

func maintenanceSummary(state maintenance.State) string {
	if !state.Down {
		return "off"
	}
	if state.Since != nil {
		return "DOWN since " + state.Since.Format(time.RFC3339)
	}
	return "DOWN"
}
