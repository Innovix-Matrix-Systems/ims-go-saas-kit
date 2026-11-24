package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/scaffold"
)

const openapiRelPath = "api/openapi.yaml"

func newGenOpenAPICommand() *cobra.Command {
	var (
		module string
		all    bool
		init   bool
		dryRun bool
		check  bool
		root   string
	)

	cmd := &cobra.Command{
		Use:   "openapi",
		Short: "Refresh the generated regions of api/openapi.yaml",
		Long: "Insert or update the generated paths and schemas for one module\n" +
			"(--module), every module in the index (--all), or insert the empty marker\n" +
			"regions (--init). Managed regions are owned by the tool; everything\n" +
			"outside the markers is hand-written and never touched.",
		Example: `  ims gen openapi --init
  ims gen openapi --module=product
  ims gen openapi --all --check
  ims gen openapi --all --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			absRoot, err := filepath.Abs(root)
			if err != nil {
				return err
			}
			if init {
				return initOpenAPI(cmd, absRoot, dryRun)
			}
			if all == (module != "") {
				return fmt.Errorf("pass exactly one of --module or --all")
			}
			var names []string
			if module != "" {
				names = []string{module}
			}
			return refreshOpenAPI(cmd, absRoot, nil, names, dryRun, check)
		},
	}

	cmd.Flags().StringVar(&module, "module", "", "refresh a single module (lower_snake name)")
	cmd.Flags().BoolVar(&all, "all", false, "refresh every module listed in the generated-region index")
	cmd.Flags().BoolVar(&init, "init", false, "insert the empty managed regions (one-time)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the regenerated regions without writing")
	cmd.Flags().BoolVar(&check, "check", false, "exit non-zero when the committed regions are stale")
	cmd.Flags().StringVar(&root, "root", ".", "repository root")
	return cmd
}

func initOpenAPI(cmd *cobra.Command, root string, dryRun bool) error {
	path := filepath.Join(root, openapiRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if scaffold.HasRegions(data) {
		fmt.Fprintln(cmd.OutOrStdout(), "managed regions already present")
		return nil
	}
	updated, err := scaffold.InitRegions(data)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "would insert empty managed regions into %s\n", openapiRelPath)
		return nil
	}
	if err := writeFileAtomic(path, updated); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "inserted empty managed regions into %s\n", openapiRelPath)
	return nil
}

// refreshOpenAPI re-renders the managed regions from the union of the current
// index, any provided specs, and (when needed) analyzed modules.
func refreshOpenAPI(cmd *cobra.Command, root string, provided []scaffold.ResourceSpec, names []string, dryRun, check bool) error {
	path := filepath.Join(root, openapiRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !scaffold.HasRegions(data) {
		return fmt.Errorf("%s has no managed regions; run `ims gen openapi --init` once", openapiRelPath)
	}

	specs, err := collectSpecs(cmd, root, data, provided, names)
	if err != nil {
		return err
	}
	updated, err := scaffold.SpliceRegions(data, specs)
	if err != nil {
		return err
	}

	if check {
		if bytes.Equal(updated, data) {
			fmt.Fprintln(cmd.OutOrStdout(), "generated OpenAPI regions are current")
			return nil
		}
		return fmt.Errorf("generated OpenAPI regions are stale; run `make openapi-gen`")
	}
	if bytes.Equal(updated, data) {
		fmt.Fprintln(cmd.OutOrStdout(), "generated OpenAPI regions already up to date")
		return nil
	}
	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "would update %s\n", openapiRelPath)
		fmt.Fprint(cmd.OutOrStdout(), scaffold.RenderRegions(scaffold.SortSpecs(specs)).Paths)
		return nil
	}
	if err := writeFileAtomic(path, updated); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "updated %s (%d resources)\n", openapiRelPath, len(specs))
	return nil
}

// checkOpenAPIRegions verifies the committed managed regions are current
// without touching the file. It is static (no database required).
func checkOpenAPIRegions(cmd *cobra.Command, root string) error {
	path := filepath.Join(root, openapiRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !scaffold.HasRegions(data) {
		fmt.Fprintln(cmd.OutOrStdout(), "openapi regions not initialised; skipping freshness check")
		return nil
	}
	specs, err := collectSpecs(cmd, root, data, nil, scaffold.ParseIndex(data))
	if err != nil {
		return err
	}
	updated, err := scaffold.SpliceRegions(data, specs)
	if err != nil {
		return err
	}
	if !bytes.Equal(updated, data) {
		return fmt.Errorf("generated OpenAPI regions are stale; run `make openapi-gen`")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "generated OpenAPI regions are current")
	return nil
}

// collectSpecs assembles the full spec list: provided specs win, everything
// else in the index (plus explicitly requested names) is analyzed.
func collectSpecs(cmd *cobra.Command, root string, data []byte, provided []scaffold.ResourceSpec, names []string) ([]scaffold.ResourceSpec, error) {
	byName := map[string]scaffold.ResourceSpec{}
	for _, rs := range provided {
		byName[rs.Name] = rs
	}
	seen := map[string]bool{}
	var ordered []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			ordered = append(ordered, name)
		}
	}
	for _, name := range scaffold.ParseIndex(data) {
		add(name)
	}
	for _, name := range names {
		add(name)
	}
	for name := range byName {
		add(name)
	}
	sort.Strings(ordered)

	warn := cmd.ErrOrStderr()
	specs := make([]scaffold.ResourceSpec, 0, len(ordered))
	for _, name := range ordered {
		if rs, ok := byName[name]; ok {
			specs = append(specs, rs)
			continue
		}
		rs, err := scaffold.AnalyzeModule(root, name)
		if err != nil {
			fmt.Fprintf(warn, "openapi: skipping %q: %v\n", name, err)
			continue
		}
		specs = append(specs, rs)
	}
	return specs, nil
}

func writeFileAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".openapi-*.yaml.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
