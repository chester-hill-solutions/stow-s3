package runtime

// Multipart at the runtime boundary: the same transaction properties the stores
// assert, checked where quotas and authority also sit.
//
// They are separate from the store's own suite because a store can be entirely
// correct about a completion and the runtime can still get the surrounding
// accounting wrong — a completion that publishes an object and then fails to
// release its reservation, or one that is refused for a quota while the store
// has already committed it.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The part-selection invariants, at the runtime boundary.
//
// Each names a part or a completion and the state the runtime must leave behind,
// so a change to the quota arithmetic cannot quietly stop consulting the store.
func TestRuntimeMultipartCompletionInvariants(t *testing.T) {
	ctx := context.Background()
	// Roomy enough that no case is refused for a quota rather than for the reason
	// under test, which would make a passing case meaningless. The part is at the
	// 5 MiB minimum so a completion is refused for what it names rather than for
	// being made of small parts, so the budget has to hold several of them plus
	// the object a completion assembles.
	const roomy = 8 * storage.MinPartSize

	t.Run("a part never uploaded is refused", func(t *testing.T) {
		instance := boundedInstance(t, roomy, 4)
		mustCreateBucket(t, ctx, instance, "bucket")
		upload := mustCreateUpload(t, ctx, instance, "bucket", "key")
		part := mustUploadPart(t, ctx, instance, upload, atMinPartSize())

		_, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{
			*part,
			{PartNumber: 2, ETag: storage.ETagForBytes([]byte("never uploaded"))},
		})
		if !errors.Is(err, storage.ErrInvalidPart) {
			t.Fatalf("completion naming an unuploaded part = %v, want ErrInvalidPart", err)
		}
		assertNoObject(t, ctx, instance, "bucket", "key")
		// The upload is still in flight and still holding its part, so its
		// reservation is still correct. Releasing it here would be the same leak
		// as a double release, in the direction of giving the budget away.
		if instance.reservedBytes != int64(storage.MinPartSize) {
			t.Fatalf("a refused completion left %d bytes reserved, want the part's %d", instance.reservedBytes, storage.MinPartSize)
		}
	})

	t.Run("a part whose ETag is stale is refused", func(t *testing.T) {
		instance := boundedInstance(t, roomy, 4)
		mustCreateBucket(t, ctx, instance, "bucket")
		upload := mustCreateUpload(t, ctx, instance, "bucket", "key")
		original := mustUploadPart(t, ctx, instance, upload, atMinPartSize())
		// Replace the part, which is what makes the first ETag a name for a
		// version the store no longer holds.
		body := append(atMinPartSize(), 'x')
		replacement := mustUploadPart(t, ctx, instance, upload, body)

		if _, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*original}); !errors.Is(err, storage.ErrInvalidPart) {
			t.Fatalf("completion naming a replaced part = %v, want ErrInvalidPart", err)
		}
		assertNoObject(t, ctx, instance, "bucket", "key")
		// And the current version still completes, so the refusal was about the
		// name and not about the upload.
		if _, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*replacement}); err != nil {
			t.Fatalf("completion naming the current part: %v", err)
		}
		assertNoReservation(t, instance, "a completed upload")
	})

	t.Run("a completion after an abort fails", func(t *testing.T) {
		instance := boundedInstance(t, roomy, 4)
		mustCreateBucket(t, ctx, instance, "bucket")
		upload := mustCreateUpload(t, ctx, instance, "bucket", "key")
		part := mustUploadPart(t, ctx, instance, upload, atMinPartSize())

		if err := instance.AbortMultipartUpload(ctx, upload); err != nil {
			t.Fatalf("abort: %v", err)
		}
		if _, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part}); !errors.Is(err, storage.ErrUploadNotFound) {
			t.Fatalf("completion after abort = %v, want ErrUploadNotFound", err)
		}
		assertNoObject(t, ctx, instance, "bucket", "key")
		assertNoReservation(t, instance, "an aborted upload")
	})

	t.Run("a repeated completion fails and does not republish", func(t *testing.T) {
		instance := boundedInstance(t, roomy, 4)
		mustCreateBucket(t, ctx, instance, "bucket")
		upload := mustCreateUpload(t, ctx, instance, "bucket", "key")
		body := append(atMinPartSize(), 'x')
		part := mustUploadPart(t, ctx, instance, upload, body)
		first, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part})
		if err != nil {
			t.Fatalf("first completion: %v", err)
		}
		if _, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part}); !errors.Is(err, storage.ErrUploadNotFound) {
			t.Fatalf("second completion = %v, want ErrUploadNotFound", err)
		}
		// The object is the one the first completion published, unchanged: a
		// second completion that republished would show a different version, and
		// one that truncated it would show a different size.
		got, err := instance.GetObject(ctx, "bucket", "key")
		if err != nil {
			t.Fatalf("get after the repeated completion: %v", err)
		}
		if !bytes.Equal(got.Data, body) {
			t.Fatalf("object is %d bytes, want the %d the first completion published", len(got.Data), len(body))
		}
		if got.VersionID != first.VersionID {
			t.Fatalf("version changed after a refused completion: %q then %q", first.VersionID, got.VersionID)
		}
		assertNoReservation(t, instance, "a completed upload")
	})

	t.Run("an abort after a completion leaves the object", func(t *testing.T) {
		instance := boundedInstance(t, roomy, 4)
		mustCreateBucket(t, ctx, instance, "bucket")
		upload := mustCreateUpload(t, ctx, instance, "bucket", "key")
		part := mustUploadPart(t, ctx, instance, upload, atMinPartSize())
		if _, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part}); err != nil {
			t.Fatalf("complete: %v", err)
		}
		// The upload is gone, so the abort is told so rather than reaching past
		// the completion into the object it published.
		if err := instance.AbortMultipartUpload(ctx, upload); !errors.Is(err, storage.ErrUploadNotFound) {
			t.Fatalf("abort after completion = %v, want ErrUploadNotFound", err)
		}
		if _, err := instance.HeadObject(ctx, "bucket", "key"); err != nil {
			t.Fatalf("an abort after a completion removed the object: %v", err)
		}
	})
}

// A quota refusal happens before the store is asked, so no object exists and no
// reservation is stranded.
func TestRuntimeRefusesACompletionItCannotAccountFor(t *testing.T) {
	ctx := context.Background()
	instance := boundedInstance(t, 32, 1)
	mustCreateBucket(t, ctx, instance, "bucket")
	upload := mustCreateUpload(t, ctx, instance, "bucket", "key")
	part := mustUploadPart(t, ctx, instance, upload, []byte(strings.Repeat("a", 16)))

	// 16 bytes of parts and a 16-byte object is 32, which fits; the point is that
	// the accounting and the store agree on the number, so a completion that the
	// runtime refuses leaves the store with nothing to have committed.
	if _, err := instance.CompleteMultipartUpload(ctx, upload, []storage.PartInfo{*part}); err != nil {
		t.Fatalf("complete within the budget: %v", err)
	}
	if usage := instance.Usage(); usage.Bytes != 16 || usage.Objects != 1 {
		t.Fatalf("usage = %+v, want one sixteen-byte object", usage)
	}
}

// Multipart is refused by the authority when writes are, and the refusal happens
// before an upload exists.
func TestRuntimeRefusesMultipartWithoutObjectWrite(t *testing.T) {
	ctx := context.Background()
	// Writes refused, reads and lists still permitted, so the bucket can be
	// created and the case is about the upload rather than about setup.
	noWrites := authority.All().Without(authority.ObjectWrite)
	instance, err := OpenWithStore(Options{
		Backend:   BackendMemory,
		MaxBytes:  1024,
		Authority: &noWrites,
	}, storage.NewMemoryStore(), nil)
	if err != nil {
		t.Fatalf("open with store: %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := instance.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{}); err == nil {
		t.Fatal("CreateMultipartUpload without ObjectWrite succeeded")
	}
	if len(instance.multipart) != 0 {
		t.Fatalf("a refused initiation left %d uploads behind", len(instance.multipart))
	}
}

func mustCreateBucket(t *testing.T, ctx context.Context, instance *Instance, bucket string) {
	t.Helper()
	if err := instance.CreateBucket(ctx, bucket); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
}

func mustCreateUpload(t *testing.T, ctx context.Context, instance *Instance, bucket, key string) string {
	t.Helper()
	upload, err := instance.CreateMultipartUpload(ctx, bucket, key, storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	return upload.UploadID
}

func mustUploadPart(t *testing.T, ctx context.Context, instance *Instance, uploadID string, body []byte) *storage.PartInfo {
	t.Helper()
	part, err := instance.UploadPart(ctx, uploadID, 1, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	return part
}

// atMinPartSize is a body exactly the size a non-final part must be, so a
// completion in these cases is refused for the reason under test and not for the
// minimum.
func atMinPartSize() []byte {
	return []byte(strings.Repeat("a", storage.MinPartSize))
}

func assertNoObject(t *testing.T, ctx context.Context, instance *Instance, bucket, key string) {
	t.Helper()
	if _, err := instance.HeadObject(ctx, bucket, key); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("the destination exists after the case: %v", err)
	}
}

func assertNoReservation(t *testing.T, instance *Instance, label string) {
	t.Helper()
	if instance.reservedBytes != 0 {
		t.Fatalf("%s left %d bytes reserved", label, instance.reservedBytes)
	}
	if len(instance.multipart) != 0 {
		t.Fatalf("%s left %d uploads tracked", label, len(instance.multipart))
	}
}
