package stow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitSeedDoesNotStartMaintenanceBeforeAdmission(t *testing.T) {
	source := budgetGitSource(t)
	trace := filepath.Join(t.TempDir(), "trace.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	root := t.TempDir()
	_, _, _, err := seedPreparedWorkspace(root, PrepareOptions{
		Repositories: []GitRepositoryInput{{Source: source, Destination: "repo", Ref: "HEAD"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	sawFetch := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "start" && containsGitArgument(event.Argv, "fetch") {
			sawFetch = true
		}
		if event.Event == "child_start" && containsGitArgument(event.Argv, "maintenance") {
			t.Fatal("Git seed launched maintenance before metadata admission completed")
		}
	}
	if !sawFetch {
		t.Fatal("trace did not observe the actual seed fetch")
	}
	data, err = os.ReadFile(filepath.Join(root, "repo", "file"))
	if err != nil || string(data) != "payload" {
		t.Fatalf("seed payload = %q, %v", data, err)
	}
}

func containsGitArgument(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
