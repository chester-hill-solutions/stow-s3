package runthrough_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	filesystem "github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type recoveryHeadStore struct {
	storage.Store
	failure error
	heads   int
}

func (s *recoveryHeadStore) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	s.heads++
	if s.failure != nil {
		return nil, s.failure
	}
	return s.Store.HeadObject(ctx, bucket, key)
}

type recoveryCase struct{ backend, operation, entrypoint, fault string }
type recoveryFixture struct {
	local    *recoveryHeadStore
	upstream *mockUpstream
	outbox   *runthrough.FileOutbox
	adapter  *runthrough.Adapter
	path     string
	entry    runthrough.OutboxEntry
	version  string
	journal  []byte
}

func newRecoveryFixture(t *testing.T, item recoveryCase) *recoveryFixture {
	t.Helper()
	var raw storage.Store
	if item.backend == "filesystem" {
		var err error
		raw, err = filesystem.NewFilesystemStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
	}
	if raw == nil {
		raw = storage.NewMemoryStore()
	}
	ctx := context.Background()
	if err := raw.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	old, err := raw.PutObject(ctx, "probe", "target", strings.NewReader("old"), storage.PutOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	upstream := newMockUpstream()
	etag, err := upstream.PutObject(ctx, "probe", "target", strings.NewReader("old"), storage.PutOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	upstream.putCalls = 0
	path := filepath.Join(t.TempDir(), "outbox.json")
	outbox, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	operation := runthrough.OutboxOperation(item.operation)
	if item.operation == "multipart" {
		operation = runthrough.OutboxMultipart
	}
	entry, err := outbox.Prepare(runthrough.OutboxEntry{Operation: operation, Bucket: "probe", Key: "target", PreviousVersion: old.VersionID, UpstreamVersion: etag})
	if err != nil {
		t.Fatal(err)
	}
	version := old.VersionID
	if entry.Operation != runthrough.OutboxDelete {
		// COPY and multipart recovery use the committed destination record too.
		meta, err := raw.PutObject(ctx, "probe", "target", strings.NewReader("new"), storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "committed"}})
		if err != nil {
			t.Fatal(err)
		}
		version = meta.VersionID
	}
	local := &recoveryHeadStore{Store: raw}
	f := &recoveryFixture{local: local, upstream: upstream, outbox: outbox, path: path, entry: entry, version: version}
	f.resetAdapter()
	t.Cleanup(func() { _ = f.adapter.Close() })
	f.journal, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *recoveryFixture) resetAdapter() {
	f.adapter = runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, f.local, f.local, f.upstream, f.outbox)
}
func (f *recoveryFixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.outbox.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.outbox, err = runthrough.NewFileOutbox(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.resetAdapter()
}
func recoveryFailure(kind string) error {
	switch kind {
	case "marked-missing":
		return errors.Join(storage.ErrMutationCommitted, storage.ErrObjectNotFound)
	case "wrapped-marked":
		return fmt.Errorf("lookup: %w", errors.Join(storage.ErrObjectNotFound, errors.New("lookup uncertain"), storage.ErrMutationCommitted))
	default:
		return errors.New("lookup uncertain")
	}
}
func (f *recoveryFixture) recover(entrypoint string) error {
	if entrypoint == "retry" {
		return f.adapter.RetryPending(context.Background())
	}
	return f.adapter.RecoverPrepared(context.Background())
}
