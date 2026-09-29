package main

import (
	"strings"
	"testing"
)

// The dispatch layer, tested in process. It was at zero, and a stranger's first command
// goes through exactly it. Why the rest of this package is not in-process-tested is
// recorded under E2 in docs/foss-readiness-plan.md.

func TestWorkspaceUsageNamesEveryVerb(t *testing.T) {
	usage := workspaceUsage()
	for verb := range workspaceVerbs {
		if !strings.Contains(usage, verb) {
			t.Errorf("the usage line does not name the verb %q: %s", verb, usage)
		}
	}
}

func TestWorkspaceUsageIsStable(t *testing.T) {
	first := workspaceUsage()
	for attempt := 0; attempt < 20; attempt += 1 {
		if got := workspaceUsage(); got != first {
			t.Fatalf("the usage line changed between calls:\n  %s\n  %s", first, got)
		}
	}
}

func TestSortedWorkspaceVerbsIsSortedAndComplete(t *testing.T) {
	verbs := sortedWorkspaceVerbs()
	if len(verbs) != len(workspaceVerbs) {
		t.Fatalf("sortedWorkspaceVerbs returned %d verbs and the table holds %d", len(verbs), len(workspaceVerbs))
	}
	for index := 1; index < len(verbs); index += 1 {
		if verbs[index-1] > verbs[index] {
			t.Fatalf("the verb list is not sorted at %d: %q before %q", index, verbs[index-1], verbs[index])
		}
	}
}

// `serve --help` prints usage and exits zero, so the two command groups agree.
func TestWorkspaceHelpSpellingsPrintUsage(t *testing.T) {
	for _, spelling := range []string{"-h", "--h", "-help", "--help", "help"} {
		t.Run(spelling, func(t *testing.T) {
			out := string(captureWorkspaceCommand(t, func() error { return workspaceCommand([]string{spelling}) }))
			if !strings.Contains(out, "usage: stow-s3 workspace") {
				t.Errorf("workspace %s printed %q, want the usage line", spelling, out)
			}
			for verb := range workspaceVerbs {
				if !strings.Contains(out, verb) {
					t.Errorf("the help for %q omits the verb %q", spelling, verb)
				}
			}
		})
	}
}

func TestWorkspaceWithNoVerbReturnsTheUsageLine(t *testing.T) {
	err := workspaceCommand(nil)
	if err == nil {
		t.Fatal("workspace with no verb returned no error")
	}
	if !strings.Contains(err.Error(), "usage: stow-s3 workspace") {
		t.Errorf("workspace with no verb = %q, want the usage line", err)
	}
}

// A version request names no verb, so it is answered before the verb table.
func TestWorkspaceVersionIsAnsweredBeforeTheVerbTable(t *testing.T) {
	for _, spelling := range []string{"--version", "-version", "version"} {
		t.Run(spelling, func(t *testing.T) {
			out := string(captureWorkspaceCommand(t, func() error { return workspaceCommand([]string{spelling}) }))
			if !strings.Contains(out, "stow-s3") {
				t.Errorf("workspace %s printed %q, want a version line", spelling, out)
			}
			if strings.Contains(out, "usage: stow-s3 workspace") {
				t.Errorf("workspace %s printed the usage line, so the verb table answered it", spelling)
			}
		})
	}
}

func TestWorkspaceRejectsAnUnknownVerb(t *testing.T) {
	err := workspaceCommand([]string{"definitely-not-a-verb"})
	if err == nil {
		t.Fatal("an unknown verb returned no error")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-verb") {
		t.Errorf("the refusal %q does not name the verb the caller asked for", err)
	}
}
