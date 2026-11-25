package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/privacy"
)

func registerPrivacyCommands(registry *Registry, state *State) error {
	for _, cmd := range []*cobra.Command{
		newPrivacyEraseCommand(state),
		newPrivacyExportCommand(state),
		newPrivacyCoverageCommand(state),
		newPrivacyLedgerCommand(state),
		newPrivacyReplayCommand(state),
	} {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

// newPrivacyEraseCommand erases a user now (no grace period), for answering
// a data-subject request through support. It calls privacy.Service.Erase,
// like the HTTP routes will.
func newPrivacyEraseCommand(state *State) *cobra.Command {
	var userID int64
	var force bool

	cmd := &cobra.Command{
		Use:   "privacy:erase",
		Short: "Erase a user's personal data now",
		Long: "Erase a user's personal data in every module and keep the users row as a tombstone " +
			"(docs/design/account-deletion-and-export.md). It cannot be undone, so it needs --force. " +
			"The super admin and a live tenant's last active Admin are refused.",
		Example: "  ims privacy:erase --user=42 --force",
		RunE: func(cmd *cobra.Command, args []string) error {
			if userID < 1 {
				return errors.New("--user is required")
			}
			if !force {
				return errors.New("erasure cannot be undone: pass --force to erase the user")
			}
			return withPlatform(state, func(platform *app.Platform) error {
				request, err := platform.Privacy.Erase(cmd.Context(), privacy.EraseInput{UserID: userID, Source: privacy.SourceCLI})
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				totals := request.Report.Totals()
				fmt.Fprintf(out, "Erased user %d (request %d): %d row(s) deleted, %d anonymized.\n",
					userID, request.ID, totals.Deleted, totals.Anonymized)
				writePrivacyReport(cmd, request.Report)
				return nil
			})
		},
	}
	cmd.Flags().Int64Var(&userID, "user", 0, "id of the user to erase")
	cmd.Flags().BoolVar(&force, "force", false, "confirm the erasure (it cannot be undone)")
	return cmd
}

// newPrivacyExportCommand writes a user's export zip to a file.
func newPrivacyExportCommand(state *State) *cobra.Command {
	var userID int64
	var outPath string

	cmd := &cobra.Command{
		Use:     "privacy:export",
		Short:   "Export a user's personal data to a zip file",
		Long:    "Write every module's data about a user (JSON per module, stored files, manifest.json) to a zip file. Secrets are never included.",
		Example: "  ims privacy:export --user=42 --out=user-42.zip",
		RunE: func(cmd *cobra.Command, args []string) error {
			if userID < 1 {
				return errors.New("--user is required")
			}
			if outPath == "" {
				outPath = fmt.Sprintf("user-%d-export.zip", userID)
			}
			return withPlatform(state, func(platform *app.Platform) error {
				file, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err != nil {
					return fmt.Errorf("create %s: %w", outPath, err)
				}
				manifest, err := platform.Privacy.Export(cmd.Context(), privacy.ExportInput{UserID: userID, Source: privacy.SourceCLI}, file)
				closeErr := file.Close()
				if err != nil {
					_ = os.Remove(outPath)
					return err
				}
				if closeErr != nil {
					return fmt.Errorf("write %s: %w", outPath, closeErr)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Exported user %d to %s (%d file(s) from %d module(s)).\n",
					userID, outPath, len(manifest.Files), len(manifest.Contributors))
				return nil
			})
		},
	}
	cmd.Flags().Int64Var(&userID, "user", 0, "id of the user to export")
	cmd.Flags().StringVar(&outPath, "out", "", "zip file to create (default: user-<id>-export.zip; never overwritten)")
	return cmd
}

// newPrivacyCoverageCommand lists every user-reference column and what
// handles it; it fails when one is not accounted for.
func newPrivacyCoverageCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "privacy:coverage",
		Short: "Show which module erases each user-reference column",
		Long:  "List every database column that may point at a user, the module whose privacy contributor handles it or the reason it is exempt. Exits with an error when a column is not covered.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				coverage, err := platform.Privacy.Coverage(cmd.Context())
				if err != nil {
					return err
				}
				missing, err := privacy.WriteCoverage(cmd.OutOrStdout(), coverage)
				if err != nil {
					return err
				}
				if missing > 0 {
					return privacy.CoverageError(missing)
				}
				return nil
			})
		},
	}
}

// newPrivacyLedgerCommand writes the ids of users erased since a date, one
// per line: the input of privacy:replay. Run it against the live database
// before restoring a backup, since the restore brings back the old ledger.
func newPrivacyLedgerCommand(state *State) *cobra.Command {
	var since, outPath string

	cmd := &cobra.Command{
		Use:   "privacy:ledger",
		Short: "List the users erased since a date (save it before a restore)",
		Long: "Write the ids of users erased since --since (RFC 3339 date or date-time), one per line, to stdout or --out. " +
			"Run it on the live database before restoring a backup, then `ims privacy:replay --from=<file>` after the restore.",
		Example: "  ims privacy:ledger --since=2026-11-01 --out=erased.txt",
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := parseSince(since)
			if err != nil {
				return err
			}
			return withPlatform(state, func(platform *app.Platform) error {
				ids, err := platform.Privacy.ErasedSince(cmd.Context(), from)
				if err != nil {
					return err
				}
				var b strings.Builder
				for _, id := range ids {
					b.WriteString(strconv.FormatInt(id, 10))
					b.WriteByte('\n')
				}
				if outPath == "" {
					_, err := io.WriteString(cmd.OutOrStdout(), b.String())
					return err
				}
				if err := os.WriteFile(outPath, []byte(b.String()), 0o600); err != nil {
					return fmt.Errorf("write %s: %w", outPath, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d erased user id(s) to %s.\n", len(ids), outPath)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "erasures completed at or after this date (RFC 3339; required)")
	cmd.Flags().StringVar(&outPath, "out", "", "file to write (default: stdout)")
	return cmd
}

// newPrivacyReplayCommand erases again the users a backup restore brought
// back (docs/operations/rollback.md).
func newPrivacyReplayCommand(state *State) *cobra.Command {
	var from string

	cmd := &cobra.Command{
		Use:   "privacy:replay",
		Short: "Erase again the users a backup restore brought back",
		Long: "Read user ids (one per line, from `ims privacy:ledger`) and erase every one that is not erased in this database. " +
			"Users still erased or no longer present are skipped; the guards do not apply, since the erasure was already decided.",
		Example: "  ims privacy:replay --from=erased.txt",
		RunE: func(cmd *cobra.Command, args []string) error {
			if from == "" {
				return errors.New("--from is required")
			}
			data, err := os.ReadFile(from)
			if err != nil {
				return fmt.Errorf("read %s: %w", from, err)
			}
			var ids []int64
			for i, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				id, err := strconv.ParseInt(line, 10, 64)
				if err != nil || id < 1 {
					return fmt.Errorf("%s line %d: %q is not a user id", from, i+1, line)
				}
				ids = append(ids, id)
			}
			return withPlatform(state, func(platform *app.Platform) error {
				replayed, err := platform.Privacy.Replay(cmd.Context(), ids)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Erased %d of %d user(s) again; the rest were still erased or gone.\n", replayed, len(ids))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "file of user ids written by privacy:ledger (required)")
	return cmd
}

func parseSince(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("--since is required")
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.DateOnly, value); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--since %q is not a date (2026-11-01) or RFC 3339 time", value)
}

func writePrivacyReport(cmd *cobra.Command, report privacy.Report) {
	names := make([]string, 0, len(report))
	for name := range report {
		names = append(names, name)
	}
	sort.Strings(names)
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "MODULE\tDELETED\tANONYMIZED")
	for _, name := range names {
		fmt.Fprintf(w, "%s\t%d\t%d\n", name, report[name].Deleted, report[name].Anonymized)
	}
	_ = w.Flush()
}
