package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type pausedGuardReader struct {
	started chan struct{}
	resume  chan struct{}
	body    io.Reader
	paused  bool
}

func (r *pausedGuardReader) Read(p []byte) (int, error) {
	if !r.paused {
		r.paused = true
		close(r.started)
		<-r.resume
	}
	return r.body.Read(p)
}

func TestGuardedPutComparesAfterConcurrentStoreMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if err := store.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	original, err := store.PutObject(ctx, "guarded", "report", bytes.NewBufferString("A"), PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	guard := WriteGuard{Fingerprint: ObjectFingerprint(*original, []byte("A"))}
	reader := &pausedGuardReader{started: make(chan struct{}), resume: make(chan struct{}), body: bytes.NewBufferString("stale")}
	done := make(chan error, 1)
	go func() {
		_, err := store.PutObject(ctx, "guarded", "report", reader, PutOptions{Guard: &guard})
		done <- err
	}()
	<-reader.started
	updated, err := store.PutObject(ctx, "guarded", "report", bytes.NewBufferString("A"), PutOptions{Metadata: map[string]string{"state": "updated"}})
	close(reader.resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrSaveConflict) {
		t.Fatalf("stale save=%v", err)
	}
	current, err := store.HeadObject(ctx, "guarded", "report")
	if err != nil || current.VersionID != updated.VersionID || current.Metadata["state"] != "updated" {
		t.Fatalf("current=%+v, %v", current, err)
	}
}
