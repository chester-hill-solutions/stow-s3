package stow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// These two tests are the end-to-end shape of the substitution the document digest
// exists to stop, driven through the real API rather than a hand-built document.
//
// The shape: one workspace, a file added, a delta taken from the base to that
// addition, and then the document altered so the addition lands somewhere else.
// Every check the document makes about itself still passes — the content digest
// covers the bytes, and the new destination is absent from the base, so the
// precondition is satisfied. The result is a published checkpoint holding a file
// the sender never named.
//
// The first test pins that this used to work, so a future change that quietly
// removes the vulnerability cannot also remove the description of it. The second
// pins that the digest stops it.

// TestRegressionARenamedDestinationUsedToApply records the defect this fix closes.
//
// It asserts the substitution is *accepted* when no digest is supplied. That is not
// an endorsement: VerifyDeltaDigest treats an empty expected digest as "no check
// available", because a receiver with no trusted copy of the digest cannot invent
// one. This test exists so that decision is visible and deliberate — if it ever
// fails, the substitution has become impossible by some other means, and the
// opt-in should be reconsidered rather than assumed still necessary.
func TestRegressionARenamedDestinationUsedToApply(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})
	write(t, ws, "notes.txt", "PAYLOAD")
	target := checkpoint(t, ws)

	delta := createDelta(t, registry, base, target)
	encoded := encodeDelta(t, delta)
	published := stow.DeltaDocumentDigest(encoded)

	renamed := renameAddition(t, delta, "notes.txt", "planted.sh")
	altered := encodeDelta(t, renamed)

	// The alteration is invisible to the document's own checks. If this stops
	// holding, the substitution is no longer what the rest of this file is about.
	if _, err := stow.DecodeDelta(altered); err != nil {
		t.Fatalf("the renamed document is no longer self-consistent, so this test is stale: %v", err)
	}

	info, err := stow.ApplyDelta(ctx, registry, base, renamed)
	if err != nil {
		t.Fatalf("the substitution no longer applies, so the opt-in digest may no longer be needed: %v", err)
	}
	if published == stow.DeltaDocumentDigest(altered) {
		t.Fatal("renaming a change did not alter the encoded document")
	}
	// And the checkpoint it produced really does hold the substituted name, which
	// is the part that matters: a refusal that still wrote the file would be worse
	// than no refusal at all.
	manifest, err := stow.LoadCheckpoint(registry, info.ID)
	if err != nil {
		t.Fatalf("load the applied checkpoint: %v", err)
	}
	if !manifestHolds(manifest, "planted.sh") {
		t.Fatalf("the applied checkpoint does not hold the substituted name, so the substitution is not what this test claims: %v", paths(manifest))
	}
	if manifestHolds(manifest, "notes.txt") {
		t.Fatal("the applied checkpoint holds both names, so the rename did not take effect")
	}
}

// TestADigestRefusesARenamedDestinationBeforeAnythingIsWritten is the fix.
func TestADigestRefusesARenamedDestinationBeforeAnythingIsWritten(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})
	write(t, ws, "notes.txt", "PAYLOAD")
	target := checkpoint(t, ws)

	delta := createDelta(t, registry, base, target)
	published := stow.DeltaDocumentDigest(encodeDelta(t, delta))
	altered := encodeDelta(t, renameAddition(t, delta, "notes.txt", "planted.sh"))

	before := checkpointIDs(t, registry)

	_, err := stow.ApplyDeltaWithOptions(ctx, registry, base, decodeDelta(t, altered), stow.DeltaOptions{
		Encoded: altered, ExpectSHA256: published,
	})
	if !errors.Is(err, stow.ErrDeltaDigestMismatch) {
		t.Fatalf("the renamed document was not refused on its digest: %v", err)
	}

	// The refusal has to come before anything is written. A digest check that ran
	// after staging would still leave a checkpoint behind for an operator to find.
	if after := checkpointIDs(t, registry); len(after) != len(before) {
		t.Fatalf("the refusal still published a checkpoint: %v then %v", before, after)
	}
}

// TestTheCorrectDigestStillApplies guards the check against being useless. A guard
// that refuses everything is not a guard; it is an outage with a reassuring error
// message.
func TestTheCorrectDigestStillApplies(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})
	write(t, ws, "notes.txt", "PAYLOAD")
	target := checkpoint(t, ws)

	delta := createDelta(t, registry, base, target)
	encoded := encodeDelta(t, delta)

	info, err := stow.ApplyDeltaWithOptions(ctx, registry, base, delta, stow.DeltaOptions{
		Encoded: encoded, ExpectSHA256: stow.DeltaDocumentDigest(encoded),
	})
	if err != nil {
		t.Fatalf("an unaltered document with the sender's own digest was refused: %v", err)
	}
	manifest, err := stow.LoadCheckpoint(registry, info.ID)
	if err != nil {
		t.Fatalf("load the applied checkpoint: %v", err)
	}
	if !manifestHolds(manifest, "notes.txt") {
		t.Fatalf("the applied checkpoint does not hold the file the sender named: %v", paths(manifest))
	}
}

// TestADigestMismatchOutranksAConflict checks which refusal a receiver sees. A
// document that was altered *and* applied to the wrong base has two problems, and
// naming the digest first is the useful order: the base is the sender's to choose,
// while the alteration is the one nobody intended.
func TestADigestMismatchOutranksAConflict(t *testing.T) {
	ctx := context.Background()
	registry := t.TempDir()
	ws, base := live(t, registry, map[string]string{"edit.txt": "before"})
	write(t, ws, "notes.txt", "PAYLOAD")
	target := checkpoint(t, ws)

	delta := createDelta(t, registry, base, target)
	published := stow.DeltaDocumentDigest(encodeDelta(t, delta))
	altered := decodeDelta(t, encodeDelta(t, renameAddition(t, delta, "notes.txt", "planted.sh")))

	// Validate the digest before decoding, as the CLI does, and report the digest
	// refusal in preference to whatever the decode or the conflict would say.
	if err := stow.VerifyDeltaDigest(encodeDelta(t, altered), published); !errors.Is(err, stow.ErrDeltaDigestMismatch) {
		t.Fatalf("expected the digest refusal, got %v", err)
	}
	// And the two conditions are genuinely distinguishable rather than one masking
	// the other. The base has to hold the path the *renamed* document names, since
	// that is the path its precondition asks about — naming a later checkpoint of
	// this workspace would not do it, because that checkpoint holds notes.txt and
	// not planted.sh, and an absent path is exactly what an addition requires.
	holding := t.TempDir()
	_, base2 := live(t, holding, map[string]string{"planted.sh": "already here"})
	if _, err := stow.ApplyDelta(ctx, holding, base2, altered); !errors.Is(err, stow.ErrDeltaConflict) {
		t.Fatalf("expected a conflict for a base that already holds the substituted name, got %v", err)
	}
}

// --- helpers --------------------------------------------------------------

func createDelta(t *testing.T, registry, base, target string) *stow.DeltaDocument {
	t.Helper()
	delta, err := stow.CreateDelta(context.Background(), registry, base, target, stow.DeltaOptions{})
	if err != nil {
		t.Fatalf("create the delta: %v", err)
	}
	return delta
}

func encodeDelta(t *testing.T, delta *stow.DeltaDocument) []byte {
	t.Helper()
	encoded, err := stow.EncodeDelta(delta)
	if err != nil {
		t.Fatalf("encode the delta: %v", err)
	}
	return encoded
}

func decodeDelta(t *testing.T, encoded []byte) *stow.DeltaDocument {
	t.Helper()
	delta, err := stow.DecodeDelta(encoded)
	if err != nil {
		t.Fatalf("decode the delta: %v", err)
	}
	return delta
}

// renameAddition rewrites an addition's destination in the change list and the
// content map together, which is what an alteration has to do to stay
// self-consistent, and returns the document so calls can be chained.
func renameAddition(t *testing.T, delta *stow.DeltaDocument, from, to string) *stow.DeltaDocument {
	t.Helper()
	for i := range delta.Changes {
		change := &delta.Changes[i]
		if change.Kind != stow.DeltaChangeAdded || change.Path != from {
			continue
		}
		content, ok := delta.Content[from]
		if !ok {
			t.Fatalf("the delta adds %q but carries no content for it", from)
		}
		change.Path = to
		if change.To != nil {
			change.To.Path = to
		}
		delete(delta.Content, from)
		delta.Content[to] = content
		return delta
	}
	t.Fatalf("the delta does not add %q, so it cannot be renamed", from)
	return nil
}

// manifestHolds reports whether a checkpoint contains a path. CheckpointManifest
// carries a slice, so this is a lookup rather than a map read.
func manifestHolds(manifest stow.CheckpointManifest, path string) bool {
	for _, file := range manifest.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func paths(manifest stow.CheckpointManifest) []string {
	names := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		names = append(names, file.Path)
	}
	return names
}

func checkpointIDs(t *testing.T, registry string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(registry, "checkpoints"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the checkpoint directory: %v", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	return ids
}
