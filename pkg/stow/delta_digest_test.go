package stow_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// A delta document's own digests cover its content and nothing else. Each changed
// path carries the sha256 of its bytes, and a receiver checks those, so the bytes
// cannot be swapped. Nothing in the document binds the change list itself — the
// The consequence is not subtle: take a document that adds "notes.txt", rename the
// addition to "planted.sh" in the change and in the content map together, and the
// document is entirely self-consistent. The content digest still matches the bytes it
// carries, and the precondition passes because "planted.sh" is absent from the target.
// Apply writes the payload under a destination the sender never chose, and reports
// success.
//
// The fix is the check the handoff archive path already had. `delta` reports the
// document's digest; the receiver hands it back and apply refuses a document that does
// not hash to it. These tests hold that line from both directions.

func TestADeltaWithARenamedDestinationIsRefusedOnTheDocumentDigest(t *testing.T) {
	document := readFixtureDelta(t)
	original := encodeFixtureDelta(t, document)

	// The substitution: same bytes, same valid content digest, different name.
	renamed := renameFixtureAddition(t, document, "notes.txt", "planted.sh")
	altered := encodeFixtureDelta(t, document)

	// Precondition: the rename is invisible to every check the document makes
	// about itself. If this stops holding, the substitution is no longer the thing
	// this test is about and the fix below is testing the wrong thing.
	if renamed == nil {
		t.Fatal("the fixture does not add notes.txt, so the substitution cannot be built")
	}
	if string(original) == string(altered) {
		t.Fatal("renaming a change did not alter the encoded document")
	}

	err := stow.VerifyDeltaDigest(altered, stow.DeltaDocumentDigest(original))
	if err == nil {
		t.Fatal("a renamed destination was accepted against the sender's digest")
	}
	if !isDigestMismatch(err) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

func TestAnUnalteredDeltaPassesTheDocumentDigest(t *testing.T) {
	// The check has to be usable, not merely strict. A receiver that computes the
	// digest the same way the sender did must get a document it can apply.
	document := readFixtureDelta(t)
	encoded := encodeFixtureDelta(t, document)
	if err := stow.VerifyDeltaDigest(encoded, stow.DeltaDocumentDigest(encoded)); err != nil {
		t.Fatalf("an unaltered document was refused: %v", err)
	}
}

func TestTheDocumentDigestIsCaseInsensitiveAndTrimmed(t *testing.T) {
	// A digest travels through whatever copied it: a chat message, a CI variable, a
	// shell variable. Upper case and a trailing newline are not a different digest,
	// and refusing them would train people to strip the check rather than use it.
	document := readFixtureDelta(t)
	encoded := encodeFixtureDelta(t, document)
	digest := stow.DeltaDocumentDigest(encoded)

	for _, form := range []string{digest, upper(digest), digest + "\n", " " + digest + " "} {
		if err := stow.VerifyDeltaDigest(encoded, form); err != nil {
			t.Fatalf("digest form %q was refused: %v", form, err)
		}
	}
}

func TestAnEmptyExpectedDigestIsNotARejection(t *testing.T) {
	// A receiver with no trusted copy of the digest cannot invent one. Failing
	// closed here would make apply unusable rather than safe, and the honest
	// position is that the check is opt-in and the documentation says so.
	document := readFixtureDelta(t)
	encoded := encodeFixtureDelta(t, document)
	if err := stow.VerifyDeltaDigest(encoded, ""); err != nil {
		t.Fatalf("an empty expected digest was treated as a mismatch: %v", err)
	}
	if err := stow.VerifyDeltaDigest(encoded, "   "); err != nil {
		t.Fatalf("a whitespace expected digest was treated as a mismatch: %v", err)
	}
}

func TestTheDigestRefusalNamesBothValues(t *testing.T) {
	// An operator holding a document that will not apply has to be able to tell
	// which side is wrong. "digest mismatch" alone sends them looking in the wrong
	// place, and the fix is often just re-copying the digest.
	document := readFixtureDelta(t)
	encoded := encodeFixtureDelta(t, document)
	err := stow.VerifyDeltaDigest(encoded, stow.DeltaDocumentDigest(append(append([]byte(nil), encoded...), ' ')))
	if err == nil {
		t.Fatal("an altered document was accepted")
	}
	message := err.Error()
	for _, want := range []string{stow.DeltaDocumentDigest(encoded), stow.DeltaDocumentDigest(append(append([]byte(nil), encoded...), ' '))} {
		if !contains(message, want) {
			t.Fatalf("the refusal does not name %q, so an operator cannot tell which side is wrong:\n%s", want, message)
		}
	}
}

func TestDeltaDocumentDigestIsOverTheEncodedBytes(t *testing.T) {
	// The digest has to be of the document as it travels, not of a re-encoding of
	// the parsed struct. A re-encode is only stable while the field order and the
	// omitempty behaviour do not change, so a digest computed from it would start
	// failing the day a field was added.
	document := readFixtureDelta(t)
	encoded := encodeFixtureDelta(t, document)
	if stow.DeltaDocumentDigest(encoded) == stow.DeltaDocumentDigest(encoded[1:]) {
		t.Fatal("the digest does not depend on the bytes, so it is not a document digest")
	}
	if len(stow.DeltaDocumentDigest(encoded)) != 64 {
		t.Fatalf("the digest is %d characters, not 64 hex characters", len(stow.DeltaDocumentDigest(encoded)))
	}
}

// --- fixtures -------------------------------------------------------------
//
// A hand-built document rather than one produced by CreateDelta, because the
// defect is about what a document means once it is separate from the checkpoints
// it came from. Building it here keeps the test independent of the code that
// writes documents, which is the code a fix is least likely to break and most
// likely to be adjusted to make a test pass.

func readFixtureDelta(t *testing.T) *stow.DeltaDocument {
	t.Helper()
	return &stow.DeltaDocument{
		Version:  stow.DeltaVersion,
		BaseID:   "cp_base000000000000000000",
		TargetID: "cp_target00000000000000",
		Changes: []stow.DeltaChange{{
			Path: "notes.txt",
			Kind: stow.DeltaChangeAdded,
			To: &stow.CheckpointFile{
				Path: "notes.txt", Size: 7, Mode: 0o644,
				SHA256: "c9e4f7d21ee0b7c2a4dbb3f5b6e1a2c3d4e5f60718293a4b5c6d7e8f901234567",
			},
		}},
		Files:   1,
		Bytes:   7,
		Content: map[string][]byte{"notes.txt": []byte("PAYLOAD")},
	}
}

func encodeFixtureDelta(t *testing.T, document *stow.DeltaDocument) []byte {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode the fixture document: %v", err)
	}
	return encoded
}

// renameFixtureAddition rewrites an addition's destination in the change list and
// the content map together, which is what an alteration in transit has to do to
// stay self-consistent. It returns the new path, or nil if there is no such
// addition to rename.
func renameFixtureAddition(t *testing.T, document *stow.DeltaDocument, from, to string) *string {
	t.Helper()
	for i := range document.Changes {
		change := &document.Changes[i]
		if change.Kind != stow.DeltaChangeAdded || change.Path != from {
			continue
		}
		content, ok := document.Content[from]
		if !ok {
			t.Fatalf("the fixture adds %q but carries no content for it", from)
		}
		change.Path = to
		if change.To != nil {
			change.To.Path = to
		}
		delete(document.Content, from)
		document.Content[to] = content
		return &to
	}
	return nil
}

// isDigestMismatch checks the sentinel rather than the message, so rewording the
// refusal does not silently turn this into a test of prose.
func isDigestMismatch(err error) bool {
	return errors.Is(err, stow.ErrDeltaDigestMismatch)
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func upper(text string) string {
	return strings.ToUpper(text)
}
