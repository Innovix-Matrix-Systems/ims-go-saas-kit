// Package rename turns a copy of this starter into your own app: it rewrites
// the Go module path, the project, deploy and binary names, the display title
// and the deploy environment-variable prefix in every tracked text file, and
// renames files named after the starter (systemd units). It runs once, right
// after creating a project from the template: `make rename MODULE=...`.
//
// What it deliberately leaves alone: the seeded e-mail addresses
// (superadmin@ims.com), credit links to Innovix Matrix System, its site and its
// other starters, and any text between "rename:keep" and "rename:end" markers.
package rename

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The starter's own names. OldModule is read from go.mod instead, so the
// tool reports clearly when it has already been run.
const (
	OldRepoName   = "ims-go-saas-kit" // repository, docs site, worker, deploy name
	OldDeployName = OldRepoName       // systemd units, image, install paths
	OldBinary     = "ims"             // binary, CLI, service user
	OldTitle      = "IMS Go SaaS Kit"
	OldShortTitle = "IMS Go SaaS"
	OldBrand      = "IMS"
	OldEnvPrefix  = "IMS_"
)

// Options are the new names. Only Module is required; the rest derive from it.
type Options struct {
	Module string // e.g. github.com/acme/shop-api
	Name   string // project and deploy name; default: last element of Module
	Binary string // binary, CLI and service user; default: Name
	Title  string // display name; default: Name in title case ("Shop API")
	DryRun bool   // report only, change nothing
	Force  bool   // allow uncommitted changes
}

// Names are the resolved new names.
type Names struct {
	Module    string
	Name      string
	Binary    string
	Title     string
	EnvPrefix string // e.g. SHOP_API_
}

// FileChange is one rewritten (and possibly renamed) file.
type FileChange struct {
	Path    string // path before renaming, relative to the root
	NewPath string // equal to Path unless the file is renamed
	Edits   int    // replacements made in the content
}

// Result is what a run changed (or would change, in a dry run).
type Result struct {
	OldModule string
	Names     Names
	Files     []FileChange
}

var (
	slugPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	modulePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*(/[A-Za-z0-9._~-]+)+$`)
	moduleLine    = regexp.MustCompile(`(?m)^module\s+(\S+)\s*$`)

	// Upstream references that must keep their names: the studio's domain and
	// sibling starters on GitHub (anything but this repository itself).
	protectedPatterns = []*regexp.Regexp{
		// Text that describes the starter's names on purpose (the rename docs)
		// sits between "rename:keep" and "rename:end" markers, written in the
		// file's own comment syntax.
		regexp.MustCompile(`(?s)rename:keep.*?rename:end`),
		regexp.MustCompile(`[A-Za-z0-9.-]*innovixmatrixsystem[A-Za-z0-9./_-]*`),
		regexp.MustCompile(`github\.com/Innovix-Matrix-Systems/[A-Za-z0-9._-]+`),
		regexp.MustCompile(`[A-Za-z0-9._%+-]*@ims\b(\.com)?`), // seeded accounts, also partial ("superadmin@ims")
	}
	envPrefixPattern = regexp.MustCompile(`\bIMS_([A-Z])`)
	binaryPattern    = regexp.MustCompile(`\bims\b`)
	brandPattern     = regexp.MustCompile(`\bIMS\b`)
)

// Resolve validates the options and fills in the derived names.
func Resolve(opts Options) (Names, error) {
	module := strings.TrimSpace(opts.Module)
	if module == "" {
		return Names{}, errors.New("MODULE is required, e.g. MODULE=github.com/acme/shop-api")
	}
	if !modulePattern.MatchString(module) {
		return Names{}, fmt.Errorf("MODULE %q is not a module path like github.com/acme/shop-api", module)
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = strings.ToLower(module[strings.LastIndex(module, "/")+1:])
	}
	if !slugPattern.MatchString(name) {
		return Names{}, fmt.Errorf("NAME %q must be lowercase letters, digits and dashes, e.g. shop-api", name)
	}
	binary := strings.TrimSpace(opts.Binary)
	if binary == "" {
		binary = name
	}
	if !slugPattern.MatchString(binary) {
		return Names{}, fmt.Errorf("BINARY %q must be lowercase letters, digits and dashes, e.g. shop", binary)
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = titleCase(name)
	}
	return Names{
		Module:    module,
		Name:      name,
		Binary:    binary,
		Title:     title,
		EnvPrefix: strings.ToUpper(strings.ReplaceAll(binary, "-", "_")) + "_",
	}, nil
}

// titleCase turns "shop-api" into "Shop API".
func titleCase(slug string) string {
	acronyms := map[string]bool{"api": true, "ui": true, "id": true, "crm": true, "erp": true, "ims": true, "cms": true, "hr": true, "pos": true}
	words := strings.Split(slug, "-")
	for i, word := range words {
		if acronyms[word] {
			words[i] = strings.ToUpper(word)
		} else if word != "" {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// Run renames the project rooted at root.
func Run(root string, opts Options) (*Result, error) {
	names, err := Resolve(opts)
	if err != nil {
		return nil, err
	}
	oldModule, err := currentModule(root)
	if err != nil {
		return nil, err
	}
	if oldModule == names.Module {
		return nil, fmt.Errorf("the module is already %s", names.Module)
	}
	if !strings.HasSuffix(oldModule, "/"+OldRepoName) {
		return nil, fmt.Errorf("go.mod declares %s: this project has already been renamed (the tool only renames the starter's own names)", oldModule)
	}
	files, tracked, err := listFiles(root)
	if err != nil {
		return nil, err
	}
	if tracked && !opts.Force && !opts.DryRun {
		if dirty, err := hasChanges(root); err != nil {
			return nil, err
		} else if dirty {
			return nil, errors.New("there are uncommitted changes: commit or stash them first (or pass FORCE=1), so the rename is one reviewable diff")
		}
	}

	result := &Result{OldModule: oldModule, Names: names}
	for _, rel := range files {
		if skip(rel) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		newPath := renamePath(rel, names)
		if isBinary(data) {
			if newPath != rel {
				result.Files = append(result.Files, FileChange{Path: rel, NewPath: newPath})
			}
			continue
		}
		var rewritten string
		var edits int
		if moduleOnly(rel) {
			edits = strings.Count(string(data), oldModule)
			rewritten = strings.ReplaceAll(string(data), oldModule, names.Module)
		} else {
			rewritten, edits = Rewrite(string(data), oldModule, names)
		}
		if edits == 0 && newPath == rel {
			continue
		}
		result.Files = append(result.Files, FileChange{Path: rel, NewPath: newPath, Edits: edits})
		if opts.DryRun {
			continue
		}
		out := []byte(rewritten)
		if strings.HasSuffix(rel, ".go") {
			if formatted, err := format.Source(out); err == nil {
				out = formatted
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
			return nil, err
		}
		if newPath != rel {
			target := filepath.Join(root, filepath.FromSlash(newPath))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			if err := os.Rename(path, target); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	return result, nil
}

// Rewrite applies every rename rule to one file's text and returns it with
// the number of replacements. Each new value is swapped for a placeholder as
// soon as it is written, so a later rule can never rewrite it again (a module
// path containing "ims", say), and protected upstream references are masked
// for the whole pass.
func Rewrite(text, oldModule string, names Names) (string, int) {
	var held []string
	hold := func(value string) string {
		held = append(held, value)
		return fmt.Sprintf("\x00%d\x00", len(held)-1)
	}
	edits := 0

	// The module path first: it contains the repository name.
	if n := strings.Count(text, oldModule); n > 0 {
		text = strings.ReplaceAll(text, oldModule, hold(names.Module))
		edits += n
	}
	for _, pattern := range protectedPatterns {
		text = pattern.ReplaceAllStringFunc(text, hold)
	}
	pairs := [][2]string{}
	for _, title := range []string{OldTitle, OldShortTitle} {
		// Titles also appear URL-encoded (an otpauth:// example in the OpenAPI
		// spec): replace those whole, encoded the same way, before the brand
		// rule below would rewrite only their first word.
		pairs = append(pairs,
			[2]string{title, names.Title},
			[2]string{strings.ReplaceAll(title, " ", "%20"), url.PathEscape(names.Title)},
			[2]string{strings.ReplaceAll(title, " ", "+"), url.QueryEscape(names.Title)},
		)
	}
	pairs = append(pairs, [2]string{OldRepoName, names.Name}, [2]string{OldDeployName, names.Name})
	for _, pair := range pairs {
		if n := strings.Count(text, pair[0]); n > 0 {
			text = strings.ReplaceAll(text, pair[0], hold(pair[1]))
			edits += n
		}
	}
	text = envPrefixPattern.ReplaceAllStringFunc(text, func(match string) string {
		edits++
		return hold(names.EnvPrefix) + match[len(OldEnvPrefix):]
	})
	text = binaryPattern.ReplaceAllStringFunc(text, func(string) string {
		edits++
		return hold(names.Binary)
	})
	text = brandPattern.ReplaceAllStringFunc(text, func(string) string {
		edits++
		return hold(names.Title)
	})

	if len(held) == 0 {
		return text, edits
	}
	var out strings.Builder
	for {
		start := strings.IndexByte(text, 0)
		if start < 0 {
			out.WriteString(text)
			break
		}
		end := strings.IndexByte(text[start+1:], 0)
		if end < 0 {
			out.WriteString(text)
			break
		}
		var index int
		if _, err := fmt.Sscanf(text[start+1:start+1+end], "%d", &index); err != nil || index >= len(held) {
			out.WriteString(text[:start+1])
			text = text[start+1:]
			continue
		}
		out.WriteString(text[:start])
		out.WriteString(held[index])
		text = text[start+1+end+1:]
	}
	return out.String(), edits
}

// renamePath renames files named after the deploy name (systemd units).
func renamePath(rel string, names Names) string {
	dir, base := filepath.ToSlash(filepath.Dir(rel)), filepath.Base(rel)
	if !strings.Contains(base, OldDeployName) {
		return rel
	}
	base = strings.Replace(base, OldRepoName, names.Name, 1)
	base = strings.Replace(base, OldDeployName, names.Name, 1)
	if dir == "." {
		return base
	}
	return dir + "/" + base
}

// skip leaves out files that must not be rewritten: this package (its
// constants are the starter's names) and go.sum.
func skip(rel string) bool {
	return strings.HasPrefix(rel, "internal/rename/") || strings.HasSuffix(rel, "go.sum")
}

// moduleOnly files only get the module path rewritten: the command imports
// this package by module path, and its help text names the starter.
func moduleOnly(rel string) bool {
	return strings.HasPrefix(rel, "cmd/rename/")
}

func currentModule(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w (run from the project root)", err)
	}
	match := moduleLine.FindSubmatch(data)
	if match == nil {
		return "", errors.New("go.mod has no module line")
	}
	return string(match[1]), nil
}

// listFiles returns the git-tracked files, or every file outside build and
// dependency folders when root is not a git checkout.
func listFiles(root string) ([]string, bool, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		var files []string
		for _, name := range strings.Split(string(out), "\x00") {
			if name != "" {
				files = append(files, name)
			}
		}
		return files, true, nil
	}
	skipDirs := map[string]bool{".git": true, "node_modules": true, "dist": true, "tmp": true, "bin": true, "vendor": true, ".astro": true}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, false, err
}

func hasChanges(root string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

func isBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0
}
