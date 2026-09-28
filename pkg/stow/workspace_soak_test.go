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
		roundDir := filepath.Join(base, fmt.Sprintf("round-%d", round))
		if err := os.MkdirAll(roundDir, 0o700); err != nil {
			t.Fatal(err)
		}
		type task struct {
			marker string
			source string
			root   string
		}
		tasks := make([]task, width)
		for index := range tasks {
			marker := fmt.Sprintf("round-%d-workspace-%d", round, index)
			tasks[index] = task{
				marker: marker,
				source: filepath.Join(roundDir, fmt.Sprintf("input-%d.txt", index)),
				root:   filepath.Join(roundDir, fmt.Sprintf("workspace-%d", index)),
			}
			if err := os.WriteFile(tasks[index].source, []byte(marker), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		prepared := make([]*stow.PreparedWorkspace, width)
		t.Cleanup(func() {
			for _, workspace := range prepared {
				if workspace != nil {
					_ = workspace.Workspace.Destroy(context.Background())
					_ = workspace.Workspace.Close()
				}
			}
		})
		errors := make([]error, width)
		var wg sync.WaitGroup
		for index, item := range tasks {
			wg.Add(1)
			go func() {
				defer wg.Done()
				prepared[index], errors[index] = stow.PrepareWorkspace(stow.PrepareOptions{
					WorkspaceOptions: stow.WorkspaceOptions{Dir: item.root, RegistryDir: registry},
					Inputs:           []stow.WorkspaceInput{{Source: item.source, Destination: "marker.txt"}},
				})
			}()
		}
		wg.Wait()

		ids := make(map[string]struct{}, width)
		for index, err := range errors {
			if err != nil {
				for _, workspace := range prepared {
					if workspace != nil {
						_ = workspace.Workspace.Destroy(context.Background())
						_ = workspace.Workspace.Close()
					}
				}
				t.Fatalf("round %d prepare %d: %v", round, index, err)
			}
			if _, exists := ids[prepared[index].Workspace.ID()]; exists {
				t.Fatalf("round %d returned duplicate workspace ID %q", round, prepared[index].Workspace.ID())
			}
			ids[prepared[index].Workspace.ID()] = struct{}{}
			got, err := os.ReadFile(filepath.Join(tasks[index].root, "marker.txt"))
			if err != nil || string(got) != tasks[index].marker {
				t.Fatalf("round %d workspace %d marker = %q, %v; want %q", round, index, got, err, tasks[index].marker)
			}
			sourceBytes, err := os.ReadFile(tasks[index].source)
			if err != nil || string(sourceBytes) != tasks[index].marker {
				t.Fatalf("round %d source %d changed: %q, %v", round, index, sourceBytes, err)
			}
		}

		for index, workspace := range prepared {
			if err := workspace.Workspace.Destroy(context.Background()); err != nil {
				t.Fatalf("round %d destroy %d: %v", round, index, err)
			}
			if err := workspace.Workspace.Close(); err != nil {
				t.Fatalf("round %d close %d: %v", round, index, err)
			}
			if _, err := os.Lstat(tasks[index].root); !os.IsNotExist(err) {
				t.Fatalf("round %d workspace %d root survived destroy: %v", round, index, err)
			}
		}
	}
}
