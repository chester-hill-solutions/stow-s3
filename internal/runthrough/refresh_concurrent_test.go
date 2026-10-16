package runthrough_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type pausedVersionCache struct {
	*storage.MemoryStore
	gets             atomic.Int32
	entered, release chan struct{}
}

func (s *pausedVersionCache) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	if s.gets.Add(1) == 1 {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return s.MemoryStore.GetObject(ctx, bucket, key)
}

type concurrentVersionResult struct {
	data []byte
	meta *storage.ObjectMeta
	err  error
}

func readConcurrentVersion(ctx context.Context, adapter *runthrough.Adapter, instance *runtime.Instance) concurrentVersionResult {
	if instance != nil {
		object, err := instance.GetObject(ctx, "probe", "target")
		return concurrentVersionResult{object.Data, &storage.ObjectMeta{Size: object.Size, ETag: object.ETag, ContentType: object.ContentType, Metadata: object.Metadata}, err}
	}
	reader, meta, err := adapter.GetObject(ctx, "probe", "target")
	if err != nil {
		return concurrentVersionResult{err: err}
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	return concurrentVersionResult{data, meta, err}
}
func TestRefreshConcurrentCacheReadCapturesVersion(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		for _, body := range []string{"new", "longer new body", ""} {
			t.Run(fmt.Sprintf("runtime=%t/size=%d", embedded, len(body)), func(t *testing.T) { checkConcurrentVersion(t, body, embedded) })
		}
	}
}
func checkConcurrentVersion(t *testing.T, body string, embedded bool) {
	t.Helper()
	ctx, cancel := versionContext(t)
	defer cancel()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	cache := &pausedVersionCache{MemoryStore: storage.NewMemoryStore(), entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(cache.release) }) }
	defer release()
	upstream := newMockUpstream()
	if err := versionPut(ctx, upstream, "old"); err != nil {
		t.Fatal(err)
	}
	adapter := runthrough.NewWithCache(runthrough.Config{Policy: runthrough.PolicyReadThroughCache, Revalidate: true}, local, cache, upstream)
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendMemory}, adapter, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { release(); _ = instance.Close() }()
	var readerInstance *runtime.Instance
	if embedded {
		readerInstance = instance
	}
	done := make(chan concurrentVersionResult, 1)
	go func() { done <- readConcurrentVersion(ctx, adapter, readerInstance) }()
	select {
	case <-cache.entered:
	case <-ctx.Done():
		t.Fatal("first refresh did not reach cache read")
	}
	if err := versionPut(ctx, upstream, body); err != nil {
		t.Fatal(err)
	}
	second := readConcurrentVersion(ctx, adapter, readerInstance)
	release()
	var first concurrentVersionResult
	select {
	case first = <-done:
	case <-ctx.Done():
		t.Fatal("first refresh did not finish")
	}
	current, err := upstream.HeadObject(ctx, "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	assertConcurrentVersion(t, first, current, body)
	assertConcurrentVersion(t, second, current, body)
	if cache.gets.Load() != 2 {
		t.Errorf("cache gets=%d want=2", cache.gets.Load())
	}
}
func assertConcurrentVersion(t *testing.T, result concurrentVersionResult, current *storage.ObjectMeta, body string) {
	t.Helper()
	if result.err != nil {
		t.Errorf("captured read failed: %v", result.err)
		return
	}
	if string(result.data) != body || result.meta.ETag != current.ETag || result.meta.Size != current.Size {
		t.Errorf("cache read mixed versions: data=%q meta=%+v current=%+v", result.data, result.meta, current)
	}
}
