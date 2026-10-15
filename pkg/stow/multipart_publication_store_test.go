package stow_test

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type singlePartPublicationStore struct {
	*multipartStore
	body      []byte
	failure   error
	lookupErr error
	missing   error
	remove    bool
	refuse    bool
	completed bool
	getCalls  int
}

func newSinglePartPublicationStore() *singlePartPublicationStore {
	return &singlePartPublicationStore{multipartStore: newMultipartStore(), remove: true, missing: stow.ErrObjectNotFound}
}

func (s *singlePartPublicationStore) UploadPart(ctx context.Context, upload string, number int, data []byte) (stow.Part, error) {
	part, err := s.multipartStore.UploadPart(ctx, upload, number, data)
	if err != nil {
		return part, err
	}
	part.ETag = fmt.Sprintf("\"%x\"", md5.Sum(data))
	s.multipartStore.mu.Lock()
	s.parts[upload][len(s.parts[upload])-1] = part
	s.body = append([]byte(nil), data...)
	s.multipartStore.mu.Unlock()
	return part, nil
}

func (s *singlePartPublicationStore) CompleteMultipartUpload(ctx context.Context, upload string, parts []stow.Part) (stow.Object, error) {
	if s.refuse {
		return stow.Object{}, s.failure
	}
	if len(parts) != 1 || parts[0].PartNumber != 1 {
		return stow.Object{}, errors.New("fixture requires one part numbered one")
	}
	state, err := s.multipartStore.GetMultipartUpload(ctx, upload)
	if err != nil {
		return stow.Object{}, err
	}
	s.multipartStore.mu.Lock()
	data := append([]byte(nil), s.body...)
	s.multipartStore.mu.Unlock()
	object, err := s.memoryStore.PutObject(ctx, state.Bucket, state.Key, data, stow.PutOptions{ContentType: state.ContentType, Metadata: state.Metadata})
	if err != nil {
		return object, err
	}
	object.ETag = storage.CompletionETag([]string{parts[0].ETag})
	s.memoryStore.mu.Lock()
	s.memoryStore.objects[state.Bucket][state.Key] = object
	s.memoryStore.mu.Unlock()
	if s.remove {
		if err := s.AbortMultipartUpload(ctx, upload); err != nil {
			return object, err
		}
	}
	s.completed = true
	return object, s.failure
}

func (s *singlePartPublicationStore) GetMultipartUpload(ctx context.Context, upload string) (stow.MultipartUpload, error) {
	s.getCalls++
	if s.completed && s.lookupErr != nil {
		return stow.MultipartUpload{}, s.lookupErr
	}
	state, err := s.multipartStore.GetMultipartUpload(ctx, upload)
	if errors.Is(err, stow.ErrObjectNotFound) {
		return state, s.missing
	}
	return state, err
}

func (s *singlePartPublicationStore) AbortMultipartUpload(ctx context.Context, upload string) error {
	if err := s.multipartStore.AbortMultipartUpload(ctx, upload); err != nil {
		return err
	}
	s.multipartStore.mu.Lock()
	s.body = nil
	s.multipartStore.mu.Unlock()
	return nil
}

type customMultipartFixture struct {
	store     *singlePartPublicationStore
	outer     *stow.Runtime
	inner     *stowruntime.Instance
	upload    *storage.MultipartUpload
	part      *storage.PartInfo
	overwrite bool
}

func newCustomMultipartFixture(t *testing.T, overwrite bool) *customMultipartFixture {
	t.Helper()
	f := &customMultipartFixture{store: newSinglePartPublicationStore(), overwrite: overwrite}
	var err error
	f.outer, err = stow.Open(stow.Options{Store: f.store, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.outer.Close() })
	ctx := context.Background()
	if err := f.outer.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	if overwrite {
		if _, err := f.outer.PutObject(ctx, "probe", "target", []byte("old"), stow.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	f.inner = embedded.UnwrapInstance(f.outer)
	f.upload, err = f.inner.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "completed"}})
	if err != nil {
		t.Fatal(err)
	}
	f.part, err = f.inner.UploadPart(ctx, f.upload.UploadID, 1, storage.NewOwnedBody([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	f.store.getCalls = 0
	return f
}

func completeCustomMultipart(t *testing.T, f *customMultipartFixture) (meta *storage.ObjectMeta, err error) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			actual, readErr := f.store.GetObject(context.Background(), "probe", "target")
			t.Errorf("completion panicked after publication: %v; actual=%q error=%v usage=%+v", value, actual.Data, readErr, f.outer.Usage())
		}
	}()
	return f.inner.CompleteMultipartUpload(context.Background(), f.upload.UploadID, []storage.PartInfo{*f.part})
}

func assertCustomMultipartIdentity(t *testing.T, f *customMultipartFixture, meta *storage.ObjectMeta) {
	t.Helper()
	object, err := f.store.GetObject(context.Background(), "probe", "target")
	if err != nil || string(object.Data) != "body" || object.Metadata["state"] != "completed" || object.ContentType != "text/plain" {
		t.Fatalf("published object=%+v error=%v", object, err)
	}
	want := &storage.ObjectMeta{Bucket: object.Bucket, Key: object.Key, Size: object.Size, ETag: object.ETag, ContentType: object.ContentType, LastModified: object.LastModified, Metadata: object.Metadata}
	assertCustomMultipartMetadata(t, meta, want)
	if f.outer.Usage() != (stow.Usage{Bytes: 4, Objects: 1}) {
		t.Fatalf("published usage=%+v", f.outer.Usage())
	}
	meta.Metadata["state"] = "caller"
	stored, err := f.store.HeadObject(context.Background(), "probe", "target")
	if err != nil || stored.Metadata["state"] != "completed" {
		t.Fatalf("result aliases stored metadata: %+v %v", stored, err)
	}
	meta.Metadata["state"] = "completed"
}
