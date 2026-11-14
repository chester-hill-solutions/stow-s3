package main

import (
	"flag"
	"strings"
	"testing"
)

func TestMCPCheckpointSelectionFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "requires resources", args: []string{"--checkpoint-id", "cp_0123456789abcdef01234567"}, want: "requires --workspace-files"},
		{name: "empty selected checkpoint", args: []string{"--workspace-files", "--checkpoint-id="}, want: "non-empty"},
		{name: "missing checkpoint value", args: []string{"--workspace-files", "--checkpoint-id"}, want: "flag needs an argument"},
		{name: "object mode conflict", args: []string{"--object-dir", "/tmp/objects", "--workspace-files", "--checkpoint-id", "cp_0123456789abcdef01234567"}, want: "incompatible"},
		{name: "browser conflict", args: []string{"--workspace-files", "--checkpoint-id", "cp_0123456789abcdef01234567", "--browser"}, want: "incompatible"},
		{name: "accepted saved selection", args: []string{"--workspace-files", "--checkpoint-id", "cp_0123456789abcdef01234567"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
			registerMCPFlags(flags)
			parseErr := flags.Parse(testCase.args)
			if testCase.want != "" {
				if parseErr == nil {
					_, parseErr = validateMCPMode(flags)
				}
				if parseErr == nil || !strings.Contains(parseErr.Error(), testCase.want) {
					t.Fatalf("error = %v, want containing %q", parseErr, testCase.want)
				}
				return
			}
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			objectMode, err := validateMCPMode(flags)
			if err != nil || objectMode {
				t.Fatalf("validateMCPMode = (%v, %v)", objectMode, err)
			}
		})
	}
}
