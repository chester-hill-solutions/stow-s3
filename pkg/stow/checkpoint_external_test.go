package stow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// A checkpoint has to be possible while an agent is still working. That was
// impossible, because the only checkpoint entry point required the handle, and the
// handle could only be obtained by claiming the session a live process already
// held — so the answer to "what has it done so far?" was "stop it first".
func TestACheckpointOfALiveWorkspaceDoesNotNeedTheSession(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	live, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer live.Close()
	if err := os.WriteFile(filepath.Join(live.Dir(), "TASK.md"), []byte("half done"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The live handle still holds the session, which is the whole obstacle.
	if _, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: registry}, live.ID()); err == nil {
		t.Fatal("a second session claimed a workspace that is in use; the exclusivity this relies on is gone")
	}

	checkpoint, err := stow.CheckpointOf(context.Background(), registry, live.ID(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CheckpointOf on a live workspace: %v", err)
	}
	assertCheckpointBody(t, registry, checkpoint.ID, "TASK.md", "half done")

	// And the live handle is untouched: it still holds the session, and it can
	// still be used afterwards.
	if _, err := live.PutObject(context.Background(), live.Bucket(), "later.txt", []byte("later"), stow.PutOptions{}); err != nil {
		t.Fatalf("the live session was disturbed by an external checkpoint: %v", err)
	}
}

// The two entry points are the same capture, so they must produce the same thing
// for the same tree. A difference between them would mean the external path had
// quietly lost an exclusion or a limit.
func TestAnExternalCheckpointAgreesWithTheHandleOnWhatIsCaptured(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer ws.Close()
	write(t, ws, "kept.txt", "kept")
	if err := os.WriteFile(filepath.Join(ws.Dir(), ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws.Dir(), ".stow"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), ".stow", "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	external, err := stow.CheckpointOf(context.Background(), registry, ws.ID(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CheckpointOf: %v", err)
	}
	internal, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	for _, pair := range []struct{ name, external, internal string }{
		{"files", describeFiles(t, registry, external.ID), describeFiles(t, registry, internal.ID)},
		{"excluded", strings.Join(external.Excluded, ","), strings.Join(internal.Excluded, ",")},
	} {
		if pair.external != pair.internal {
			t.Errorf("%s differ: external %q, handle %q", pair.name, pair.external, pair.internal)
		}
	}
	if got := strings.Join(external.Excluded, ","); got != ".env" {
		t.Errorf("excluded = %q, want the sensitive .env reported rather than copied", got)
	}
}

func describeFiles(t *testing.T, registryDir, checkpointID string) string {
	t.Helper()
	manifest, err := stow.LoadCheckpoint(registryDir, checkpointID)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	names := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		names = append(names, file.Path)
	}
	return strings.Join(names, ",")
}

// Two captures in flight would each read the same retention count and both pass.
// The capture lock is what makes the cap exact, and the loser of the race has to be
// refused by the cap rather than quietly exceeding it.
func TestConcurrentCapturesOfOneWorkspaceRespectTheRetentionCap(t *testing.T) {
	if !workspace.LockSupported() {
		t.Skip("this host cannot serialize captures; the cap is approximate here by design")
	}
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{
		Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registry, MaxCheckpoints: 1,
	})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer ws.Close()
	write(t, ws, "a.txt", "one")

	// The handle is left open on purpose: a live session must not be what makes
	// the cap hold, since the whole point is that captures happen beside one.
	start := make(chan struct{})
	results := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, err := stow.CheckpointOf(context.Background(), registry, ws.ID(), stow.CheckpointOptions{})
			results[index] = err
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded, refused := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, workspace.ErrCaptureInProgress), strings.Contains(err.Error(), "count limit reached"):
			// Both refusals are correct and neither is a cap overshoot: one capture
			// lost the lock, and the other lost the cap. Which of the two happened
			// is a race, and neither outcome publishes a second checkpoint.
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 || refused != 1 {
		t.Fatalf("succeeded=%d refused=%d, want exactly one of each: %v", succeeded, refused, results)
	}
	// Exactly one checkpoint exists, so the cap held rather than being overshot.
	count := 0
	entries, err := os.ReadDir(filepath.Join(registry, "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".checkpoint-") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d checkpoints published, want 1", count)
	}
}

// A capture must not be able to publish two checkpoints past a cap even when the
// captures come from different processes, which is the same property seen through
// the handle rather than the registry.
func TestTheCaptureLockIsPerWorkspaceAndNotGlobal(t *testing.T) {
	if !workspace.LockSupported() {
		t.Skip("this host has no advisory capture lock")
	}
	registry := filepath.Join(t.TempDir(), "registry")
	first, err := workspace.AcquireCapture(registry, "ws_one")
	if err != nil {
		t.Fatalf("AcquireCapture: %v", err)
	}
	defer first.Release()
	if !first.Held() {
		t.Fatal("the capture lock was not taken on a host that supports it")
	}
	// A different workspace is not blocked, or one busy workspace would stall
	// every other capture on the machine.
	other, err := workspace.AcquireCapture(registry, "ws_two")
	if err != nil {
		t.Fatalf("AcquireCapture for a second workspace: %v", err)
	}
	if !other.Held() {
		t.Error("capturing a different workspace was blocked by an unrelated capture")
	}
	other.Release()
	// A second claim on the same workspace is refused, and named, rather than
	// handed out. Two claims would both reach the retention check and both pass.
	if _, err := workspace.AcquireCapture(registry, "ws_one"); !errors.Is(err, workspace.ErrCaptureInProgress) {
		t.Errorf("second claim error = %v, want ErrCaptureInProgress", err)
	}
}

// The capture lock is a separate file from the session lock. If they were the same
// file, capturing would still be blocked by a live session, which is the defect
// this whole change exists to remove.
func TestTheCaptureLockDoesNotDisturbTheSessionLock(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "registry")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "task"), RegistryDir: registry})
	if err != nil {
		t.Fatalf("OpenWorkspace: %v", err)
	}
	defer ws.Close()
	lock, err := workspace.AcquireCapture(registry, ws.ID())
	if err != nil {
		t.Fatalf("AcquireCapture: %v", err)
	}
	defer lock.Release()
	// The session is still held: taking a capture lock released nothing.
	if _, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: registry}, ws.ID()); err == nil {
		t.Fatal("taking a capture lock released the session lock")
	}
	// And liveness still reads as live, so a collector would still decline.
	live, err := workspace.ProbeLiveness(ws.Dir())
	if err != nil {
		t.Fatalf("ProbeLiveness: %v", err)
	}
	if !live {
		t.Error("a workspace with a live session read as not live while a capture lock was held")
	}
}
