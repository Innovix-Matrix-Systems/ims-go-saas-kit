package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestVersionCommand(t *testing.T) {
	state := &State{}
	root, err := BuildRoot(state)
	if err != nil {
		t.Fatalf("BuildRoot() error = %v", err)
	}

	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(buf.String(), "ims ") {
		t.Errorf("output = %q, want version line", buf.String())
	}
}

func TestScheduleListCommand(t *testing.T) {
	state := &State{}
	root, err := BuildRoot(state)
	if err != nil {
		t.Fatalf("BuildRoot() error = %v", err)
	}

	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"schedule:list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "scheduler.heartbeat") || !strings.Contains(output, "dataprocessing.cleanup-completed") {
		t.Errorf("output = %q, want registered tasks", output)
	}
}

func TestBuildRootRejectsDuplicateCommands(t *testing.T) {
	state := &State{}
	root, err := BuildRoot(state)
	if err != nil {
		t.Fatalf("BuildRoot() error = %v", err)
	}

	registry := NewRegistry()
	if err := registry.Add(&cobra.Command{Use: "version"}); err != nil {
		t.Fatalf("first Add() error = %v", err)
	}
	if err := registry.Add(&cobra.Command{Use: "version"}); err == nil {
		t.Fatal("duplicate Add() = nil error, want error")
	}

	names := map[string]bool{}
	for _, cmd := range root.Commands() {
		if names[cmd.Name()] {
			t.Fatalf("duplicate command %q in root", cmd.Name())
		}
		names[cmd.Name()] = true
	}
}
