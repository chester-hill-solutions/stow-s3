package s3api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// staleHeadStore reports an older LastModified from HeadObject than the version
// it copies, standing in for a source that changed between the head and the copy.
// A date-only condition has to be evaluated by the store on the version it
// copies; evaluating it on the head lets the newer version through.
type staleHeadStore struct {
	storage.Store
}

func (s *staleHeadStore) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	meta, err := s.Store.HeadObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	meta.LastModified = meta.LastModified.Add(-time.Hour)
	return meta, nil
}

func (s *staleHeadStore) CopyObjectCond(ctx context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	conditional, ok := s.Store.(storage.ConditionalCopyStore)
	if !ok {
		return nil, fmt.Errorf("staleHeadStore wraps a store without conditional copy")
	}
	return conditional.CopyObjectCond(ctx, req)
}

// A date-only copy condition must be answered about the version that is copied.
//
// The wrapper's head is an hour stale, so a condition that the real version
// violates passes the weaker head-then-copy sequence. Routing the condition to
// the store is what makes the refusal happen, and it is the only thing that
// distinguishes the two paths.
func TestCopySourceDateConditionIsEvaluatedOnTheCopiedVersion(t *testing.T) {
	ctx := context.Background()
	memory := storage.NewMemoryStore()
	if err := memory.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	stored, err := memory.PutObject(ctx, "bucket", "source", strings.NewReader("source"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	store := &staleHeadStore{Store: memory}
	srv, err := New(Config{Store: store, Auth: DevBypass})
	if err != nil {
		t.Fatal(err)
	}

	// The condition is satisfied by the stale head and violated by the real
	// version, which was modified thirty minutes after the condition.
	refused := httptest.NewRequest("PUT", "/bucket/refused", nil)
	refused.Header.Set("X-Amz-Copy-Source", "/bucket/source")
	refused.Header.Set("X-Amz-Copy-Source-If-Unmodified-Since", stored.LastModified.Add(-30*time.Minute).Format(http.TimeFormat))
	refusedResponse := httptest.NewRecorder()
	srv.ServeHTTP(refusedResponse, refused)
	if refusedResponse.Code != http.StatusPreconditionFailed {
		t.Fatalf("date-only copy condition ignored: %d %s", refusedResponse.Code, refusedResponse.Body.String())
	}
	if _, err := memory.HeadObject(ctx, "bucket", "refused"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("refused copy published a destination: %v", err)
	}

	// A condition the real version satisfies still copies.
	allowed := httptest.NewRequest("PUT", "/bucket/allowed", nil)
	allowed.Header.Set("X-Amz-Copy-Source", "/bucket/source")
	allowed.Header.Set("X-Amz-Copy-Source-If-Unmodified-Since", stored.LastModified.Add(30*time.Minute).Format(http.TimeFormat))
	allowedResponse := httptest.NewRecorder()
	srv.ServeHTTP(allowedResponse, allowed)
	if allowedResponse.Code != http.StatusOK {
		t.Fatalf("satisfied date-only condition refused: %d %s", allowedResponse.Code, allowedResponse.Body.String())
	}
	copied, err := memory.HeadObject(ctx, "bucket", "allowed")
	if err != nil || copied.Size != stored.Size {
		t.Fatalf("allowed copy: %v %v", copied, err)
	}
}
