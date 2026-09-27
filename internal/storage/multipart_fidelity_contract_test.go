package storage_test

// Multipart fidelity: what an upload fixes at initiation reaches the object it
// publishes, and what completion does to state it did not name.
//
// The refusals live in multipart_refusal_contract_test.go, because a case whose
// subject is "this changes nothing" reads differently next to one whose subject is
// "this is what gets published".

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The properties a multipart initiation fixes, and the reason they matter.
//
// The storage model kept only the upload ID, the bucket, the key and the
// initiation time. Everything else a caller supplied to CreateMultipartUpload
// had nowhere to live, so completion had nothing to build the object's metadata
// from and published an object with no content type, no user metadata and no
// checksum. A client that uploaded a JSON document and asked for a content type
// read back an object indistinguishable from one written with no options.
//
// This is a conformance case because the answer must be the same whichever store
// is underneath: a caller who cannot rely on it cannot use multipart at all
// above a memory store and work below a filesystem one.
func TestMultipartInitiationPropertiesSurviveIntoTheCompletedObject(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}

		// A content type, user metadata, and a checksum configuration, which
		// between them are every creation-time property stow accepts.
		body := append(make([]byte, storage.MinPartSize), []byte("tail")...)
		checksum, err := storage.ComputeChecksum("CRC32", body)
		if err != nil {
			t.Fatalf("compute checksum: %v", err)
		}
		upload, err := multi.CreateMultipartUpload(ctx, "uploads", "object.bin", storage.MultipartOptions{
			ContentType:       "application/json",
			Metadata:          map[string]string{"x-amz-meta-foo": "bar", "x-amz-meta-owner": "dev"},
			ChecksumAlgorithm: "CRC32",
			ChecksumValue:     checksum,
		})
		if err != nil {
			t.Fatalf("create upload: %v", err)
		}

		// The upload itself reports what it will publish, so a caller reconciling
		// in-flight uploads can answer without completing it.
		got, err := multi.GetMultipartUpload(ctx, upload.UploadID)
		if err != nil {
			t.Fatalf("get upload: %v", err)
		}
		assertInitiationProperties(t, got.Options, "in-flight upload", checksum)

		part, err := multi.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("upload part: %v", err)
		}
		meta, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part})
		if err != nil {
			t.Fatalf("complete: %v", err)
		}
		assertInitiationProperties(t, storage.MultipartOptions{
			ContentType:       meta.ContentType,
			Metadata:          meta.Metadata,
			ChecksumAlgorithm: meta.ChecksumAlgorithm,
			ChecksumValue:     meta.ChecksumValue,
		}, "completed object", checksum)

		// And what a later read sees, which is what a client actually observes.
		head, err := store.HeadObject(ctx, "uploads", "object.bin")
		if err != nil {
			t.Fatalf("head object: %v", err)
		}
		if head.ContentType != "application/json" || head.Metadata["x-amz-meta-foo"] != "bar" {
			t.Errorf("head = %+v, want the initiation's content type and metadata", head)
		}
		rc, _, err := store.GetObject(ctx, "uploads", "object.bin")
		if err != nil {
			t.Fatalf("get object: %v", err)
		}
		defer rc.Close()
		got2, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read object: %v", err)
		}
		if !bytes.Equal(got2, body) {
			t.Errorf("object body is %d bytes, want the uploaded %d", len(got2), len(body))
		}
	})
}

// assertInitiationProperties checks the three creation-time properties against
// what was supplied, in one place so the upload and the completed object are
// held to the same standard.
func assertInitiationProperties(t *testing.T, got storage.MultipartOptions, label, checksum string) {
	t.Helper()
	if got.ContentType != "application/json" {
		t.Errorf("%s ContentType = %q, want application/json", label, got.ContentType)
	}
	if got.Metadata["x-amz-meta-foo"] != "bar" || got.Metadata["x-amz-meta-owner"] != "dev" {
		t.Errorf("%s metadata = %+v, want the metadata given at initiation", label, got.Metadata)
	}
	if got.ChecksumAlgorithm != "CRC32" || got.ChecksumValue != checksum {
		t.Errorf("%s checksum = %s/%s, want CRC32/%s", label, got.ChecksumAlgorithm, got.ChecksumValue, checksum)
	}
}

// A checksum the client claims for the object it is assembling is verified
// against the object that is published.
//
// A single write verifies it, and a completion that accepted one without
// checking would be a hole in the same rule reached by a longer path: the
// integrity claim is about the object's bytes, and there is no reason it should
// hold for a body of one megabyte and not for a body of six.
func TestMultipartCompletionRejectsAChecksumTheAssembledObjectContradicts(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		part1 := make([]byte, storage.MinPartSize)
		part2 := []byte("tail")
		// The correct checksum for the first part alone, not for the whole object.
		partChecksum, err := storage.ComputeChecksum("CRC32", part1)
		if err != nil {
			t.Fatalf("compute checksum: %v", err)
		}
		upload, err := multi.CreateMultipartUpload(ctx, "uploads", "object.bin", storage.MultipartOptions{
			ChecksumAlgorithm: "CRC32",
			ChecksumValue:     partChecksum,
		})
		if err != nil {
			t.Fatalf("create upload: %v", err)
		}
		first, err := multi.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(part1))
		if err != nil {
			t.Fatalf("upload first part: %v", err)
		}
		if _, err := multi.UploadPart(ctx, upload.UploadID, 2, bytes.NewReader(part2)); err != nil {
			t.Fatalf("upload second part: %v", err)
		}

		_, err = multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*first, {PartNumber: 2, ETag: storage.ETagForBytes(part2)}})
		if !errors.Is(err, storage.ErrChecksumMismatch) {
			t.Fatalf("complete with a checksum for the wrong bytes = %v, want ErrChecksumMismatch", err)
		}
		// And nothing was published: a refused completion leaves the destination
		// exactly as it was.
		if _, err := store.HeadObject(ctx, "uploads", "object.bin"); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Errorf("a refused completion published an object: %v", err)
		}
	})
}

// The object properties of an in-flight upload are not aliased by the store.
//
// This is the same rule the object model already follows for a stored object
// (TestStoreMetadataMapsDoNotAliasStoredState) applied to the upload: a caller
// that mutates the map it passed in, or the map it was handed back, must not be
// able to change what a later completion publishes.
func TestMultipartUploadMetadataDoesNotAliasStoredState(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		input := map[string]string{"x-amz-meta-foo": "original"}
		upload, err := multi.CreateMultipartUpload(ctx, "uploads", "object.bin", storage.MultipartOptions{Metadata: input})
		if err != nil {
			t.Fatalf("create upload: %v", err)
		}
		input["x-amz-meta-foo"] = "changed by the caller"
		upload.Options.Metadata["x-amz-meta-foo"] = "changed through the descriptor"

		part, err := multi.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("body"))
		if err != nil {
			t.Fatalf("upload part: %v", err)
		}
		meta, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part})
		if err != nil {
			t.Fatalf("complete: %v", err)
		}
		if meta.Metadata["x-amz-meta-foo"] != "original" {
			t.Fatalf("completed metadata = %+v, want the value given at initiation", meta.Metadata)
		}
	})
}

// A completion is a transaction. These are the ways it can fail to be one, and
// each was reachable independently of the others.

// An invalid completion must not touch the destination.
//
// The destination already holds an object — that is the interesting case, since
// an upload completing over an existing key is how a client replaces a large
// object with a newer one. A completion that fails afterwards must leave the
// previous object exactly as it was, not a partial write of the new one.
func TestAFailedCompletionLeavesThePreviousObjectIntact(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		original, err := store.PutObject(ctx, "uploads", "object.bin", strings.NewReader("the previous object"), storage.PutOptions{
			ContentType: "text/plain",
			Metadata:    map[string]string{"x-amz-meta-owner": "original"},
		})
		if err != nil {
			t.Fatalf("put original: %v", err)
		}
		upload, err := multi.CreateMultipartUpload(ctx, "uploads", "object.bin", storage.MultipartOptions{ContentType: "application/json"})
		if err != nil {
			t.Fatalf("create upload: %v", err)
		}
		part, err := multi.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("the replacement"))
		if err != nil {
			t.Fatalf("upload part: %v", err)
		}

		// Each of these completions is invalid, and each must change nothing.
		invalid := map[string][]storage.PartInfo{
			"a wrong ETag":      {{PartNumber: 1, ETag: `"not-the-part-etag"`}},
			"an absent part":    {{PartNumber: 1, ETag: part.ETag}, {PartNumber: 2, ETag: part.ETag}},
			"a part over 10000": {{PartNumber: 1, ETag: part.ETag}, {PartNumber: 10001, ETag: part.ETag}},
			"no parts at all":   nil,
		}
		for name, parts := range invalid {
			if _, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, parts); err == nil {
				t.Errorf("completion with %s succeeded", name)
			}
			head, err := store.HeadObject(ctx, "uploads", "object.bin")
			if err != nil {
				t.Fatalf("head after a completion with %s: %v", name, err)
			}
			if head.ETag != original.ETag || head.ContentType != "text/plain" || head.Metadata["x-amz-meta-owner"] != "original" {
				t.Fatalf("a completion with %s disturbed the destination: %+v", name, head)
			}
			rc, _, err := store.GetObject(ctx, "uploads", "object.bin")
			if err != nil {
				t.Fatalf("get after a completion with %s: %v", name, err)
			}
			body, _ := io.ReadAll(rc)
			rc.Close()
			if string(body) != "the previous object" {
				t.Fatalf("a completion with %s changed the destination's bytes to %q", name, body)
			}
		}

		// The upload is still completable afterwards: a refusal is not a
		// cancellation, and the caller retries with a legal part list.
		meta, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part})
		if err != nil {
			t.Fatalf("complete after refusals: %v", err)
		}
		if meta.ContentType != "application/json" {
			t.Errorf("completed ContentType = %q, want the initiation's application/json", meta.ContentType)
		}
	})
}

// A completion after an abort fails, and a repeated completion fails.
//
// Both are the same question — is the upload still there? — and both matter
// because a client retrying a completion it believes timed out must not be able
// to publish a second object from parts the store has already discarded.
func TestCompletionAfterAbortAndRepeatedCompletionFail(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}

		aborted, err := multi.CreateMultipartUpload(ctx, "uploads", "aborted.bin", storage.MultipartOptions{})
		if err != nil {
			t.Fatalf("create aborted upload: %v", err)
		}
		abortedPart, err := multi.UploadPart(ctx, aborted.UploadID, 1, strings.NewReader("aborted"))
		if err != nil {
			t.Fatalf("upload part: %v", err)
		}
		if err := multi.AbortMultipartUpload(ctx, aborted.UploadID); err != nil {
			t.Fatalf("abort: %v", err)
		}
		if _, err := multi.CompleteMultipartUpload(ctx, aborted.UploadID, []storage.PartInfo{*abortedPart}); !errors.Is(err, storage.ErrUploadNotFound) {
			t.Fatalf("completion after abort = %v, want ErrUploadNotFound", err)
		}
		if _, err := store.HeadObject(ctx, "uploads", "aborted.bin"); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Errorf("a completion after an abort published an object: %v", err)
		}

		completed, err := multi.CreateMultipartUpload(ctx, "uploads", "completed.bin", storage.MultipartOptions{})
		if err != nil {
			t.Fatalf("create completed upload: %v", err)
		}
		part, err := multi.UploadPart(ctx, completed.UploadID, 1, strings.NewReader("once"))
		if err != nil {
			t.Fatalf("upload part: %v", err)
		}
		if _, err := multi.CompleteMultipartUpload(ctx, completed.UploadID, []storage.PartInfo{*part}); err != nil {
			t.Fatalf("complete: %v", err)
		}
		if _, err := multi.CompleteMultipartUpload(ctx, completed.UploadID, []storage.PartInfo{*part}); !errors.Is(err, storage.ErrUploadNotFound) {
			t.Fatalf("repeated completion = %v, want ErrUploadNotFound", err)
		}
		// And an abort after a successful completion does not take the committed
		// object with it: the object is not the upload's to remove.
		if err := multi.AbortMultipartUpload(ctx, completed.UploadID); !errors.Is(err, storage.ErrUploadNotFound) {
			t.Fatalf("abort after completion = %v, want ErrUploadNotFound", err)
		}
		if _, err := store.HeadObject(ctx, "uploads", "completed.bin"); err != nil {
			t.Fatalf("an abort after a successful completion removed the object: %v", err)
		}
	})
}

// Replacing a part invalidates the version it replaced.
//
// A client that uploads a part, decides it was wrong, and re-uploads it holds
// the first ETag. If completion accepted that first ETag it would assemble a
// part the store no longer has, from bytes the store no longer holds — so the
// stale ETag must be refused and the current one accepted.
func TestReplacingAPartInvalidatesItsPreviousETag(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		upload, err := multi.CreateMultipartUpload(ctx, "uploads", "object.bin", storage.MultipartOptions{})
		if err != nil {
			t.Fatalf("create upload: %v", err)
		}
		first, err := multi.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(make([]byte, storage.MinPartSize)))
		if err != nil {
			t.Fatalf("first upload of the part: %v", err)
		}
		replacement, err := multi.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(append(make([]byte, storage.MinPartSize), 'x')))
		if err != nil {
			t.Fatalf("replacement upload of the part: %v", err)
		}
		if first.ETag == replacement.ETag {
			t.Fatal("the replacement has the same ETag as the part it replaced")
		}

		// The stale ETag is refused.
		if _, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*first}); !errors.Is(err, storage.ErrInvalidPart) {
			t.Fatalf("completion naming a replaced part = %v, want ErrInvalidPart", err)
		}
		// The current one is accepted, and what lands is the replacement.
		if _, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*replacement}); err != nil {
			t.Fatalf("complete with the replacement: %v", err)
		}
		rc, _, err := store.GetObject(ctx, "uploads", "object.bin")
		if err != nil {
			t.Fatalf("get object: %v", err)
		}
		defer rc.Close()
		body, _ := io.ReadAll(rc)
		if int64(len(body)) != int64(storage.MinPartSize)+1 || body[len(body)-1] != 'x' {
			t.Fatalf("the object is %d bytes and does not end with the replacement's byte", len(body))
		}
	})
}

// A completion and an abort racing produce one coherent outcome, not a state
// that is neither.
//
// This is a race, so it is asserted as a property and repeated: exactly one of
// the two wins, the loser is told the upload is gone, and the destination is
// either untouched or fully published. The failure this rules out is a store
// that completed the object and then aborted the upload around it, or one that
// removed the staging for a completion that was still assembling.
func TestConcurrentCompletionAndAbortHaveOneOutcome(t *testing.T) {
	withStores(t, func(t *testing.T, store storage.Store) {
		ctx := context.Background()
		multi := requireMultipart(t, store)
		if err := store.CreateBucket(ctx, "uploads"); err != nil {
			t.Fatalf("create bucket: %v", err)
		}
		body := make([]byte, storage.MinPartSize)

		for attempt := 0; attempt < 20; attempt++ {
			// A key per attempt. Sharing one would let a previous attempt's
			// published object be mistaken for this attempt's, which is exactly
			// the state the assertion below is about.
			key := fmt.Sprintf("object-%d.bin", attempt)
			upload, err := multi.CreateMultipartUpload(ctx, "uploads", key, storage.MultipartOptions{})
			if err != nil {
				t.Fatalf("create upload: %v", err)
			}
			part, err := multi.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("upload part: %v", err)
			}

			var completed, aborted bool
			var wg sync.WaitGroup
			start := make(chan struct{})
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				if _, err := multi.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err == nil {
					completed = true
				}
			}()
			go func() {
				defer wg.Done()
				<-start
				if err := multi.AbortMultipartUpload(ctx, upload.UploadID); err == nil {
					aborted = true
				}
			}()
			close(start)
			wg.Wait()

			if completed == aborted {
				t.Fatalf("attempt %d: completed=%v aborted=%v; exactly one must win", attempt, completed, aborted)
			}
			// The destination reflects the winner and only the winner.
			head, err := store.HeadObject(ctx, "uploads", key)
			if completed {
				if err != nil || head.Size != int64(storage.MinPartSize) {
					t.Fatalf("attempt %d: the completion won but the object is not published: %v %+v", attempt, err, head)
				}
			} else if !errors.Is(err, storage.ErrObjectNotFound) {
				t.Fatalf("attempt %d: the abort won but the destination is %v", attempt, err)
			}
			// And the bucket is free of the upload either way, so a bucket that
			// was deletable before is deletable after.
			if _, err := multi.GetMultipartUpload(ctx, upload.UploadID); !errors.Is(err, storage.ErrUploadNotFound) {
				t.Fatalf("attempt %d: the upload survived both operations: %v", attempt, err)
			}
		}
	})
}
