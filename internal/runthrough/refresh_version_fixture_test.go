package runthrough_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type versionClient struct {
	*mockUpstream
	gets, closes int
	change       func() error
}

func (c *versionClient) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	c.gets++
	if c.change != nil {
		if err := c.change(); err != nil {
			return nil, nil, err
		}
	}
	reader, meta, err := c.mockUpstream.GetObject(ctx, bucket, key)
	if reader != nil {
		reader = &versionReader{ReadCloser: reader, client: c}
	}
	return reader, meta, err
}

type versionReader struct {
	io.ReadCloser
	client *versionClient
}

func (r *versionReader) Close() error { r.client.closes++; return r.ReadCloser.Close() }

type versionOutbox struct {
	*runthrough.MemoryOutbox
	saves   int
	failure error
}

func (o *versionOutbox) SaveUpstreamState(bucket, key string, state runthrough.UpstreamState) error {
	o.saves++
	if o.saves == 2 && o.failure != nil {
		return o.failure
	}
	return o.MemoryOutbox.SaveUpstreamState(bucket, key, state)
}

type versionFixture struct {
	adapter  *runthrough.Adapter
	instance *runtime.Instance
	client   *versionClient
	cache    storage.Store
	outbox   runthrough.Outbox
	original *storage.ObjectMeta
	body     string
	local    *storage.MemoryStore
}

func versionPut(ctx context.Context, client *mockUpstream, body string) error {
	checksum := sha256.Sum256([]byte(body))
	_, err := client.PutObject(ctx, "probe", "target", strings.NewReader(body), storage.PutOptions{ContentType: "application/new", Metadata: map[string]string{"state": "new"}, ChecksumAlgorithm: "SHA256", ChecksumValue: base64.StdEncoding.EncodeToString(checksum[:])})
	return err
}

// disappearingVersionCache deterministically models another reader evicting the
// freshly published entry before refresh can open it.
type disappearingVersionCache struct{ *storage.MemoryStore }

func (s *disappearingVersionCache) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if err := s.MemoryStore.DeleteObject(ctx, bucket, key); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		return nil, nil, err
	}
	return s.MemoryStore.GetObject(ctx, bucket, key)
}

func newVersionFixture(t testing.TB, body string, outbox runthrough.Outbox) *versionFixture {
	t.Helper()
	return newVersionFixtureWithCache(t, body, outbox, &disappearingVersionCache{storage.NewMemoryStore()})
}

func newVersionFixtureWithCache(t testing.TB, body string, outbox runthrough.Outbox, cache storage.Store) *versionFixture {
	t.Helper()
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if cache == nil {
		cache = storage.NewMemoryStore()
	}
	if err := local.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	client := &versionClient{mockUpstream: newMockUpstream()}
	if _, err := client.PutObject(ctx, "probe", "target", strings.NewReader("old"), storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "old"}}); err != nil {
		t.Fatal(err)
	}
	original, err := client.HeadObject(ctx, "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	if outbox == nil {
		outbox = runthrough.NewMemoryOutbox()
	}
	f := &versionFixture{client: client, cache: cache, outbox: outbox, original: original, body: body, local: local}
	client.change = func() error {
		if client.gets == 2 {
			return versionPut(ctx, client.mockUpstream, body)
		}
		return nil
	}
	maxBytes := int64(1)
	if _, disappears := cache.(*disappearingVersionCache); disappears {
		maxBytes = 0
	}
	f.adapter = runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyReadThroughCache, Cache: runthrough.CachePolicy{MaxBytes: maxBytes}}, local, cache, client, outbox)
	f.instance, err = runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendMemory}, f.adapter, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.instance.Close() })
	return f
}
func observeVersion(f *versionFixture, mode string) ([]byte, *storage.ObjectMeta, error) {
	ctx := context.Background()
	if mode == "runtime" {
		object, err := f.instance.GetObject(ctx, "probe", "target")
		return object.Data, &storage.ObjectMeta{Size: object.Size, ETag: object.ETag, ContentType: object.ContentType, Metadata: object.Metadata, ChecksumAlgorithm: object.ChecksumAlgorithm, ChecksumValue: object.ChecksumValue}, err
	}
	reader, meta, err := f.adapter.GetObject(ctx, "probe", "target")
	if err != nil {
		return nil, meta, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	return data, meta, err
}
func assertVersionResult(t testing.TB, f *versionFixture, data []byte, meta *storage.ObjectMeta) {
	t.Helper()
	current, err := f.client.HeadObject(context.Background(), "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != f.body || meta.Size != current.Size || meta.ETag != current.ETag {
		t.Errorf("mixed body/identity: data=%q meta=%+v current=%+v", data, meta, current)
	}
	if meta.ContentType != current.ContentType || meta.Metadata["state"] != current.Metadata["state"] {
		t.Errorf("mixed properties: got=%+v current=%+v", meta, current)
	}
	if meta.ChecksumAlgorithm != current.ChecksumAlgorithm || meta.ChecksumValue != current.ChecksumValue {
		t.Errorf("mixed checksum: got=%+v current=%+v", meta, current)
	}
	state, known, err := f.outbox.(interface {
		UpstreamState(string, string) (runthrough.UpstreamState, bool, error)
	}).UpstreamState("probe", "target")
	if err != nil || !known || state.ETag != current.ETag || state.Absent || state.Unknown {
		t.Errorf("stale upstream state: state=%+v known=%t error=%v current=%s", state, known, err, current.ETag)
	}
}
func fileVersionOutbox(t testing.TB) *runthrough.FileOutbox {
	t.Helper()
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	return outbox
}
func assertEvictedVersion(t testing.TB, f *versionFixture) {
	t.Helper()
	if _, err := f.cache.HeadObject(context.Background(), "probe", "target"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Errorf("oversized cached copy not evicted: %v", err)
	}
	if f.client.gets != 2 || f.client.closes != 2 {
		t.Errorf("gets=%d closes=%d want=2/2", f.client.gets, f.client.closes)
	}
}
func versionContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 10*time.Second)
}
