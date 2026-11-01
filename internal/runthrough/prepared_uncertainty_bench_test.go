package runthrough

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type recoveryCostStore struct {
	*storage.MemoryStore
	failure error
	heads   int
}

func (s *recoveryCostStore) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	s.heads++
	if s.failure != nil {
		return nil, s.failure
	}
	return s.MemoryStore.HeadObject(ctx, bucket, key)
}

// These counters isolate recovery classification from journal I/O. They do not
// qualify durable persistence; the integration regressions use FileOutbox.
type recoveryCostOutbox struct {
	CoordinatedOutbox
	commits, discards int
}

func (o *recoveryCostOutbox) Commit(string, string) (OutboxEntry, error) {
	o.commits++
	return OutboxEntry{}, nil
}
func (o *recoveryCostOutbox) DiscardPrepared(string) error { o.discards++; return nil }

func BenchmarkPreparedRecoveryClassification(b *testing.B) {
	for _, operation := range []OutboxOperation{OutboxPut, OutboxDelete} {
		for _, mode := range []string{"present", "missing", "marked", "unknown"} {
			b.Run(string(operation)+"/"+mode, func(b *testing.B) { benchmarkPreparedRecovery(b, operation, mode) })
		}
	}
}
func benchmarkPreparedRecovery(b *testing.B, operation OutboxOperation, mode string) {
	ctx := context.Background()
	local := &recoveryCostStore{MemoryStore: storage.NewMemoryStore()}
	b.Cleanup(func() { _ = local.Close() })
	if err := local.CreateBucket(ctx, "probe"); err != nil {
		b.Fatal(err)
	}
	old, err := local.PutObject(ctx, "probe", "target", strings.NewReader("old"), storage.PutOptions{})
	if err != nil {
		b.Fatal(err)
	}
	if operation == OutboxPut {
		if _, err := local.PutObject(ctx, "probe", "target", strings.NewReader("new"), storage.PutOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	switch mode {
	case "missing":
		local.failure = storage.ErrObjectNotFound
	case "marked":
		local.failure = errors.Join(storage.ErrMutationCommitted, storage.ErrObjectNotFound)
	case "unknown":
		local.failure = errors.New("HEAD uncertain")
	}
	outbox := &recoveryCostOutbox{}
	adapter := &Adapter{local: local, coordinatedOutbox: outbox}
	entry := OutboxEntry{ID: "fixed", Operation: operation, Bucket: "probe", Key: "target", PreviousVersion: old.VersionID}
	failures := 0
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := adapter.reconcilePreparedEntryLocked(ctx, entry)
		if err != nil {
			if err != local.failure {
				b.Fatal(err)
			}
			failures++
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(outbox.commits)/float64(b.N), "commits/op")
	b.ReportMetric(float64(outbox.discards)/float64(b.N), "discards/op")
	b.ReportMetric(float64(failures)/float64(b.N), "errors/op")
	b.ReportMetric(float64(local.heads)/float64(b.N), "heads/op")
}
