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

func TestProductionPreparedDeleteCrashProcess(t *testing.T) {
	phase := os.Getenv("STOW_DELETE_CRASH_PHASE")
	if phase == "" {
		return
	}
	dir := os.Getenv("STOW_DELETE_CRASH_DIR")
	box, err := NewFileOutbox(filepath.Join(dir, "outbox.json"))
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
	meta, err := local.PutObject(ctx, "bucket", "key", strings.NewReader("original"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewWithOutbox(Config{}, local, local, nil, box)
	if err := adapter.saveUpstreamState("bucket", "key", UpstreamState{Absent: true}); err != nil {
		t.Fatal(err)
	}
	entry, err := adapter.prepareIntent(OutboxDelete, "bucket", "key", objectVersion(meta))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Version != "" || entry.PreviousVersion != objectVersion(meta) {
		t.Fatalf("production intent=%+v", entry)
	}
	if err := box.RenewPrepared(entry.ID, entry.PreparedOwner, entry.PreparedToken, time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	if phase == "after" {
		if err := local.DeleteObject(ctx, "bucket", "key"); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(0)
}

func TestRecoverProductionPreparedDeleteAfterCrash(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			process := exec.Command(os.Args[0], "-test.run=^TestProductionPreparedDeleteCrashProcess$")
			process.Env = append(os.Environ(), "STOW_DELETE_CRASH_PHASE="+phase, "STOW_DELETE_CRASH_DIR="+dir)
			if output, err := process.CombinedOutput(); err != nil {
				t.Fatalf("child=%v %s", err, output)
			}
			box, err := NewFileOutbox(filepath.Join(dir, "outbox.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer box.Close()
			local, err := filesystem.NewFilesystemStore(filepath.Join(dir, "objects"))
			if err != nil {
				t.Fatal(err)
			}
			defer local.Close()
			prepared := box.Prepared()
			if len(prepared) != 1 || outboxOwnerAlive(prepared[0].PreparedOwner) {
				t.Fatalf("crashed intent=%+v", prepared)
			}
			adapter := NewWithOutbox(Config{}, local, local, nil, box)
			if err := adapter.RecoverPrepared(context.Background()); err != nil {
				t.Fatal(err)
			}
			pending := box.Pending()
			if len(box.Prepared()) != 0 {
				t.Fatalf("stranded prepared=%+v", box.Prepared())
			}
			if phase == "before" && len(pending) != 0 {
				t.Fatalf("uncommitted delete became pending: %+v", pending)
			}
			if phase == "after" && (len(pending) != 1 || pending[0].Version != prepared[0].PreviousVersion) {
				t.Fatalf("committed delete lost version: %+v", pending)
			}
		})
	}
}
