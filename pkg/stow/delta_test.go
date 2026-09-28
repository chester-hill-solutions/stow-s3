package stow_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// A delta carries the difference between two known points and can bring a third
// to the second, so an agent and a device exchange what changed rather than a
// whole working set.
//
// The conflict rule is the point. A delta says "this file was A, it is now B", so
// applying it to a target whose copy of that file is neither A nor B is applying
// one writer's intent to a state that does not exist. That is the same problem
// run-through propagation had, and it must not get a second answer: the same
// precondition, the same refusal rather than a merge, and the same stable error.

// live opens one workspace in the given registry with the given files, and returns
// it alongside its first checkpoint. Everything in a test happens to that one
// workspace, because a delta is computed between two points in the same history —
// a base and a target separated by edits — not between two unrelated trees.
func live(t *testing.T, registryDir string, files map[string]string) (*stow.Workspace, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "task")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, Bucket: "task", RegistryDir: registryDir})
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	for name, body := range files {
		write(t, ws, name, body)
	}
	base, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("base checkpoint: %v", err)
	}
	return ws, base.ID
}

func write(t *testing.T, ws *stow.Workspace, name, body string) {
	t.Helper()
	destination := filepath.Join(ws.Dir(), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(destination, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func checkpoint(t *testing.T, ws *stow.Workspace) string {
	t.Helper()
	info, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	return info.ID
}

func TestADeltaBringsAThirdPointToTheTarget(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{
		"keep.txt":      "unchanged",
		"edit.txt":      "before",
		"untouched.txt": "also unchanged",
	})

	write(t, ws, "edit.txt", "after")
	write(t, ws, "added.txt", "new file")
	if err := os.Remove(filepath.Join(ws.Dir(), "keep.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	target := checkpoint(t, ws)

	delta, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatalf("create delta: %v", err)
	}
	if delta.Version != stow.DeltaVersion {
		t.Fatalf("delta version = %d, want %d", delta.Version, stow.DeltaVersion)
	}
	if delta.BaseID != base || delta.TargetID != target {
		t.Fatalf("delta names base %q target %q, want %q and %q", delta.BaseID, delta.TargetID, base, target)
	}

	kinds := map[string]string{}
	for _, change := range delta.Changes {
		kinds[change.Path] = change.Kind
	}
	for path, kind := range map[string]string{
		"edit.txt": "changed", "added.txt": "added", "keep.txt": "deleted",
	} {
		if kinds[path] != kind {
			t.Errorf("delta says %s is %q, want %q", path, kinds[path], kind)
		}
	}
	// An unchanged file must not appear at all. That is the entire point of a
	// delta: carrying it would make the document the size of the tree.
	if _, present := kinds["untouched.txt"]; present {
		t.Error("delta carried a file that did not change")
	}
	if _, present := delta.Content["untouched.txt"]; present {
		t.Error("delta carried the content of a file that did not change")
	}

	// Applying to the base brings it to the target's state.
	applied, err := stow.ApplyDelta(ctx, registry, base, delta)
	if err != nil {
		t.Fatalf("apply delta: %v", err)
	}
	if applied.ID == base || applied.ID == target {
		t.Fatalf("applied checkpoint %q must be a new point, not one of the ends", applied.ID)
	}
	assertCheckpointHas(t, registry, applied.ID, map[string]bool{
		"edit.txt": true, "added.txt": true, "untouched.txt": true, "keep.txt": false,
	})
	assertCheckpointBody(t, registry, applied.ID, "edit.txt", "after")
	assertCheckpointBody(t, registry, applied.ID, "added.txt", "new file")
	assertCheckpointBody(t, registry, applied.ID, "untouched.txt", "also unchanged")

	// The base must be untouched. A delta that rewrites the point it was measured
	// from cannot be applied twice, and a caller retrying would get a different
	// answer.
	assertCheckpointBody(t, registry, base, "edit.txt", "before")
	assertCheckpointHas(t, registry, base, map[string]bool{"keep.txt": true, "added.txt": false})
}

// A delta that has been through a file is the only kind two machines ever
// exchange, and the encoding is what carries it. It used not to: the content map
// was excluded from the JSON, so a decoded delta described every change and held
// none of the bytes, and applying it failed on the first added or changed path.
func TestADeltaSurvivesEncodingAndDecoding(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})

	write(t, ws, "edit.txt", "after")
	write(t, ws, "added.txt", "new file")
	target := checkpoint(t, ws)

	delta, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatalf("create delta: %v", err)
	}
	encoded, err := stow.EncodeDelta(delta)
	if err != nil {
		t.Fatalf("encode delta: %v", err)
	}
	decoded, err := stow.DecodeDelta(encoded)
	if err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if decoded.BaseID != delta.BaseID || decoded.TargetID != delta.TargetID {
		t.Fatalf("decoded delta names %q -> %q, want %q -> %q",
			decoded.BaseID, decoded.TargetID, delta.BaseID, delta.TargetID)
	}
	for path, want := range delta.Content {
		if string(decoded.Content[path]) != string(want) {
			t.Errorf("decoded delta content for %q = %q, want %q", path, decoded.Content[path], want)
		}
	}

	// The transported document is what gets applied, to the base, on the other side.
	applied, err := stow.ApplyDelta(ctx, registry, base, decoded)
	if err != nil {
		t.Fatalf("apply decoded delta: %v", err)
	}
	assertCheckpointBody(t, registry, applied.ID, "edit.txt", "after")
	assertCheckpointBody(t, registry, applied.ID, "added.txt", "new file")
}

// Content arrives from somewhere else, so it is verified against the digest the
// document itself claims before it is written. Create verifies what it reads out
// of a checkpoint; a decoded document can carry anything, and a delta that applies
// bytes nobody hashed is a delta that can change a working set silently.
func TestApplyingADeltaWithContentThatFailsItsDigestIsRefused(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})

	write(t, ws, "edit.txt", "after")
	target := checkpoint(t, ws)
	delta, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatalf("create delta: %v", err)
	}
	encoded, err := stow.EncodeDelta(delta)
	if err != nil {
		t.Fatalf("encode delta: %v", err)
	}
	decoded, err := stow.DecodeDelta(encoded)
	if err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	decoded.Content["edit.txt"] = []byte("not what the digest says")

	if _, err := stow.ApplyDelta(ctx, registry, base, decoded); err == nil {
		t.Fatal("a delta whose content failed its digest was applied")
	}
	assertCheckpointBody(t, registry, base, "edit.txt", "before")
}

// A document is bounded on the way in as well as on the way out, because a
// decoded document can arrive from anywhere and the decoder is the only place
// that sees it before the bytes are in memory.
func TestDecodeDeltaRefusesAnOversizedDocument(t *testing.T) {
	oversized := fmt.Sprintf(`{"version":%d,"base_id":"cp_1","target_id":"cp_2","bytes":%d,"content":{"a.txt":%q}}`,
		stow.DeltaVersion, stow.MaxDeltaBytes+1, base64.StdEncoding.EncodeToString([]byte("x")))
	if _, err := stow.DecodeDelta([]byte(oversized)); !errors.Is(err, stow.ErrDeltaTooLarge) {
		t.Fatalf("error = %v, want ErrDeltaTooLarge", err)
	}
}

// Applying a delta to a diverged target is a conflict, and the refusal must leave
// the target exactly as it was. A delta applied part way leaves a target matching
// neither end, which is worse than a refusal because nothing reports it.
func TestApplyingADeltaToADivergedTargetIsAConflict(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})

	write(t, ws, "edit.txt", "after")
	target := checkpoint(t, ws)
	delta, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatalf("create delta: %v", err)
	}

	// A third point that is neither the base nor the target.
	write(t, ws, "edit.txt", "someone else's work")
	third := checkpoint(t, ws)

	if _, err := stow.ApplyDelta(ctx, registry, third, delta); !errors.Is(err, stow.ErrDeltaConflict) {
		t.Fatalf("error = %v, want ErrDeltaConflict", err)
	}
	assertCheckpointBody(t, registry, third, "edit.txt", "someone else's work")
}

// An "added" entry asserts absence, the same shape as an If-None-Match of "*" in
// run-through: the delta claims the path is new, so a target that already has it
// holds someone else's file and taking it would lose their work.
func TestADeltaRefusesWhenAnAddedPathAlreadyExists(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"keep.txt": "x"})
	write(t, ws, "claimed.txt", "mine")
	target := checkpoint(t, ws)
	delta, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatalf("create delta: %v", err)
	}

	write(t, ws, "claimed.txt", "already theirs")
	third := checkpoint(t, ws)

	if _, err := stow.ApplyDelta(ctx, registry, third, delta); !errors.Is(err, stow.ErrDeltaConflict) {
		t.Fatalf("error = %v, want ErrDeltaConflict when an added path already exists", err)
	}
	assertCheckpointBody(t, registry, third, "claimed.txt", "already theirs")
}

// A delta refused by its caps is better than a delta the receiver cannot accept,
// and the caps are the archive path's so a delta and an archive of the same work
// are accepted or refused together.
func TestADeltaIsBoundedByTheSameCapsAsAnArchive(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"a.txt": "one"})
	write(t, ws, "b.txt", "two")
	write(t, ws, "c.txt", "three")
	target := checkpoint(t, ws)

	// Two changed files totalling eight bytes, so a cap below that must refuse.
	// A cap equal to the size does not, which is the boundary and worth being
	// explicit about rather than leaving to a rounding accident.
	if _, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{MaxBytes: 7}); err == nil {
		t.Fatal("a delta over its byte cap was produced")
	}
	if _, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{MaxBytes: 8}); err != nil {
		t.Fatalf("a delta exactly at its byte cap was refused: %v", err)
	}
	if _, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{MaxFiles: 1}); err == nil {
		t.Fatal("a delta over its file cap was produced")
	}
	// Zero means the default, not unlimited, matching the archive options.
	delta, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{MaxBytes: 0, MaxFiles: 0})
	if err != nil {
		t.Fatalf("zero caps should mean the defaults: %v", err)
	}
	if len(delta.Changes) != 2 || delta.Bytes <= 0 {
		t.Fatalf("delta = %d bytes / %d changes, want a real delta", delta.Bytes, len(delta.Changes))
	}
}

// A version this build does not speak is a deployment problem, not a data one, and
// it is reported as such rather than as a conflict — retrying will not help.
func TestADeltaRefusesAnUnknownVersion(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"a.txt": "one"})
	write(t, ws, "a.txt", "two")
	target := checkpoint(t, ws)
	delta, err := stow.CreateDelta(ctx, registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatalf("create delta: %v", err)
	}

	delta.Version = stow.DeltaVersion + 1
	if _, err := stow.ApplyDelta(ctx, registry, base, delta); !errors.Is(err, stow.ErrDeltaVersionUnsupported) {
		t.Fatalf("error = %v, want ErrDeltaVersionUnsupported", err)
	}
	if _, err := stow.EncodeDelta(delta); !errors.Is(err, stow.ErrDeltaVersionUnsupported) {
		t.Fatalf("encode error = %v, want ErrDeltaVersionUnsupported", err)
	}
}

// A delta that crosses two workspaces is refused. The change list is meaningless
// between different histories, and accepting it would apply one workspace's
// intent to another's tree.
func TestADeltaRefusesToCrossWorkspaces(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	_, base := live(t, registry, map[string]string{"a.txt": "one"})

	other := filepath.Join(t.TempDir(), "other")
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: other, Bucket: "task", RegistryDir: registry})
	if err != nil {
		t.Fatalf("open second workspace: %v", err)
	}
	defer ws.Close()
	write(t, ws, "b.txt", "two")
	otherTarget := checkpoint(t, ws)

	if _, err := stow.CreateDelta(ctx, registry, base, otherTarget, stow.DeltaOptions{}); err == nil {
		t.Fatal("a delta between two different workspaces was produced")
	}
}

func assertCheckpointHas(t *testing.T, registryDir, checkpointID string, want map[string]bool) {
	t.Helper()
	manifest, err := stow.LoadCheckpoint(registryDir, checkpointID)
	if err != nil {
		t.Fatalf("load checkpoint %s: %v", checkpointID, err)
	}
	present := map[string]bool{}
	for _, file := range manifest.Files {
		present[file.Path] = true
	}
	for path, expected := range want {
		if present[path] != expected {
			t.Errorf("checkpoint has %q = %v, want %v", path, present[path], expected)
		}
	}
}

func assertCheckpointBody(t *testing.T, registryDir, checkpointID, path, want string) {
	t.Helper()
	manifest, err := stow.LoadCheckpoint(registryDir, checkpointID)
	if err != nil {
		t.Fatalf("load checkpoint %s: %v", checkpointID, err)
	}
	var found bool
	for _, file := range manifest.Files {
		if file.Path == path {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("checkpoint %s has no %q", checkpointID, path)
	}
	body, err := stow.ReadCheckpointFile(registryDir, checkpointID, path)
	if err != nil {
		t.Fatalf("read %s from %s: %v", path, checkpointID, err)
	}
	if string(body) != want {
		t.Fatalf("%s in %s = %q, want %q", path, checkpointID, body, want)
	}
}
