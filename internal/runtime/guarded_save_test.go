package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type guardedPostEffectStore struct {
	*storage.MemoryStore
	failure error
}

func (s *guardedPostEffectStore) PutObject(ctx context.Context, bucket, key string, body io.Reader, options storage.PutOptions) (*storage.ObjectMeta, error) {
	meta, err := s.MemoryStore.PutObject(ctx, bucket, key, body, options)
	if err != nil {
		return meta, err
	}
	return meta, s.failure
}

func TestGuardedSaveUnexpectedErrorDoesNotClaimNoEffect(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "committed"}[committed], func(t *testing.T) {
			failure := errors.New("reply lost")
			if committed {
				failure = storage.CommittedError(failure)
			}
			store := &guardedPostEffectStore{MemoryStore: storage.NewMemoryStore(), failure: failure}
			instance, err := OpenWithStore(Options{}, store, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			ctx := context.Background()
			if err := instance.CreateBucket(ctx, "guarded"); err != nil {
				t.Fatal(err)
			}
			_, condition, err := instance.ReadForSave(ctx, "guarded", "new")
			if err != nil {
				t.Fatal(err)
			}
			result, err := instance.SaveObject(ctx, "guarded", "new", []byte("saved"), SaveOptions{Condition: condition})
			want := SaveUnknown
			if committed {
				want = SaveCommitted
			}
			if !errors.Is(err, failure) || result.Outcome != want {
				t.Fatalf("result=%+v, err=%v", result, err)
			}
			object, err := instance.GetObject(ctx, "guarded", "new")
			if err != nil || string(object.Data) != "saved" {
				t.Fatalf("persisted effect=%+v, %v", object, err)
			}
		})
	}
}

func TestGuardedSavePreservesMultipartReservation(t *testing.T) {
	ctx := context.Background()
	instance, err := Open(Options{MaxBytes: 20, MaxObjects: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	upload, err := instance.CreateMultipartUpload(ctx, "guarded", "same", storage.MultipartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.UploadPart(ctx, upload.UploadID, 1, bytes.NewBufferString("part")); err != nil {
		t.Fatal(err)
	}
	_, elsewhere, _ := instance.ReadForSave(ctx, "guarded", "elsewhere")
	if result, err := instance.SaveObject(ctx, "guarded", "elsewhere", []byte("x"), SaveOptions{Condition: elsewhere}); !errors.Is(err, ErrQuotaExceeded) || result.Outcome != SaveNotCommitted {
		t.Fatalf("reserved slot=%+v, %v", result, err)
	}
	_, same, _ := instance.ReadForSave(ctx, "guarded", "same")
	if result, err := instance.SaveObject(ctx, "guarded", "same", []byte("saved"), SaveOptions{Condition: same}); err != nil || result.Outcome != SaveCommitted {
		t.Fatalf("same target save=%+v, %v", result, err)
	}
	if instance.Usage() != (Usage{Objects: 1, Bytes: 5}) || instance.reservedObjects != 0 || instance.reservedBytes != 4 {
		t.Fatalf("usage=%+v, reserved=%d/%d", instance.Usage(), instance.reservedObjects, instance.reservedBytes)
	}
}

func TestReadForSaveRequiresReadAuthority(t *testing.T) {
	granted := authority.None()
	instance, err := Open(Options{Authority: &granted})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	_, condition, err := instance.ReadForSave(context.Background(), "guarded", "secret")
	var denied *authority.ErrNotAuthorized
	if !errors.As(err, &denied) || condition.ObservedAbsence() {
		t.Fatalf("read=%v, condition=%+v", err, condition)
	}
}
