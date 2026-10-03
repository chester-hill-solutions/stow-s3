package runthrough

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	filesystem "github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

func TestOwnedPreparedCrashProcess(t *testing.T) {
	if os.Getenv("STOW_OWNED_CRASH_HELPER") != "1" {
		return
	}
	dir := os.Getenv("STOW_OWNED_CRASH_DIR")
	outbox, err := NewFileOutbox(filepath.Join(dir, "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := filesystem.NewFilesystemStore(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	adapter := NewWithOutbox(Config{}, local, local, nil, outbox)
	if _, err := outbox.PrepareOwned(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: "key", UpstreamAbsent: true}, adapter.claimOwner, time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	if _, err := local.PutObject(ctx, "bucket", "key", strings.NewReader("survives crash"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	// Exit between the durable local mutation and CommitPrepared, without cleanup.
	os.Exit(0)
}

func TestRecoverOwnedIntentAfterProcessExit(t *testing.T) {
	dir := t.TempDir()
	process := exec.Command(os.Args[0], "-test.run=^TestOwnedPreparedCrashProcess$")
	process.Env = append(os.Environ(), "STOW_OWNED_CRASH_HELPER=1", "STOW_OWNED_CRASH_DIR="+dir)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, output)
	}
	outbox, err := NewFileOutbox(filepath.Join(dir, "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer outbox.Close()
	local, err := filesystem.NewFilesystemStore(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	prepared := outbox.Prepared()
	if len(prepared) != 1 || outboxOwnerAlive(prepared[0].PreparedOwner) {
		t.Fatalf("expected owned crashed intent: %+v", prepared)
	}
	adapter := NewWithOutbox(Config{}, local, local, nil, outbox)
	if err := adapter.RecoverPrepared(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending := outbox.Pending()
	meta, err := local.HeadObject(context.Background(), "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox.Prepared()) != 0 || len(pending) != 1 || pending[0].Version != objectVersion(meta) {
		t.Fatalf("recovery did not commit exact version: %+v", pending)
	}
	if err := adapter.RecoverPrepared(context.Background()); err != nil || len(outbox.Pending()) != 1 {
		t.Fatalf("repeat recovery: %v", err)
	}
}
