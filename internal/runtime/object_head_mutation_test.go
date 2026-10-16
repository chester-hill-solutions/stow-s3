package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type observationHeadStore struct {
	*storage.MemoryStore
	headErr                 error
	faultAt, heads, deletes int
}

func (s *observationHeadStore) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if key == "target" {
		s.heads++
		if s.headErr != nil && s.heads >= s.faultAt {
			return nil, s.headErr
		}
	}
	return s.MemoryStore.HeadObject(ctx, bucket, key)
}

func (s *observationHeadStore) DeleteObject(ctx context.Context, bucket, key string) error {
	s.deletes++
	return s.MemoryStore.DeleteObject(ctx, bucket, key)
}

func (s *observationHeadStore) DeleteObjects(ctx context.Context, bucket string, keys []string) ([]string, error) {
	s.deletes++
	return s.MemoryStore.DeleteObjects(ctx, bucket, keys)
}

type observationBufferedStore struct {
	storage.Store
	storage.MultipartStore
}
type observationQuotaStore struct {
	*storage.MemoryStore
	quota storage.Store
}

func (s *observationQuotaStore) QuotaStore() storage.Store { return s.quota }

type objectHeadFixture struct {
	store             *observationHeadStore
	instance          *Instance
	operation, upload string
	part              *storage.PartInfo
	before            map[string]Object
	usage             Usage
	revision          uint64
}

func newObjectHeadFixture(t *testing.T, operation string) *objectHeadFixture {
	t.Helper()
	ctx := context.Background()
	backing := &observationHeadStore{MemoryStore: storage.NewMemoryStore(), faultAt: 1}
	var store storage.Store = backing
	if operation == "buffered-put" {
		store = &observationBufferedStore{Store: backing, MultipartStore: backing}
	}
	if strings.HasPrefix(operation, "quota-") {
		store = &observationQuotaStore{MemoryStore: backing.MemoryStore, quota: backing}
	}
	instance, err := OpenWithStore(Options{Backend: BackendMemory, MaxBytes: 16, MaxObjects: 3, MaxMultipartUploads: 1}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	f := &objectHeadFixture{store: backing, instance: instance, operation: operation, before: make(map[string]Object)}
	for _, key := range []string{"source", "target"} {
		object, err := instance.PutObject(ctx, "probe", key, []byte("old"), PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": key}})
		if err != nil {
			t.Fatal(err)
		}
		f.before[key] = object
	}
	if operation == "complete" || operation == "abort" {
		f.upload = mustCreateUpload(t, ctx, instance, "probe", "target")
		f.part = mustUploadPart(t, ctx, instance, f.upload, []byte("part"))
	}
	f.usage, f.revision = instance.Usage(), instance.usageRevision
	backing.heads = 0
	if operation == "staged-recheck" {
		backing.faultAt = 2
	}
	return f
}

func (f *objectHeadFixture) mutate() error {
	ctx := context.Background()
	switch f.operation {
	case "delete", "quota-delete":
		return f.instance.DeleteObject(ctx, "probe", "target")
	case "batch-delete", "quota-batch":
		_, err := f.instance.DeleteObjects(ctx, "probe", []string{"source", "target"})
		return err
	case "buffered-put", "staged-put", "staged-recheck":
		_, err := f.instance.PutObjectStream(ctx, "probe", "target", strings.NewReader("replaced"), StreamPutOptions{Size: 8})
		return err
	case "copy":
		_, err := f.instance.CopyObject(ctx, "probe", "source", "probe", "target")
		return err
	case "initiate":
		_, err := f.instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
		return err
	case "complete":
		_, err := f.instance.CompleteMultipartUpload(ctx, f.upload, []storage.PartInfo{*f.part})
		return err
	case "abort":
		return f.instance.AbortMultipartUpload(ctx, f.upload)
	default:
		return fmt.Errorf("unknown operation: %s", f.operation)
	}
}

func TestObjectHeadAmbiguityStopsMutation(t *testing.T) {
	for _, operation := range []string{"delete", "batch-delete", "quota-delete", "quota-batch", "buffered-put", "staged-put", "staged-recheck", "copy", "initiate", "complete", "abort"} {
		for _, order := range []string{"marker-first", "missing-first", "wrapped-marker-first", "wrapped-missing-first"} {
			t.Run(fmt.Sprintf("%s/%s", operation, order), func(t *testing.T) {
				f := newObjectHeadFixture(t, operation)
				f.store.headErr = objectHeadAmbiguity(order)
				if err := f.mutate(); err != f.store.headErr {
					t.Errorf("observation error lost: %v want %v", err, f.store.headErr)
				}
				assertObjectHeadState(t, f)
				recoverObjectHead(t, f)
			})
		}
	}
}

func assertObjectHeadState(t *testing.T, f *objectHeadFixture) {
	t.Helper()
	for key, before := range f.before {
		reader, meta, err := f.store.MemoryStore.GetObject(context.Background(), "probe", key)
		if err != nil {
			t.Errorf("%s changed after unknown HEAD: %v", key, err)
			continue
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || string(data) != "old" || meta.ETag != before.ETag || meta.VersionID != before.VersionID || meta.Metadata["state"] != key {
			t.Errorf("%s changed: meta=%+v data=%s error=%v", key, meta, data, err)
		}
	}
	assertObjectHeadLedger(t, f)
	if f.upload != "" {
		parts, err := f.store.ListParts(context.Background(), f.upload)
		if err != nil || len(parts) != 1 || parts[0].ETag != f.part.ETag {
			t.Errorf("retained upload changed: parts=%+v error=%v", parts, err)
		}
	}
	if f.store.deletes != 0 {
		t.Errorf("deletion called before reliable HEAD: %d", f.store.deletes)
	}
	if f.store.heads != f.store.faultAt {
		t.Errorf("target HEAD calls=%d want %d", f.store.heads, f.store.faultAt)
	}
}

func assertObjectHeadLedger(t *testing.T, f *objectHeadFixture) {
	t.Helper()
	sessions, bytes := 0, int64(0)
	if f.upload != "" {
		sessions, bytes = 1, 4
	}
	i := f.instance
	if i.Usage() != f.usage || i.usageRevision != f.revision || i.reservedBytes != bytes || i.reservedObjects != 0 || len(i.multipart) != sessions || i.staged != (streamReservation{}) {
		t.Errorf("ledger changed: usage=%+v revision=%d reserved=%d/%d sessions=%d staged=%+v", i.Usage(), i.usageRevision, i.reservedBytes, i.reservedObjects, len(i.multipart), i.staged)
	}
}

func recoverObjectHead(t *testing.T, f *objectHeadFixture) {
	t.Helper()
	ctx := context.Background()
	f.store.headErr = nil
	if err := f.mutate(); err != nil {
		t.Errorf("healthy retry: %v", err)
		return
	}
	uploads, err := f.store.ListMultipartUploads(ctx, "probe", storage.MultipartListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, upload := range uploads.Uploads {
		if err := f.instance.AbortMultipartUpload(ctx, upload.UploadID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.instance.DeleteObjects(ctx, "probe", []string{"source", "target"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.instance.PutObject(ctx, "probe", "capacity", []byte("1234567890123456"), PutOptions{}); err != nil {
		t.Errorf("capacity stranded: %v usage=%+v", err, f.instance.Usage())
	}
}

func TestObjectHeadConfirmedMissingBatch(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprint(wrapped), func(t *testing.T) {
			f := newObjectHeadFixture(t, "batch-delete")
			ctx := context.Background()
			if err := f.instance.DeleteObject(ctx, "probe", "target"); err != nil {
				t.Fatal(err)
			}
			f.store.headErr = storage.ErrObjectNotFound
			if wrapped {
				f.store.headErr = fmt.Errorf("head: %w", f.store.headErr)
			}
			keys := []string{"missing", "source", "target", "source"}
			deleted, err := f.instance.DeleteObjects(ctx, "probe", keys)
			if err != nil || strings.Join(deleted, ",") != strings.Join(keys, ",") || f.instance.Usage() != (Usage{}) {
				t.Fatalf("confirmed batch: deleted=%v error=%v usage=%+v", deleted, err, f.instance.Usage())
			}
			f.store.headErr = nil
			if _, err := f.instance.PutObject(ctx, "probe", "capacity", []byte("1234567890123456"), PutOptions{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
