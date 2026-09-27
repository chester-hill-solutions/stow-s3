package runtime

// Multipart quota accounting: what a session's advertised byte and object bounds
// mean while an upload is in flight, and what they mean after it completes.
//
// The bound a session publishes is a promise about the bytes it holds. An upload
// in flight holds bytes — the parts — and a completion holds them twice over, the
// parts and the object being assembled from them. If the accounting counts only
// committed objects, a session can hold twice what it says it will, which is
// exactly the session whose memory grows past the limit the caller set.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// boundedInstance opens an instance with an explicit byte budget over a memory
// store, which is the backend where the accounting is the only thing bounding
// memory.
func boundedInstance(t *testing.T, maxBytes, maxObjects int64) *Instance {
	t.Helper()
	instance, err := OpenWithStore(Options{
		Backend:    BackendMemory,
		MaxBytes:   maxBytes,
		MaxObjects: maxObjects,
	}, storage.NewMemoryStore(), nil)
	if err != nil {
		t.Fatalf("open with store: %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return instance
}

// The bounded state is committed bytes plus the parts of every upload in flight
// plus the object a completion is assembling.
//
// The last term is the one that was missing. A completion holds the selected
// parts and builds a second copy of the same bytes before publishing, so a
// session that could fit the object in its budget could not fit the completion —
// and the store went ahead and built it anyway. Peak is asserted here rather than
// the object alone because the object alone is what the old accounting checked,
// and it is the wrong question: the question is what the process holds.
func TestCompletionReservesTheBufferItAssembles(t *testing.T) {
	ctx := context.Background()
	const objectSize = 4
	// Exactly the object and its assembly, and not one byte less. The completion
	// is therefore permitted, and a budget of objectSize-1 would refuse it — which
	// is the boundary the case is about.
	instance := boundedInstance(t, 2*objectSize, 1)
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	part, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("1234"))
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	if _, err := instance.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
		t.Fatalf("complete inside the budget: %v", err)
	}
	if usage := instance.Usage(); usage.Bytes != objectSize || instance.reservedBytes != 0 {
		t.Fatalf("usage = %+v, reserved = %d; the object is committed and nothing is reserved", usage, instance.reservedBytes)
	}
}

// A budget that cannot hold the object and its assembly refuses the completion,
// and refuses it before the store assembles anything.
//
// This is the same accounting seen from the refusal side. The store is not asked
// to build an object the session has no room for, and the parts stay reserved —
// a refused completion is not a cancelled upload, and the caller may complete it
// later when there is room.
func TestCompletionIsRefusedWhenTheAssemblyDoesNotFit(t *testing.T) {
	ctx := context.Background()
	// Enough for the 4-byte object, not enough for the object and its assembly.
	instance := boundedInstance(t, 4, 1)
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	part, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("1234"))
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	if _, err := instance.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("complete = %v, want ErrQuotaExceeded", err)
	}
	if instance.reservedBytes != 4 {
		t.Fatalf("a refused completion released the upload's parts: %d bytes still reserved", instance.reservedBytes)
	}
	if usage := instance.Usage(); usage.Objects != 0 || usage.Bytes != 0 {
		t.Fatalf("a refused completion published an object: %+v", usage)
	}
}

// Every exit from an upload releases its reservation, and each release happens
// once.
//
// An upload's bytes are reserved when its parts arrive. They must come back when
// the upload ends, whichever way it ends, and they must not come back twice — a
// double release would hand the budget to two uploads at once, which is the same
// bound defeated by arithmetic rather than by memory.
func TestUploadReservationsAreReleasedExactlyOnce(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		leave func(t *testing.T, instance *Instance, uploadID string)
	}{
		{
			name:  "abort",
			leave: func(t *testing.T, instance *Instance, uploadID string) { abortUpload(t, instance, uploadID) },
		},
		{
			name: "a completion that fails on a bad ETag, then an abort",
			leave: func(t *testing.T, instance *Instance, uploadID string) {
				// The upload stays in flight after the refusal, so its parts stay
				// reserved: a refusal is not an exit from the upload. What matters
				// is that the refusal releases nothing, so the abort below gives
				// back exactly what the parts took.
				if _, err := instance.CompleteMultipartUpload(context.Background(), uploadID, []storage.PartInfo{{PartNumber: 1, ETag: `"wrong"`}}); err == nil {
					t.Fatal("a completion with a wrong ETag succeeded")
				}
				if instance.reservedBytes == 0 {
					t.Fatal("a refused completion released the upload's reservation")
				}
				abortUpload(t, instance, uploadID)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			instance := boundedInstance(t, 64, 4)
			if err := instance.CreateBucket(ctx, "bucket"); err != nil {
				t.Fatalf("create bucket: %v", err)
			}
			upload, err := instance.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
			if err != nil {
				t.Fatalf("create upload: %v", err)
			}
			if _, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("12345")); err != nil {
				t.Fatalf("upload part: %v", err)
			}
			if instance.reservedBytes != 5 {
				t.Fatalf("reserved = %d after a 5-byte part, want 5", instance.reservedBytes)
			}
			tc.leave(t, instance, upload.UploadID)
			if instance.reservedBytes != 0 {
				t.Fatalf("reserved = %d after the upload ended, want 0", instance.reservedBytes)
			}
			// And the budget is whole: a second upload of the same size still
			// fits, which a double release would also permit and a leak would not.
			second, err := instance.CreateMultipartUpload(ctx, "bucket", "other", storage.MultipartOptions{})
			if err != nil {
				t.Fatalf("second upload: %v", err)
			}
			if _, err := instance.UploadPart(ctx, second.UploadID, 1, strings.NewReader("12345")); err != nil {
				t.Fatalf("second part: %v", err)
			}
			if instance.reservedBytes != 5 {
				t.Fatalf("reserved = %d after the second upload, want 5", instance.reservedBytes)
			}
		})
	}
}

func abortUpload(t *testing.T, instance *Instance, uploadID string) {
	t.Helper()
	if err := instance.AbortMultipartUpload(context.Background(), uploadID); err != nil {
		t.Fatalf("abort: %v", err)
	}
}

// Replacing a part releases the bytes the part it replaced was holding.
//
// A client re-uploading a part has not added an object; it has corrected one. If
// the correction did not release the first part's bytes, a client that replaced
// a 100 MiB part with a 1 MiB one would permanently lose 99 MiB of its budget,
// and the loss would be invisible: every request would be refused with a quota
// error and nothing in the state would say why.
func TestReplacingAPartReleasesTheBytesItReplaced(t *testing.T) {
	ctx := context.Background()
	instance := boundedInstance(t, 20, 1)
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	if _, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader(strings.Repeat("a", 15))); err != nil {
		t.Fatalf("upload 15-byte part: %v", err)
	}
	if instance.reservedBytes != 15 {
		t.Fatalf("reserved = %d, want 15", instance.reservedBytes)
	}
	// A replacement small enough to fit only if the original is released.
	if _, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("bb")); err != nil {
		t.Fatalf("replacement part: %v", err)
	}
	if instance.reservedBytes != 2 {
		t.Fatalf("reserved = %d after replacing a 15-byte part with a 2-byte one, want 2", instance.reservedBytes)
	}
}

// Completing over an existing object accounts for the replacement, not the sum.
//
// The object being replaced is released as the new one is committed, so the
// budget must show the difference rather than both. A session that overwrote a
// large object with a small one would otherwise find its budget consumed by an
// object that no longer exists.
func TestCompletionOverAnExistingObjectReleasesTheOldBytes(t *testing.T) {
	ctx := context.Background()
	instance := boundedInstance(t, 40, 1)
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := instance.PutObject(ctx, "bucket", "key", bytes.Repeat([]byte("o"), 20), PutOptions{}); err != nil {
		t.Fatalf("put original: %v", err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	part, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("12345"))
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	if _, err := instance.CompleteMultipartUpload(ctx, upload.UploadID, []storage.PartInfo{*part}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// Five bytes committed, not twenty-five, and the old twenty are gone.
	if usage := instance.Usage(); usage.Bytes != 5 || usage.Objects != 1 || instance.reservedBytes != 0 {
		t.Fatalf("usage = %+v, reserved = %d", usage, instance.reservedBytes)
	}
	head, err := instance.HeadObject(ctx, "bucket", "key")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Size != 5 {
		t.Fatalf("published size = %d, want 5", head.Size)
	}
}

// Concurrent uploads share one budget, and the one that does not fit is the one
// refused.
//
// This is the property the reservation exists for, and it is only visible when
// two uploads are in flight at once: a budget large enough for either alone and
// too small for both. The accounting has to be against the shared total, not
// against one upload's own parts.
func TestConcurrentUploadsShareOneByteBudget(t *testing.T) {
	ctx := context.Background()
	// Room for two 5-byte parts and no more. Three object slots, because the
	// third upload has to be initiated to be refused: an upload for a key with no
	// object claims a slot at initiation, and two in-flight uploads have claimed
	// two.
	instance := boundedInstance(t, 10, 3)
	if err := instance.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	const uploads, partSize = 2, 5
	var wg sync.WaitGroup
	uploadIDs := make([]string, uploads)
	results := make([]error, uploads)
	for i := range uploads {
		upload, err := instance.CreateMultipartUpload(ctx, "bucket", "key-"+string(rune('a'+i)), storage.MultipartOptions{})
		if err != nil {
			t.Fatalf("create upload %d: %v", i, err)
		}
		uploadIDs[i] = upload.UploadID
	}
	for i := range uploads {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, results[index] = instance.UploadPart(ctx, uploadIDs[index], 1, strings.NewReader(strings.Repeat("x", partSize)))
		}(i)
	}
	wg.Wait()

	refused, accepted := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrQuotaExceeded):
			refused++
		default:
			t.Fatalf("upload part error = %v, want nil or ErrQuotaExceeded", err)
		}
	}
	if accepted != uploads {
		t.Fatalf("accepted %d of %d five-byte parts against a ten-byte budget", accepted, uploads)
	}
	if refused != 0 {
		t.Fatalf("%d parts refused against a budget that fits them all", refused)
	}
	if instance.reservedBytes != uploads*partSize {
		t.Fatalf("reserved = %d, want %d", instance.reservedBytes, uploads*partSize)
	}

	// A third does not fit, and is refused rather than allowed to displace the
	// two that are already there.
	third, err := instance.CreateMultipartUpload(ctx, "bucket", "key-c", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create third upload: %v", err)
	}
	if _, err := instance.UploadPart(ctx, third.UploadID, 1, strings.NewReader("12345")); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("a part past the shared budget = %v, want ErrQuotaExceeded", err)
	}
	if instance.reservedBytes != uploads*partSize {
		t.Fatalf("a refused part changed the reservation to %d", instance.reservedBytes)
	}
}
