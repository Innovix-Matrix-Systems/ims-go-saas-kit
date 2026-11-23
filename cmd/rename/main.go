// Command rename turns a copy of the starter into your own app. Run it once,
// from the project root, right after creating your repository from the
// template:
//
//	make rename MODULE=github.com/acme/shop-api
//	make rename MODULE=github.com/acme/shop-api NAME=shop-api BINARY=shop TITLE="Shop API"
//	go run ./cmd/rename --module github.com/acme/shop-api --dry-run
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/rename"
)

func main() {
	var opts rename.Options
	flag.StringVar(&opts.Module, "module", "", "new Go module path, e.g. github.com/acme/shop-api (required)")
	flag.StringVar(&opts.Name, "name", "", "project and deploy name (default: last element of the module)")
	flag.StringVar(&opts.Binary, "binary", "", "binary, CLI and service-user name (default: the name)")
	flag.StringVar(&opts.Title, "title", "", `display name (default: the name in title case, e.g. "Shop API")`)
	flag.BoolVar(&opts.DryRun, "dry-run", false, "list what would change, change nothing")
	flag.BoolVar(&opts.Force, "force", false, "run even with uncommitted changes")
	flag.Parse()

	result, err := rename.Run(".", opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rename:", err)
		os.Exit(1)
	}

	n := result.Names
	verb := "Renamed"
	if opts.DryRun {
		verb = "Would rename"
	}
	fmt.Printf("%s %s\n", verb, result.OldModule)
	fmt.Printf("  module      %s\n  name        %s\n  binary      %s\n  title       %s\n  env prefix  %s\n\n",
		n.Module, n.Name, n.Binary, n.Title, n.EnvPrefix)
	edits := 0
	for _, file := range result.Files {
		edits += file.Edits
		switch {
		case file.NewPath != file.Path:
			fmt.Printf("  %-58s → %s (%d edits)\n", file.Path, file.NewPath, file.Edits)
		default:
			fmt.Printf("  %-58s %d edits\n", file.Path, file.Edits)
		}
	}
	fmt.Printf("\n%d files, %d edits.\n", len(result.Files), edits)
	if opts.DryRun {
		return
	}
	fmt.Print(`
Next (make rename runs the first two for you):
  go mod tidy
  make generate docs-gen     # the generated contract embeds the renamed spec
  make check                 # format, vet, lint, unit tests
  git add -A && git commit -m "chore: rename the starter to ` + n.Name + `"

Then review: the seeded accounts still use @ims.com e-mails (internal/seeder),
and the README still credits the starter. Set APP_NAME and the deploy paths in
your .env files.
`)
}
