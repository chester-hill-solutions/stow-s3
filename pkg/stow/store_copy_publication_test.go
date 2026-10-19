package stow_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// Embed only Store so the runtime exercises its base-store COPY fallback.
type copyPublicationStore struct {
	stow.Store
	writeErr error
	refuse   bool
	gets     int
	puts     int
	copies   int
}

func (s *copyPublicationStore) GetObject(ctx context.Context, bucket, key string) (stow.Object, error) {
	s.gets++
	return s.Store.GetObject(ctx, bucket, key)
}

func (s *copyPublicationStore) PutObject(ctx context.Context, bucket, key string, data []byte, opts stow.PutOptions) (stow.Object, error) {
	s.puts++
	if s.refuse {
		return stow.Object{}, s.writeErr
	}
	object, err := s.Store.PutObject(ctx, bucket, key, data, opts)
	if err != nil {
		return object, err
	}
	return object, s.writeErr
}

type optionalCopyPublicationStore struct{ *copyPublicationStore }

func (s optionalCopyPublicationStore) CopyObject(ctx context.Context, sb, sk, db, dk string) (stow.Object, error) {
	s.copies++
	if s.refuse {
		return stow.Object{}, s.writeErr
	}
	object, err := s.Store.(stow.CopyStore).CopyObject(ctx, sb, sk, db, dk)
	if err != nil {
		return object, err
	}
	return object, s.writeErr
}

func TestCustomStoreCopyPublication(t *testing.T) {
	for _, optional := range []bool{false, true} {
		for _, overwrite := range []bool{false, true} {
			for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
				t.Run(fmt.Sprintf("optional=%t/overwrite=%t/%s", optional, overwrite, outcome), func(t *testing.T) {
					testCustomStoreCopyPublication(t, optional, overwrite, outcome)
				})
			}
		}
	}
}

func testCustomStoreCopyPublication(t *testing.T, optional, overwrite bool, outcome string) {
	t.Helper()
	ctx := context.Background()
	base, err := stow.Open(stow.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	if err := base.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.PutObject(ctx, "probe", "source", []byte("body"), stow.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "copied"}}); err != nil {
		t.Fatal(err)
	}
	if overwrite {
		if _, err := base.PutObject(ctx, "probe", "target", []byte("old"), stow.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	store := &copyPublicationStore{Store: base}
	cause := errors.New("post-publication failure")
	switch outcome {
	case "committed":
		store.writeErr = errors.Join(stow.ErrMutationCommitted, cause)
	case "committed-missing":
		cause = fmt.Errorf("propagation: %w", stow.ErrObjectNotFound)
		store.writeErr = errors.Join(stow.ErrMutationCommitted, cause)
	case "refused":
		store.writeErr, store.refuse = stow.ErrPreconditionFailed, true
	}
	var supplied stow.Store = store
	if optional {
		supplied = optionalCopyPublicationStore{store}
	}
	outer, err := stow.Open(stow.Options{Store: supplied, MaxBytes: 8, MaxObjects: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Close() })
	object, copyErr := copyPublicationWithoutPanic(t, outer)
	wantUsage := stow.Usage{Bytes: 8, Objects: 2}
	if outcome == "refused" {
		wantUsage = assertRefusedCustomCopy(t, base, object, copyErr, overwrite)
	} else {
		assertPublishedCustomCopy(t, base, object)
		assertCustomCopyError(t, copyErr, outcome, cause)
	}
	if usage := outer.Usage(); usage != wantUsage {
		t.Errorf("usage=%+v, want %+v", usage, wantUsage)
	}
	assertCustomCopyCalls(t, store, optional)
	if outcome == "refused" {
		return
	}
	store.writeErr = nil
	assertCustomCopyQuota(t, outer)
}

func assertRefusedCustomCopy(t *testing.T, base *stow.Runtime, object stow.Object, copyErr error, overwrite bool) stow.Usage {
	t.Helper()
	stored, readErr := base.HeadObject(context.Background(), "probe", "target")
	if !errors.Is(copyErr, stow.ErrPreconditionFailed) || !reflect.DeepEqual(object, stow.Object{}) {
		t.Fatalf("refusal result=%+v error=%v", object, copyErr)
	}
	if overwrite {
		if readErr != nil || stored.Size != 3 {
			t.Fatalf("refusal changed destination: %+v %v", stored, readErr)
		}
		return stow.Usage{Bytes: 7, Objects: 2}
	}
	if !errors.Is(readErr, stow.ErrObjectNotFound) {
		t.Fatalf("refused copy published destination: %+v %v", stored, readErr)
	}
	return stow.Usage{Bytes: 4, Objects: 1}
}

func assertPublishedCustomCopy(t *testing.T, base *stow.Runtime, object stow.Object) {
	t.Helper()
	stored, readErr := base.HeadObject(context.Background(), "probe", "target")
	if readErr != nil || stored.Size != 4 || stored.Metadata["state"] != "copied" || stored.ContentType != "text/plain" {
		t.Fatalf("published destination=%+v error=%v", stored, readErr)
	}
	payload, err := base.GetObject(context.Background(), "probe", "target")
	if err != nil || string(payload.Data) != "body" {
		t.Fatalf("published payload=%q error=%v", payload.Data, err)
	}
	stored.Data = nil
	if !reflect.DeepEqual(object, stored) {
		t.Errorf("committed identity=%+v, want %+v", object, stored)
	}
}

func assertCustomCopyError(t *testing.T, copyErr error, outcome string, cause error) {
	t.Helper()
	if outcome == "success" && copyErr != nil || outcome != "success" && (!errors.Is(copyErr, stow.ErrMutationCommitted) || !errors.Is(copyErr, cause)) {
		t.Errorf("publication error lost: %v", copyErr)
	}
}

func assertCustomCopyCalls(t *testing.T, store *copyPublicationStore, optional bool) {
	t.Helper()
	if optional && (store.copies != 1 || store.gets != 0 || store.puts != 0) || !optional && (store.copies != 0 || store.gets != 1 || store.puts != 1) {
		t.Fatalf("COPY changed storage calls: gets=%d puts=%d copies=%d", store.gets, store.puts, store.copies)
	}
}

func assertCustomCopyQuota(t *testing.T, outer *stow.Runtime) {
	t.Helper()
	ctx := context.Background()
	if _, err := outer.PutObject(ctx, "probe", "source", []byte("12345"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("published bytes escaped quota: %v", err)
	}
	if err := outer.DeleteObject(ctx, "probe", "target"); err != nil {
		t.Fatal(err)
	}
	if outer.Usage() != (stow.Usage{Bytes: 4, Objects: 1}) {
		t.Fatalf("deleting published object left stale usage: %+v", outer.Usage())
	}
	if _, err := outer.PutObject(ctx, "probe", "source", []byte("12345678"), stow.PutOptions{}); err != nil {
		t.Fatalf("released bytes still charged: %v", err)
	}
}

func copyPublicationWithoutPanic(t *testing.T, outer *stow.Runtime) (object stow.Object, err error) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Errorf("COPY panicked after publication: %v; usage=%+v", value, outer.Usage())
		}
	}()
	return outer.CopyObject(context.Background(), "probe", "source", "probe", "target")
}

func BenchmarkCustomStoreCopyPublication(b *testing.B) {
	for _, size := range []int{4 << 10, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			ctx := context.Background()
			base, err := stow.Open(stow.Options{MaxBytes: 4 << 20})
			if err != nil {
				b.Fatal(err)
			}
			if err := base.CreateBucket(ctx, "probe"); err != nil {
				b.Fatal(err)
			}
			if _, err := base.PutObject(ctx, "probe", "source", bytes.Repeat([]byte("x"), size), stow.PutOptions{}); err != nil {
				b.Fatal(err)
			}
			outer, err := stow.Open(stow.Options{Store: &copyPublicationStore{Store: base}, MaxBytes: 4 << 20})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = outer.Close() })
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for j := 0; j < b.N; j++ {
				if _, err := outer.CopyObject(ctx, "probe", "source", "probe", "target"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
