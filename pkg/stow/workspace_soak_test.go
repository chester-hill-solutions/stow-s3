package stow_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestParallelPreparedWorkspacesStayIsolatedAndCleanUp(t *testing.T) {
	const rounds = 3
	const width = 8
	base := t.TempDir()
	registry := filepath.Join(base, "registry")
	for round := range rounds {
		runWorkspaceSoakRound(t, base, registry, round, width)
	}
}

type soakWorkspaceTask struct {
	marker string
	source string
	root   string
}

func runWorkspaceSoakRound(t *testing.T, base, registry string, round, width int) {
	t.Helper()
	roundDir := filepath.Join(base, fmt.Sprintf("round-%d", round))
	if err := os.MkdirAll(roundDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tasks := createSoakWorkspaceTasks(t, roundDir, round, width)
	prepared := make([]*stow.PreparedWorkspace, width)
	t.Cleanup(func() { cleanupPreparedWorkspaces(prepared) })
	prepareSoakWorkspaces(t, tasks, registry, prepared)
	verifySoakWorkspaceIsolation(t, tasks, prepared, round)
	destroySoakWorkspaces(t, tasks, prepared, round)
}

func createSoakWorkspaceTasks(t *testing.T, roundDir string, round, width int) []soakWorkspaceTask {
	t.Helper()
	tasks := make([]soakWorkspaceTask, width)
	for index := range tasks {
		marker := fmt.Sprintf("round-%d-workspace-%d", round, index)
		tasks[index] = soakWorkspaceTask{
			marker: marker,
			source: filepath.Join(roundDir, fmt.Sprintf("input-%d.txt", index)),
			root:   filepath.Join(roundDir, fmt.Sprintf("workspace-%d", index)),
		}
		if err := os.WriteFile(tasks[index].source, []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return tasks
}

func prepareSoakWorkspaces(t *testing.T, tasks []soakWorkspaceTask, registry string, prepared []*stow.PreparedWorkspace) {
	t.Helper()
	granted := stow.ReadWrite().With(stow.EnvironmentDestroy)
	prepareErrors := make([]error, len(tasks))
	var workers sync.WaitGroup
	for index, item := range tasks {
		workers.Add(1)
		go func() {
			defer workers.Done()
			prepared[index], prepareErrors[index] = stow.PrepareWorkspace(stow.PrepareOptions{
				WorkspaceOptions: stow.WorkspaceOptions{Dir: item.root, RegistryDir: registry, Authority: &granted},
				Inputs:           []stow.WorkspaceInput{{Source: item.source, Destination: "marker.txt"}},
			})
		}()
	}
	workers.Wait()
	for index, err := range prepareErrors {
		if err != nil {
			t.Fatalf("prepare workspace %d: %v", index, err)
		}
	}
}

func verifySoakWorkspaceIsolation(t *testing.T, tasks []soakWorkspaceTask, prepared []*stow.PreparedWorkspace, round int) {
	t.Helper()
	ids := make(map[string]struct{}, len(tasks))
	for index, item := range tasks {
		verifyUniqueSoakID(t, ids, prepared[index].Workspace.ID(), round)
		verifySoakFile(t, soakFileExpectation{
			path: filepath.Join(item.root, "marker.txt"), want: item.marker,
			kind: "workspace", round: round, index: index,
		})
		verifySoakFile(t, soakFileExpectation{
			path: item.source, want: item.marker, kind: "source", round: round, index: index,
		})
	}
}

func verifyUniqueSoakID(t *testing.T, ids map[string]struct{}, id string, round int) {
	t.Helper()
	if _, exists := ids[id]; exists {
		t.Fatalf("round %d returned duplicate workspace ID %q", round, id)
	}
	ids[id] = struct{}{}
}

type soakFileExpectation struct {
	path  string
	want  string
	kind  string
	round int
	index int
}

func verifySoakFile(t *testing.T, expectation soakFileExpectation) {
	t.Helper()
	got, err := os.ReadFile(expectation.path)
	if err != nil || string(got) != expectation.want {
		t.Fatalf("round %d %s %d = %q, %v; want %q", expectation.round, expectation.kind, expectation.index, got, err, expectation.want)
	}
}

func destroySoakWorkspaces(t *testing.T, tasks []soakWorkspaceTask, prepared []*stow.PreparedWorkspace, round int) {
	t.Helper()
	for index, item := range tasks {
		workspace := prepared[index].Workspace
		if err := workspace.Destroy(context.Background()); err != nil {
			t.Fatalf("round %d destroy %d: %v", round, index, err)
		}
		if err := workspace.Close(); err != nil {
			t.Fatalf("round %d close %d: %v", round, index, err)
		}
		if _, err := os.Lstat(item.root); !os.IsNotExist(err) {
			t.Fatalf("round %d workspace %d root survived destroy: %v", round, index, err)
		}
	}
}

func cleanupPreparedWorkspaces(prepared []*stow.PreparedWorkspace) {
	for _, item := range prepared {
		if item != nil {
			_ = item.Workspace.Destroy(context.Background())
			_ = item.Workspace.Close()
		}
	}
}
