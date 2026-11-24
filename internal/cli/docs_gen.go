package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/docsgen"
)

func newGenDocsCommand() *cobra.Command {
	var (
		root  string
		check bool
	)
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate the reference pages of the documentation site",
		Long: "Render docs/reference/{cli,configuration,permissions,make}.md from the code: the\n" +
			"command tree, .env.example, the permission catalog and the Makefile. With --check\n" +
			"nothing is written and the command fails when a committed page is stale (CI).",
		Example: `  ims gen docs
  ims gen docs --check`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			envExample, err := os.ReadFile(filepath.Join(root, ".env.example"))
			if err != nil {
				return err
			}
			makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
			if err != nil {
				return err
			}
			pages := docsgen.Render(docsgen.Inputs{Root: cmd.Root(), EnvExample: envExample, Makefile: makefile})
			out := cmd.OutOrStdout()
			var stale []string
			for _, page := range pages {
				target := filepath.Join(root, filepath.FromSlash(page.Path))
				current, _ := os.ReadFile(target)
				if bytes.Equal(current, page.Content) {
					continue
				}
				if check {
					stale = append(stale, page.Path)
					continue
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(target, page.Content, 0o644); err != nil {
					return err
				}
				fmt.Fprintln(out, "wrote", page.Path)
			}
			if len(stale) > 0 {
				return fmt.Errorf("generated docs are stale (run `make docs-gen`): %v", stale)
			}
			if check {
				fmt.Fprintln(out, "generated docs are current")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", ".", "repository root")
	cmd.Flags().BoolVar(&check, "check", false, "fail when a generated page is stale instead of writing it")
	return cmd
}
