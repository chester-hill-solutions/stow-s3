package stow_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type initiationPublicationStore struct {
	*partPublicationStore
	initiationErr    error
	refuseInitiation bool
	initiationCalls  int
	published        stow.MultipartUpload
}

func (s *initiationPublicationStore) CreateMultipartUpload(ctx context.Context, bucket, key string, opts stow.MultipartOptions) (stow.MultipartUpload, error) {
	s.initiationCalls++
	if s.refuseInitiation {
		return stow.MultipartUpload{UploadID: "not-created"}, s.initiationErr
	}
	upload, err := s.partPublicationStore.CreateMultipartUpload(ctx, bucket, key, opts)
	if err != nil {
		return upload, err
	}
	s.multipartStore.mu.Lock()
	delete(s.uploads, upload.UploadID)
	delete(s.parts, upload.UploadID)
	upload.UploadID = fmt.Sprintf("init-%d", s.initiationCalls)
	s.uploads[upload.UploadID] = upload
	s.parts[upload.UploadID] = nil
	s.multipartStore.mu.Unlock()
	s.published = upload
	return upload, s.initiationErr
}

type initiationFixture struct {
	store  *initiationPublicationStore
	outer  *stow.Runtime
	inner  *stowruntime.Instance
	exists bool
}

func newInitiationFixture(t testing.TB, exists bool) *initiationFixture {
	t.Helper()
	store := &initiationPublicationStore{partPublicationStore: &partPublicationStore{singlePartPublicationStore: newSinglePartPublicationStore()}}
	outer, err := stow.Open(stow.Options{Store: store, MaxBytes: 8, MaxObjects: 1, MaxMultipartUploads: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Close() })
	if err := outer.CreateBucket(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	if exists {
		if _, err := outer.PutObject(context.Background(), "probe", "target", []byte("old"), stow.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return &initiationFixture{store: store, outer: outer, inner: embedded.UnwrapInstance(outer), exists: exists}
}
