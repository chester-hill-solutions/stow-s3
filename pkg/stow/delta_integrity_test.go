package stow_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestRegressionDeltaWireRoundTrip(t *testing.T) {
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})
	write(t, ws, "edit.txt", "after")
	target := checkpoint(t, ws)
	delta, err := stow.CreateDelta(context.Background(), registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := stow.EncodeDelta(delta)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := stow.DecodeDelta(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stow.ApplyDelta(context.Background(), registry, base, decoded); err != nil {
		t.Fatalf("public encode/decode/apply loses content: %v (decoded content entries=%d)", err, len(decoded.Content))
	}
}

func TestRegressionDeltaCannotModifyBaseThroughTraversal(t *testing.T) {
	registry := t.TempDir()
	_, base := live(t, registry, map[string]string{"edit.txt": "before"})
	path := "../../files/edit.txt"
	payload := []byte("pwned!")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	delta := &stow.DeltaDocument{Version: stow.DeltaVersion, BaseID: base, TargetID: base, Files: 1, Bytes: int64(len(payload)),
		Changes: []stow.DeltaChange{{Kind: "added", Path: path, To: &stow.CheckpointFile{Path: path, Size: int64(len(payload)), Mode: 0o644, SHA256: digest}}},
		Content: map[string][]byte{path: payload}}
	_, applyErr := stow.ApplyDelta(context.Background(), registry, base, delta)
	body, err := os.ReadFile(filepath.Join(registry, "checkpoints", base, "files", "edit.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "before" {
		t.Fatalf("base checkpoint overwritten: got %q; ApplyDelta error=%v", body, applyErr)
	}
	if applyErr == nil {
		t.Fatal("unsafe path accepted")
	}
}

func TestRegressionDeltaVerifiesDigest(t *testing.T) {
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})
	write(t, ws, "edit.txt", "after!")
	target := checkpoint(t, ws)
	err := os.WriteFile(filepath.Join(registry, "checkpoints", target, "files", "edit.txt"), []byte("forged"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	delta, err := stow.CreateDelta(context.Background(), registry, base, target, stow.DeltaOptions{})
	if err == nil {
		t.Fatalf("same-length checkpoint corruption accepted: content=%q", delta.Content["edit.txt"])
	}
}

func TestRegressionDeltaPreservesModeChange(t *testing.T) {
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"run.sh": "echo ok"})
	if err := os.Chmod(filepath.Join(ws.Dir(), "run.sh"), 0755); err != nil {
		t.Fatal(err)
	}
	target := checkpoint(t, ws)
	delta, err := stow.CreateDelta(context.Background(), registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := stow.ApplyDelta(context.Background(), registry, base, delta)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := stow.LoadCheckpoint(registry, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Files[0].Mode != 0755 {
		t.Fatalf("executable mode lost: got %o want 755", manifest.Files[0].Mode)
	}
}
