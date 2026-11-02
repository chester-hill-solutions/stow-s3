package storage

import "testing"

func TestWritePreconditionsRespectWeakValidators(t *testing.T) {
	existing := &ObjectMeta{ETag: `"abc"`}
	if err := CheckWritePreconditions(PutOptions{IfNoneMatch: `W/"abc"`}, existing); err != ErrPreconditionFailed {
		t.Fatalf("weak If-None-Match error = %v", err)
	}
	if err := CheckWritePreconditions(PutOptions{IfMatch: `W/"abc"`}, existing); err != ErrPreconditionFailed {
		t.Fatalf("weak If-Match error = %v", err)
	}
	if err := CheckWritePreconditions(PutOptions{IfMatch: `"abc"`}, existing); err != nil {
		t.Fatalf("strong If-Match error = %v", err)
	}
}

// An entity tag is opaque. It identifies a representation, so a tag that
// differs only in case names a different representation and a condition
// carrying it must not match.
func TestOpaqueEntityTagsAreNotFoldedByCase(t *testing.T) {
	existing := &ObjectMeta{ETag: `"abc123"`}
	if err := CheckWritePreconditions(PutOptions{IfMatch: `"ABC123"`}, existing); err != ErrPreconditionFailed {
		t.Fatalf("a case-changed If-Match error = %v, want a refusal", err)
	}
	if err := CheckWritePreconditions(PutOptions{IfNoneMatch: `"ABC123"`}, existing); err != nil {
		t.Fatalf("a case-changed If-None-Match error = %v, want the write to proceed", err)
	}
	if err := CheckCopySourceConditions(CopyOptions{SourceIfMatch: `"ABC123"`}, existing); err != ErrPreconditionFailed {
		t.Fatalf("a case-changed copy-source If-Match error = %v, want a refusal", err)
	}
	if err := CheckCopySourceConditions(CopyOptions{SourceIfNoneMatch: `"ABC123"`}, existing); err != nil {
		t.Fatalf("a case-changed copy-source If-None-Match error = %v, want the copy to proceed", err)
	}
}

// The wildcard names every representation rather than one, so it keeps matching
// whatever the tag is.
func TestWildcardEntityTagsStillMatch(t *testing.T) {
	existing := &ObjectMeta{ETag: `"abc"`}
	if err := CheckWritePreconditions(PutOptions{IfMatch: "*"}, existing); err != nil {
		t.Fatalf("a wildcard If-Match error = %v, want the write to proceed", err)
	}
	if err := CheckWritePreconditions(PutOptions{IfNoneMatch: "*"}, existing); err != ErrPreconditionFailed {
		t.Fatalf("a wildcard If-None-Match error = %v, want a refusal", err)
	}
}
