package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/scaffold"
)

// sqlcVersion must match the Makefile/CI pin so generated code stays in sync.
const sqlcVersion = "v1.31.1"

func registerGenCommands(registry *Registry, _ *State) error {
	return registry.Add(newGenCommand())
}

func newGenCommand() *cobra.Command {
	gen := &cobra.Command{
		Use:   "gen",
		Short: "Scaffold code from declarative specs",
	}
	gen.AddCommand(newGenModuleCommand())
	gen.AddCommand(newGenOpenAPICommand())
	gen.AddCommand(newGenDocsCommand())
	return gen
}

func newGenModuleCommand() *cobra.Command {
	var (
		name       string
		fieldsSpec string
		table      string
		pkg        string
		root       string
		modulePath string
		dryRun     bool
		force      bool
		noSqlc     bool
		noOpenapi  bool
	)

	cmd := &cobra.Command{
		Use:   "module",
		Short: "Scaffold a CRUD module from a declarative field spec",
		Long: "Generate a goose migration, sqlc queries and a feature-first module package\n" +
			"from a field spec. Non-interactive and CI-friendly.",
		Example: `  ims gen module --name=product --fields="name:string,price:decimal,is_active:bool"
  ims gen module --name=product --fields="name:string,notes:text?" --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGenModule(cmd, genModuleArgs{
				name:       name,
				fieldsSpec: fieldsSpec,
				table:      table,
				pkg:        pkg,
				root:       root,
				modulePath: modulePath,
				dryRun:     dryRun,
				force:      force,
				noSqlc:     noSqlc,
				noOpenapi:  noOpenapi,
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "module name, lower_snake, singular (required)")
	cmd.Flags().StringVar(&fieldsSpec, "fields", "", "comma-separated name:type spec, e.g. name:string,notes:text?")
	cmd.Flags().StringVar(&table, "table", "", "table name (default: naive plural of --name)")
	cmd.Flags().StringVar(&pkg, "package", "", "go package name (default: --name)")
	cmd.Flags().StringVar(&root, "root", ".", "repository root")
	cmd.Flags().StringVar(&modulePath, "module", "", "go module path (default: read from go.mod)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the files without writing them")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().BoolVar(&noSqlc, "no-sqlc", false, "skip the automatic `sqlc generate` step")
	cmd.Flags().BoolVar(&noOpenapi, "no-openapi", false, "skip refreshing the generated OpenAPI regions")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("fields")
	return cmd
}

type genModuleArgs struct {
	name       string
	fieldsSpec string
	table      string
	pkg        string
	root       string
	modulePath string
	dryRun     bool
	force      bool
	noSqlc     bool
	noOpenapi  bool
}

func runGenModule(cmd *cobra.Command, args genModuleArgs) error {
	fields, err := scaffold.ParseFields(args.fieldsSpec)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(args.root)
	if err != nil {
		return err
	}
	modulePath := args.modulePath
	if modulePath == "" {
		modulePath, err = readModulePath(root)
		if err != nil {
			return err
		}
	}
	next, err := nextMigrationNumber(root)
	if err != nil {
		return err
	}

	files, err := scaffold.Generate(scaffold.Options{
		ModulePath: modulePath,
		Package:    args.pkg,
		Name:       args.name,
		Table:      args.table,
		NextNumber: next,
		Fields:     fields,
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if args.dryRun {
		fmt.Fprintln(out, "dry run — the following files would be written:")
		for _, f := range files {
			fmt.Fprintf(out, "  %s\n", f.Path)
		}
		return nil
	}

	for _, f := range files {
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		if _, statErr := os.Stat(abs); statErr == nil && !args.force {
			return fmt.Errorf("%s already exists (use --force to overwrite)", f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, f.Content, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s\n", f.Path)
	}

	if !args.noSqlc {
		fmt.Fprintln(out, "running sqlc generate…")
		if err := runSqlc(cmd, root); err != nil {
			return err
		}
	}

	if !args.noOpenapi {
		fmt.Fprintln(out, "refreshing generated OpenAPI regions…")
		spec := scaffold.SpecFromFields(args.name, fields)
		if err := refreshOpenAPI(cmd, root, []scaffold.ResourceSpec{spec}, nil, false, false); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, `
Module %q generated.

Next steps:
  1. Wire the handler in internal/app/wiring.go and mount it in internal/app/routes.go.
  2. Add permissions %q, %q, %q and %q to internal/authz/catalog.go,
     then run `+"`ims permission:sync`"+`.
  3. Run `+"`make sqlc`"+` (if skipped) and `+"`go build ./...`"+`.
`, args.name,
		args.name+".view", args.name+".create", args.name+".update", args.name+".delete")
	return nil
}

func readModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("module path not found in go.mod (pass --module)")
}

var migrationNameRe = regexp.MustCompile(`^(\d+)_`)

func nextMigrationNumber(root string) (int, error) {
	dir := filepath.Join(root, "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read migrations dir: %w", err)
	}
	numbers := make([]int, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationNameRe.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		n, convErr := strconv.Atoi(match[1])
		if convErr != nil {
			continue
		}
		numbers = append(numbers, n)
	}
	if len(numbers) == 0 {
		return 1, nil
	}
	sort.Ints(numbers)
	return numbers[len(numbers)-1] + 1, nil
}

func runSqlc(cmd *cobra.Command, root string) error {
	sqlc := exec.Command("go", "run", "github.com/sqlc-dev/sqlc/cmd/sqlc@"+sqlcVersion, "generate")
	sqlc.Dir = root
	sqlc.Stdout = cmd.OutOrStdout()
	sqlc.Stderr = cmd.ErrOrStderr()
	sqlc.Env = os.Environ()
	return sqlc.Run()
}
