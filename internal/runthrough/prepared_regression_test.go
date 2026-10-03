package runthrough

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestOwnedPreparedRecoveryAndSlowMutation(t *testing.T) {
	for _, dead := range []bool{false, true} {
		t.Run(map[bool]string{false: "slow-owner", true: "dead-owner"}[dead], func(t *testing.T) {
			outbox, err := NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer outbox.Close()
			local := storage.NewMemoryStore()
			ctx := context.Background()
			local.CreateBucket(ctx, "bucket")
			adapter := NewWithOutbox(Config{}, local, local, nil, outbox)
			owner := adapter.claimOwner
			if dead {
				owner = "dead-2147483647-test"
			}
			entry, err := outbox.PrepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "key", UpstreamAbsent: true}, owner, time.Nanosecond)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := local.PutObject(ctx, "bucket", "key", strings.NewReader("committed"), storage.PutOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := outbox.CommitPrepared(entry.ID, "wrong-owner", entry.PreparedToken, "other"); !errors.Is(err, ErrOutboxClaimLost) {
				t.Fatalf("stale owner committed: %v", err)
			}
			if dead {
				err = adapter.RecoverPrepared(ctx)
			} else {
				_, err = adapter.commitPreparedIntent(entry, objectVersion(meta))
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(outbox.Prepared()) != 0 || len(outbox.Pending()) != 1 {
				t.Fatalf("intent stranded: prepared=%+v pending=%+v", outbox.Prepared(), outbox.Pending())
			}
		})
	}
}
