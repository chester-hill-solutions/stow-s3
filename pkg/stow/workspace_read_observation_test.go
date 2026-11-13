package stow_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceDirectoryObservationPagesStableMetadataAndCurrentFiles(t *testing.T) {
	ws, registryDir := createWorkspaceObservationFixture(t, 101)
	reader := openWorkspaceReaderForTest(t, registryDir, ws.ID())
	defer reader.Close()
	assertLegacyDirectoryLimit(t, reader)
	observation := observeWorkspaceDirectoryForTest(t, reader, ".")
	assertObservationSummary(t, observation, 101)
	assertObservationPagesAndCurrentFiles(t, ws, reader, observation)
	if err := verifyObservationMetadataCharge(observation); err != nil {
		t.Fatal(err)
	}
}

func createWorkspaceObservationFixture(t *testing.T, count int) (*stow.Workspace, string) {
	t.Helper()
	ws, registryDir := workspaceReaderFixture(t)
	for index := range count {
		name := fmt.Sprintf("file-%03d.txt", index)
		if err := os.WriteFile(filepath.Join(ws.Dir(), name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return ws, registryDir
}

func openWorkspaceReaderForTest(t *testing.T, registryDir, id string) *stow.WorkspaceReader {
	t.Helper()
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: id})
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func assertLegacyDirectoryLimit(t *testing.T, reader *stow.WorkspaceReader) {
	t.Helper()
	if _, err := reader.ReadDirectory(context.Background(), "."); !errors.Is(err, stow.ErrWorkspaceReadTooLarge) {
		t.Fatalf("legacy ReadDirectory did not preserve <=100 behavior: %v", err)
	}
}

func observeWorkspaceDirectoryForTest(t *testing.T, reader *stow.WorkspaceReader, path string) *stow.WorkspaceDirectoryObservation {
	t.Helper()
	observation, err := reader.ObserveDirectory(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func assertObservationSummary(t *testing.T, observation *stow.WorkspaceDirectoryObservation, count int) {
	t.Helper()
	if observation.Len() != count || observation.Path() != "." || observation.MetadataBytes() <= 256 || observation.ObservedAt().IsZero() {
		t.Fatalf("observation metadata: len=%d path=%q bytes=%d at=%v", observation.Len(), observation.Path(), observation.MetadataBytes(), observation.ObservedAt())
	}
}

func assertObservationPagesAndCurrentFiles(t *testing.T, ws *stow.Workspace, reader *stow.WorkspaceReader, observation *stow.WorkspaceDirectoryObservation) {
	t.Helper()
	first, err := observation.Page(context.Background(), 0, 100)
	if err != nil || len(first) != 100 {
		t.Fatalf("first page len=%d err=%v", len(first), err)
	}
	wantFirst := first[0]
	first[0].Path = "caller-mutated"
	second, err := observation.Page(context.Background(), 100, 100)
	if err != nil || len(second) != 1 || second[0].Name != "file-100.txt" {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	again, err := observation.Page(context.Background(), 0, 1)
	if err != nil || len(again) != 1 || again[0].Path != wantFirst.Path {
		t.Fatalf("page metadata was caller-mutable: %+v err=%v", again, err)
	}
	updated := []byte("current bytes after observation")
	if err := os.WriteFile(filepath.Join(ws.Dir(), wantFirst.Path), updated, 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := reader.ReadFile(context.Background(), wantFirst.Path)
	if err != nil || string(current.Data) != string(updated) {
		t.Fatalf("subsequent file read = %q, %v", current.Data, err)
	}
	stable, err := observation.Page(context.Background(), 0, 1)
	if err != nil || stable[0].Size != 1 {
		t.Fatalf("observation changed after file edit: %+v, %v", stable, err)
	}
}

func verifyObservationMetadataCharge(observation *stow.WorkspaceDirectoryObservation) error {
	want := int64(256)
	for offset := 0; offset < observation.Len(); offset += 100 {
		page, err := observation.Page(context.Background(), offset, 100)
		if err != nil {
			return err
		}
		for _, entry := range page {
			want += int64(256 + len(entry.Name) + len(entry.Path))
		}
	}
	if observation.MetadataBytes() != want {
		return fmt.Errorf("metadata charge=%d, want %d", observation.MetadataBytes(), want)
	}
	return nil
}

func TestWorkspaceDirectoryObservationChecksPageScopeIdentityAndClose(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	directory := filepath.Join(ws.Dir(), "allowed")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "visible.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: []string{"allowed"}})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := reader.ObserveDirectory(context.Background(), "allowed")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][2]int{{-1, 1}, {2, 1}, {0, 0}, {0, 101}} {
		if _, err := observation.Page(context.Background(), args[0], args[1]); !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
			t.Errorf("Page(%d,%d) error = %v", args[0], args[1], err)
		}
	}
	if err := os.Rename(directory, directory+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := observation.Page(context.Background(), 0, 1); !errors.Is(err, stow.ErrWorkspaceReadChanged) {
		t.Fatalf("page after parent replacement = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := observation.Page(context.Background(), 0, 1); !errors.Is(err, stow.ErrWorkspaceReaderClosed) {
		t.Fatalf("page after reader close = %v", err)
	}
}

func TestWorkspaceDirectoryObservationRejectsRootReplacement(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	if err := os.Mkdir(filepath.Join(ws.Dir(), "allowed"), 0o700); err != nil {
		t.Fatal(err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID(), ReadRoots: []string{"allowed"}})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	observation, err := reader.ObserveDirectory(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	root := ws.Dir()
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := observation.Page(context.Background(), 0, 1); !errors.Is(err, stow.ErrWorkspaceReadChanged) {
		t.Fatalf("page after root replacement = %v", err)
	}
}

func TestWorkspaceDirectoryObservationCancellationReturnsNoObservation(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	for index := range 20 {
		if err := os.WriteFile(filepath.Join(ws.Dir(), fmt.Sprintf("f-%02d", index)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if observation, err := reader.ObserveDirectory(ctx, "."); observation != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observation=%v err=%v", observation, err)
	}
}

type cancellationAfterChecks struct {
	context.Context
	remaining int
}

type mutateOnContextCheck struct {
	context.Context
	checks  int
	trigger int
	mutate  func()
}

func (c *mutateOnContextCheck) Err() error {
	c.checks++
	if c.checks == c.trigger {
		c.mutate()
	}
	return nil
}

func (c *cancellationAfterChecks) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestWorkspaceDirectoryObservationCancelsDuringBatch(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	for index := range 20 {
		if err := os.WriteFile(filepath.Join(ws.Dir(), fmt.Sprintf("f-%02d", index)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ctx := &cancellationAfterChecks{Context: context.Background(), remaining: 12}
	if observation, err := reader.ObserveDirectory(ctx, "."); observation != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-batch cancellation observation=%v err=%v", observation, err)
	}
}

func TestWorkspaceDirectoryObservationRefusesObservableParentChange(t *testing.T) {
	ws, registryDir := createWorkspaceObservationFixture(t, 20)
	reader := openWorkspaceReaderForTest(t, registryDir, ws.ID())
	defer reader.Close()
	directory := ws.Dir()
	ctx := &mutateOnContextCheck{Context: context.Background(), trigger: 5}
	ctx.mutate = func() {
		if err := os.WriteFile(filepath.Join(directory, "late-entry"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		future := info.ModTime().Add(2 * time.Second)
		if err := os.Chtimes(directory, future, future); err != nil {
			t.Fatal(err)
		}
	}
	if observation, err := reader.ObserveDirectory(ctx, "."); observation != nil || !errors.Is(err, stow.ErrWorkspaceReadChanged) {
		t.Fatalf("changed-parent observation=%v err=%v", observation, err)
	}
}

func TestWorkspaceDirectoryObservationEmptyDirectoryChargeAndEndPage(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	if err := os.Mkdir(filepath.Join(ws.Dir(), "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	observation, err := reader.ObserveDirectory(context.Background(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	if observation.Len() != 0 || observation.MetadataBytes() != 256 || time.Since(observation.ObservedAt()) < 0 {
		t.Fatalf("empty observation = len:%d bytes:%d at:%v", observation.Len(), observation.MetadataBytes(), observation.ObservedAt())
	}
	page, err := observation.Page(context.Background(), 0, 100)
	if err != nil || len(page) != 0 {
		t.Fatalf("empty page = %+v, %v", page, err)
	}
}

func TestWorkspaceDirectoryObservationBoundsMetadataCharge(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	root := filepath.Join(ws.Dir(), "wide")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("n", 230)
	for index := range 5_800 {
		name := prefix + fmt.Sprintf("%05d", index)
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if observation, err := reader.ObserveDirectory(context.Background(), "wide"); observation != nil || !errors.Is(err, stow.ErrWorkspaceReadTooLarge) {
		t.Fatalf("oversize metadata observation=%v err=%v", observation, err)
	}
}

func TestWorkspaceDirectoryObservationScanCapIncludesHiddenEntries(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	root := filepath.Join(ws.Dir(), "hidden")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range 10_001 {
		name := fmt.Sprintf(".env.%05d", index)
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if observation, err := reader.ObserveDirectory(context.Background(), "hidden"); observation != nil || !errors.Is(err, stow.ErrWorkspaceReadTooLarge) {
		t.Fatalf("hidden-entry scan observation=%v err=%v", observation, err)
	}
}
