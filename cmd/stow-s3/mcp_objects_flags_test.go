package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestMCPRejectsMixedStorageModesBeforeOpening(t *testing.T) {
	cases := [][]string{
		{"--registry-dir", ""}, {"--workspace-id", ""}, {"--max-files", "100000"},
		{"--export-root", ""}, {"--adopt-root", ""}, {"--timeout", "0s"}, {"positional"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "never-opened")
			prefix := []string{"--object-dir", dir, "--bucket", "objects"}
			if err := mcpCommand(append(prefix, args...)); err == nil {
				t.Fatal("mixed mode accepted")
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("invalid flags opened object store: %v", err)
			}
		})
	}
}

func TestMCPRejectsObjectFlagsOnWorkspaceMode(t *testing.T) {
	for _, args := range [][]string{
		{"--max-objects", "10000"}, {"--max-object-bytes", "65536"},
		{"--max-observations", "256"}, {"--create-bucket=false"}, {"--read-only=false"}, {"--browser=false"},
	} {
		t.Run(args[0], func(t *testing.T) {
			prefix := []string{"--registry-dir", t.TempDir(), "--workspace-id", "missing", "--timeout", "1s"}
			if err := mcpCommand(append(prefix, args...)); err == nil {
				t.Fatal("object flag accepted in workspace mode")
			}
		})
	}
}

func TestMCPObjectBoundsAndSelectionValidateBeforeOpen(t *testing.T) {
	for _, extra := range [][]string{
		{"--max-object-bytes", "65537"}, {"--max-observations", "257"}, {"--read-only", "--create-bucket"},
		{"--max-bytes", "-1"}, {"--max-objects", "-1"},
	} {
		t.Run(extra[0], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "never-opened")
			args := []string{"--object-dir", dir, "--bucket", "objects"}
			if err := mcpCommand(append(args, extra...)); err == nil {
				t.Fatal("invalid bounds accepted")
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("invalid bounds opened directory: %v", err)
			}
		})
	}
	for _, args := range [][]string{{"--bucket", "objects"}, {"--object-dir", filepath.Join(t.TempDir(), "missing-bucket")}} {
		if err := mcpCommand(args); err == nil {
			t.Fatalf("incomplete selector accepted: %v", args)
		}
	}
}

func TestMCPHelpListsBothStorageProfiles(t *testing.T) {
	flags := flag.NewFlagSet("test-mcp", flag.ContinueOnError)
	registerMCPFlags(flags)
	for _, name := range []string{"registry-dir", "workspace-id", "timeout", "object-dir", "bucket", "max-objects", "max-object-bytes", "max-observations", "create-bucket", "read-only", "browser"} {
		if flags.Lookup(name) == nil {
			t.Fatalf("help missing --%s", name)
		}
	}
	if err := mcpCommand([]string{"--help"}); err != nil {
		t.Fatalf("help returned failure: %v", err)
	}
}
