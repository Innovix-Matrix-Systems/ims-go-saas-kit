// Command skills keeps the Claude Code copy of the agent skills in step with
// the canonical ones, in Go so it runs the same in any shell (PowerShell has
// no `diff -r`, `rm -rf` or `cp -r`):
//
//	go run ./cmd/skills sync   # copy .agents/skills to .claude/skills
//	go run ./cmd/skills check  # fail when the two differ
//
// `make skills-sync` and `make skills-check` call it.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const (
	source = ".agents/skills"
	target = ".claude/skills"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "sync" && os.Args[1] != "check") {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/skills sync|check")
		os.Exit(2)
	}
	var err error
	if os.Args[1] == "sync" {
		err = syncSkills()
	} else {
		err = checkSkills()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "skills:", err)
		os.Exit(1)
	}
}

// syncSkills replaces the copy with the canonical skills.
func syncSkills() error {
	files, err := readTree(source)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	for rel, data := range files {
		path := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("copied %d files from %s to %s\n", len(files), source, target)
	return nil
}

// checkSkills lists every file that is missing, extra or different in the copy.
func checkSkills() error {
	want, err := readTree(source)
	if err != nil {
		return err
	}
	got, err := readTree(target)
	if err != nil {
		return err
	}
	var problems []string
	for rel, data := range want {
		other, ok := got[rel]
		switch {
		case !ok:
			problems = append(problems, "missing in "+target+": "+rel)
		case !bytes.Equal(data, other):
			problems = append(problems, "differs: "+rel)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			problems = append(problems, "only in "+target+": "+rel)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	return fmt.Errorf("%s is out of date: run `make skills-sync`", target)
}

// readTree reads every file under root, keyed by its slash-separated path.
// A missing root is an empty tree, so a first sync and a check both work.
func readTree(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if os.IsNotExist(err) {
		return files, nil
	}
	return files, err
}
