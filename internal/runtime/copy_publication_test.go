package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type adapterCopyFixture struct {
	instance *Instance
	adapter  *StoreAdapter
	root     string
	source   Object
}

func openAdapterCopyFixture(t *testing.T, overwrite bool) *adapterCopyFixture {
	t.Helper()
	f := &adapterCopyFixture{root: t.TempDir()}
	f.reopen(t)
	t.Cleanup(func() { _ = f.instance.Close() })
	var err error
	f.source, err = f.instance.PutObject(context.Background(), "probe", "source", []byte("body"), PutOptions{
		ContentType: "text/plain", Metadata: map[string]string{"state": "original"},
		computeChecksum: true, ChecksumAlgorithm: "CRC32",
	})
	if err != nil {
		t.Fatal(err)
	}
	if overwrite {
		if _, err := f.instance.PutObject(context.Background(), "probe", "target", []byte("old"), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *adapterCopyFixture) reopen(t *testing.T) {
	t.Helper()
	store, err := workspace.New(workspace.Options{Root: f.root, Bucket: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	f.instance, err = OpenWithStore(Options{Backend: BackendWorkspace, MaxBytes: 8, MaxObjects: 2}, store, nil)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	f.adapter, err = NewStoreAdapter(f.instance)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *adapterCopyFixture) blockIndex(t *testing.T) func() {
	t.Helper()
	index := filepath.Join(f.root, ".stow", "index.json")
	if err := os.Rename(index, index+".saved"); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	repair := func() { once.Do(func() { _ = os.Remove(index); _ = os.Rename(index+".saved", index) }) }
	t.Cleanup(repair)
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	return repair
}

func (f *adapterCopyFixture) request(mode string) storage.CopyRequest {
	req := storage.CopyRequest{SourceBucket: "probe", SourceKey: "source", DestBucket: "probe", DestKey: "target"}
	switch mode {
	case "etag":
		req.Options.SourceIfMatch = f.source.ETag
	case "date":
		req.Options.SourceIfUnmodifiedSince = f.source.LastModified.Add(time.Hour).Format(http.TimeFormat)
	}
	return req
}

func (f *adapterCopyFixture) copy(ctx context.Context, mode string, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	if mode == "plain" {
		return f.adapter.CopyObject(ctx, req.SourceBucket, req.SourceKey, req.DestBucket, req.DestKey)
	}
	return f.adapter.CopyObjectCond(ctx, req)
}

func TestStoreAdapterCopyPreservesCommittedPublication(t *testing.T) {
	for _, mode := range []string{"plain", "etag", "date"} {
		for _, overwrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/overwrite=%t", mode, overwrite), func(t *testing.T) {
				f := openAdapterCopyFixture(t, overwrite)
				repair := f.blockIndex(t)
				meta, err := f.copy(context.Background(), mode, f.request(mode))
				if !errors.Is(err, storage.ErrMutationCommitted) {
					t.Fatalf("completion error=%v, want committed marker", err)
				}
				assertAdapterCopyPublication(t, f, meta)
				repair()
				if err := f.instance.Close(); err != nil {
					t.Fatal(err)
				}
				f.reopen(t)
				assertAdapterCopyPublication(t, f, meta)
				retried, err := f.copy(context.Background(), mode, f.request(mode))
				if err != nil || retried == nil {
					t.Fatalf("copy after repair meta=%+v error=%v", retried, err)
				}
				assertAdapterCopyPublication(t, f, retried)
			})
		}
	}
}

func assertAdapterCopyPublication(t *testing.T, f *adapterCopyFixture, meta *storage.ObjectMeta) {
	t.Helper()
	ctx := context.Background()
	object, err := f.instance.GetObject(ctx, "probe", "target")
	if err != nil || string(object.Data) != "body" {
		t.Fatalf("published payload=%q error=%v", object.Data, err)
	}
	if meta == nil || !reflect.DeepEqual(meta, objectMeta(&object)) {
		t.Fatalf("published identity lost: result=%+v stored=%+v usage=%+v", meta, objectMeta(&object), f.instance.Usage())
	}
	if meta.VersionID == "" || meta.ChecksumAlgorithm != "CRC32" || meta.ChecksumValue == "" || meta.Metadata["state"] != "original" {
		t.Fatalf("publication properties missing: %+v", meta)
	}
	if got := f.instance.Usage(); got != (Usage{Bytes: 8, Objects: 2}) {
		t.Fatalf("committed usage=%+v", got)
	}
	if _, err := f.instance.PutObject(ctx, "probe", "target", []byte("12345"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("published bytes escaped quota: %v", err)
	}
	meta.Metadata["state"] = "caller"
	head, err := f.instance.HeadObject(ctx, "probe", "target")
	if err != nil || head.Metadata["state"] != "original" {
		t.Fatalf("returned metadata aliases storage: %+v %v", head, err)
	}
	meta.Metadata["state"] = "original"
}

func TestStoreAdapterCopyRefusalKeepsDestination(t *testing.T) {
	for _, mode := range []string{"plain", "etag", "date"} {
		t.Run(mode, func(t *testing.T) {
			f := openAdapterCopyFixture(t, true)
			req := f.request(mode)
			want := storage.ErrPreconditionFailed
			switch mode {
			case "plain":
				req.SourceKey, want = "missing", storage.ErrObjectNotFound
			case "etag":
				req.Options.SourceIfMatch = "mismatch"
			case "date":
				req.Options.SourceIfUnmodifiedSince = f.source.LastModified.Add(-time.Hour).Format(http.TimeFormat)
			}
			meta, err := f.copy(context.Background(), mode, req)
			if meta != nil || !errors.Is(err, want) {
				t.Fatalf("refusal meta=%+v error=%v, want %v", meta, err, want)
			}
			object, err := f.instance.GetObject(context.Background(), "probe", "target")
			if err != nil || string(object.Data) != "old" || f.instance.Usage() != (Usage{Bytes: 7, Objects: 2}) {
				t.Fatalf("refusal changed destination: %+v usage=%+v error=%v", object, f.instance.Usage(), err)
			}
		})
	}
}

type adapterCopyOutcomeStore struct {
	*storage.MemoryStore
	failure error
}

func (s *adapterCopyOutcomeStore) CopyObject(ctx context.Context, sb, sk, db, dk string) (*storage.ObjectMeta, error) {
	return s.CopyObjectCond(ctx, storage.CopyRequest{SourceBucket: sb, SourceKey: sk, DestBucket: db, DestKey: dk})
}

func (s *adapterCopyOutcomeStore) CopyObjectCond(ctx context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	meta, err := s.MemoryStore.CopyObjectCond(ctx, req)
	if err != nil {
		return meta, err
	}
	return meta, s.failure
}

func BenchmarkStoreAdapterCopyPublication(b *testing.B) {
	for _, mode := range []string{"plain", "etag"} {
		for _, committed := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/committed=%t", mode, committed), func(b *testing.B) {
				store := &adapterCopyOutcomeStore{MemoryStore: storage.NewMemoryStore()}
				if committed {
					store.failure = storage.CommittedError(errors.New("after local copy"))
				}
				instance, err := OpenWithStore(Options{MaxBytes: 4 << 20}, store, nil)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = instance.Close() })
				ctx := context.Background()
				if err := instance.CreateBucket(ctx, "probe"); err != nil {
					b.Fatal(err)
				}
				source, err := instance.PutObject(ctx, "probe", "source", bytes.Repeat([]byte("x"), 1<<20), PutOptions{Metadata: map[string]string{"state": "original"}})
				if err != nil {
					b.Fatal(err)
				}
				adapter, err := NewStoreAdapter(instance)
				if err != nil {
					b.Fatal(err)
				}
				f := &adapterCopyFixture{adapter: adapter, source: source}
				req := f.request(mode)
				b.ReportAllocs()
				b.SetBytes(1 << 20)
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					_, err := f.copy(ctx, mode, req)
					if !errors.Is(err, store.failure) {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
