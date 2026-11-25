package privacy

import (
	"fmt"
	"io"
	"text/tabwriter"

	contracts "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/privacy"
)

// WriteCoverage prints the coverage table (`ims privacy:coverage` and the
// dashboard command) and returns how many columns are not covered.
func WriteCoverage(out io.Writer, coverage []contracts.Coverage) (int, error) {
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "COLUMN\tHANDLED BY")
	missing := 0
	for _, entry := range coverage {
		switch {
		case entry.Contributor != "":
			fmt.Fprintf(w, "%s\t%s\n", entry.Column, entry.Contributor)
		case entry.Exemption != "":
			fmt.Fprintf(w, "%s\texempt: %s\n", entry.Column, entry.Exemption)
		default:
			missing++
			fmt.Fprintf(w, "%s\tNOT COVERED\n", entry.Column)
		}
	}
	return missing, w.Flush()
}

// CoverageError is the message for uncovered columns.
func CoverageError(missing int) error {
	return fmt.Errorf("%d user-reference column(s) are not covered: add them to a privacy contributor or exempt them in internal/app/privacy.go", missing)
}
