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

func TestCustomStoreBufferedPutPublication(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		for _, outcome := range []string{"success", "committed", "committed-missing", "refused"} {
			t.Run(fmt.Sprintf("overwrite=%t/%s", overwrite, outcome), func(t *testing.T) {
				checkBufferedPutPublication(t, overwrite, outcome)
			})
		}
	}
}

func openBufferedPutFixture(t *testing.T, overwrite bool) (*stow.Runtime, *stow.Runtime, *copyPublicationStore) {
	t.Helper()
	base, err := stow.Open(stow.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	if err := base.CreateBucket(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	if overwrite {
		if _, err := base.PutObject(context.Background(), "probe", "target", []byte("old"), stow.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	store := &copyPublicationStore{Store: base}
	outer, err := stow.Open(stow.Options{Store: store, MaxBytes: 4, MaxObjects: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Close() })
	return outer, base, store
}

func checkBufferedPutPublication(t *testing.T, overwrite bool, outcome string) {
	t.Helper()
	outer, base, store := openBufferedPutFixture(t, overwrite)
	cause := errors.New("after local PUT")
	switch outcome {
	case "committed":
		store.writeErr = errors.Join(stow.ErrMutationCommitted, cause)
	case "committed-missing":
		cause = fmt.Errorf("propagation: %w", stow.ErrObjectNotFound)
		store.writeErr = errors.Join(stow.ErrMutationCommitted, cause)
	case "refused":
		store.writeErr, store.refuse = stow.ErrPreconditionFailed, true
	}
	result, err := outer.PutObject(context.Background(), "probe", "target", []byte("body"), stow.PutOptions{
		ContentType: "text/plain", Metadata: map[string]string{"state": "published"},
	})
	if store.puts != 1 || store.gets != 0 || store.copies != 0 {
		t.Fatalf("PUT changed storage calls: puts=%d gets=%d copies=%d", store.puts, store.gets, store.copies)
	}
	if outcome == "refused" {
		assertBufferedPutRefusal(t, base, result, err, overwrite)
		want := stow.Usage{}
		if overwrite {
			want = stow.Usage{Bytes: 3, Objects: 1}
		}
		if outer.Usage() != want {
			t.Fatalf("refusal changed usage: %+v, want %+v", outer.Usage(), want)
		}
		return
	}
	assertBufferedPutError(t, err, outcome, cause)
	assertBufferedPutResult(t, outer, base, result)
	store.writeErr = nil
	assertBufferedPutQuota(t, outer, store)
}

func assertBufferedPutError(t *testing.T, err error, outcome string, cause error) {
	t.Helper()
	if outcome == "success" && err != nil || outcome != "success" && (!errors.Is(err, stow.ErrMutationCommitted) || !errors.Is(err, cause)) {
		t.Errorf("publication error lost: %v", err)
	}
}

func assertBufferedPutQuota(t *testing.T, outer *stow.Runtime, store *copyPublicationStore) {
	t.Helper()
	if _, err := outer.PutObject(context.Background(), "probe", "target", []byte("12345"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("published bytes escaped quota: %v", err)
	}
	if store.puts != 1 {
		t.Fatalf("over-quota write reached store: puts=%d", store.puts)
	}
	if err := outer.DeleteObject(context.Background(), "probe", "target"); err != nil {
		t.Fatal(err)
	}
	if _, err := outer.PutObject(context.Background(), "probe", "reuse", []byte("body"), stow.PutOptions{}); err != nil {
		t.Fatalf("deletion did not release quota: %v", err)
	}
}

func assertBufferedPutResult(t *testing.T, outer, base *stow.Runtime, result stow.Object) {
	t.Helper()
	ctx := context.Background()
	stored, err := base.GetObject(ctx, "probe", "target")
	if err != nil || string(stored.Data) != "body" || stored.Metadata["state"] != "published" || stored.ContentType != "text/plain" {
		t.Fatalf("published destination=%+v error=%v", stored, err)
	}
	stored.Data = nil
	if !reflect.DeepEqual(result, stored) {
		t.Fatalf("committed identity missing: result=%+v actual=%+v usage=%+v", result, stored, outer.Usage())
	}
	if outer.Usage() != (stow.Usage{Bytes: 4, Objects: 1}) {
		t.Fatalf("committed usage=%+v", outer.Usage())
	}
	result.Metadata["state"] = "caller"
	head, err := base.HeadObject(ctx, "probe", "target")
	if err != nil || head.Metadata["state"] != "published" {
		t.Fatalf("returned metadata aliases storage: %+v %v", head, err)
	}
}

func assertBufferedPutRefusal(t *testing.T, base *stow.Runtime, result stow.Object, putErr error, overwrite bool) {
	t.Helper()
	if !errors.Is(putErr, stow.ErrPreconditionFailed) || !reflect.DeepEqual(result, stow.Object{}) {
		t.Fatalf("refusal result=%+v error=%v", result, putErr)
	}
	stored, err := base.GetObject(context.Background(), "probe", "target")
	if overwrite {
		if err != nil || string(stored.Data) != "old" {
			t.Fatalf("refusal changed existing destination: %+v %v", stored, err)
		}
	} else if !errors.Is(err, stow.ErrObjectNotFound) {
		t.Fatalf("refusal published destination: %+v %v", stored, err)
	}
}

func BenchmarkCustomStoreBufferedPutPublication(b *testing.B) {
	for _, size := range []int{4 << 10, 1 << 20} {
		for _, committed := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/committed=%t", size, committed), func(b *testing.B) {
				ctx := context.Background()
				base, err := stow.Open(stow.Options{MaxBytes: 4 << 20})
				if err != nil {
					b.Fatal(err)
				}
				if err := base.CreateBucket(ctx, "probe"); err != nil {
					b.Fatal(err)
				}
				store := &copyPublicationStore{Store: base}
				if committed {
					store.writeErr = errors.Join(stow.ErrMutationCommitted, errors.New("after local PUT"))
				}
				outer, err := stow.Open(stow.Options{Store: store, MaxBytes: 4 << 20})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = outer.Close() })
				body := bytes.Repeat([]byte("x"), size)
				opts := stow.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "published"}}
				b.ReportAllocs()
				b.SetBytes(int64(size))
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					_, err := outer.PutObject(ctx, "probe", "target", body, opts)
					if !errors.Is(err, store.writeErr) {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
