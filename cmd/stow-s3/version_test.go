package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/version"
)

// The binary could not identify itself at all before this: `stow-s3 --version`
// printed the usage text and exited 1, and `stow-s3 serve --version` died on
// `flag provided but not defined`.
func TestVersionIsAnswerableAtEveryLevel(t *testing.T) {
	for _, args := range [][]string{
		{"--version"},
		{"-version"},
		{"-v"},
		{"version"},
	} {
		if !versionRequested(args) {
			t.Errorf("versionRequested(%q) = false, want true", args)
		}
		if !topLevelVersion(args) {
			t.Errorf("topLevelVersion(%q) did not handle the request", args)
		}
	}
}

func TestVersionIsNotAnsweredForACommand(t *testing.T) {
	// The check is on the first argument alone, so a command whose name starts
	// with the same letters must still dispatch.
	for _, args := range [][]string{{"serve"}, {"workspace"}, {"versions"}, {"prepare"}, {}} {
		if versionRequested(args) {
			t.Errorf("versionRequested(%q) = true, want false", args)
		}
	}
}

func TestPrintVersionWritesOnlyWhenAsked(t *testing.T) {
	var out bytes.Buffer
	if printVersion(&out, false) {
		t.Error("printVersion reported that it handled a request it was not given")
	}
	if out.Len() != 0 {
		t.Errorf("printVersion wrote %q without being asked", out.String())
	}
	if !printVersion(&out, true) {
		t.Error("printVersion did not report handling a request it was given")
	}
	if got := strings.TrimSpace(out.String()); got != versionLine {
		t.Errorf("printed %q, want %q", got, versionLine)
	}
}

// The two answers must be the same answer. A bug report quoting `--version` beside
// a doctor report carrying a different number reads as two facts rather than one
// drift.
func TestEveryVersionAnswerIsTheSameValue(t *testing.T) {
	if want := "stow-s3 " + version.Version; versionLine != want {
		t.Fatalf("versionLine = %q, want %q", versionLine, want)
	}

	report, err := collectDoctorReport()
	if err != nil {
		t.Fatalf("collect doctor report: %v", err)
	}
	if report.BinaryVersion != version.Version {
		t.Errorf("doctor reports %q, the version flag prints %q", report.BinaryVersion, version.Version)
	}

	// And the subcommand flags resolve to the same variable, so a caller can read
	// either spelling.
	flags := flag.NewFlagSet("version-test", flag.ContinueOnError)
	flags.SetOutput(&nullWriter{})
	show := versionFlag(flags)
	if *show {
		t.Error("--version defaulted to true")
	}
	if err := flags.Parse([]string{"--version"}); err != nil {
		t.Fatalf("parse --version: %v", err)
	}
	if !*show {
		t.Error("--version did not set its variable")
	}
	if err := flags.Parse([]string{"-v"}); err != nil {
		t.Fatalf("parse -v: %v", err)
	}
	if !*show {
		t.Error("-v did not set the same variable as --version")
	}
}

func TestUsageNamesTheVersionFlag(t *testing.T) {
	// Someone told to check the version is most likely reading the usage text.
	var out bytes.Buffer
	usageTo(&out)
	if !strings.Contains(out.String(), "stow-s3 --version") {
		t.Errorf("usage text does not mention the version flag:\n%s", out.String())
	}
}
