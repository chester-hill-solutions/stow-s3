package runthrough

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestOutboxRetryJitterIsBoundedAndCapsExponentialGrowth(t *testing.T) {
	for _, item := range []struct {
		attempts int
		ceiling  time.Duration
	}{
		{-100, time.Second}, {0, time.Second}, {1, time.Second},
		{2, 2 * time.Second}, {3, 4 * time.Second}, {4, 8 * time.Second},
		{5, 16 * time.Second}, {6, 32 * time.Second}, {100, 32 * time.Second},
	} {
		lower := outboxRetryDelayFrom(item.attempts, func(int64) int64 { return 0 })
		upper := outboxRetryDelayFrom(item.attempts, func(limit int64) int64 { return limit - 1 })
		if lower != item.ceiling/2 || upper != item.ceiling {
			t.Fatalf("attempt %d jitter window = %v..%v", item.attempts, lower, upper)
		}
		delay := outboxRetryDelay(item.attempts)
		if delay < lower || delay > upper {
			t.Fatalf("delay %v outside %v..%v", delay, lower, upper)
		}
	}
}

type cancellingRetryClient struct {
	Client
	cancel context.CancelFunc
	puts   int
}

func (c *cancellingRetryClient) PutObject(context.Context, string, string, io.Reader, storage.PutOptions) (string, error) {
	c.puts++
	c.cancel()
	return "", context.Canceled
}

func TestCancelledRetryLeavesUnstartedIntentsUntouched(t *testing.T) {
	for _, cancelBeforeStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "during propagation", true: "before retry"}[cancelBeforeStart], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			local := storage.NewMemoryStore()
			outbox := NewMemoryOutbox()
			if err := local.CreateBucket(context.Background(), "assets"); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"first", "second"} {
				meta, err := local.PutObject(context.Background(), "assets", key, bytes.NewReader([]byte(key)), storage.PutOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := outbox.Enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "assets", Key: key, Version: meta.VersionID, UpstreamAbsent: true}); err != nil {
					t.Fatal(err)
				}
			}
			upstream := &cancellingRetryClient{cancel: cancel}
			adapter := NewWithOutbox(Config{Policy: PolicyMirrorWrites, AllowLiveWrites: true}, local, local, upstream, outbox)
			if cancelBeforeStart {
				cancel()
			}
			if err := adapter.RetryPending(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("retry error = %v", err)
			}
			wantCalls := 1
			if cancelBeforeStart {
				wantCalls = 0
			}
			if upstream.puts != wantCalls {
				t.Fatalf("started %d upstream calls after cancellation; want %d", upstream.puts, wantCalls)
			}
			pending := outbox.Pending()
			if len(pending) != 2 || pending[1].Attempts != 0 || pending[1].Attempted || !pending[1].NextAttempt.IsZero() {
				t.Fatalf("cancellation changed an unstarted intent: %+v", pending)
			}
		})
	}
}
