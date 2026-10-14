package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestMemoryStagingOwnsPayloadAndCommitsOnce(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	body := []byte("payload")
	metadata := map[string]string{"state": "original"}
	pending, err := store.StageObject(ctx, "assets", "target", bytes.NewReader(body), storage.PutOptions{ComputeChecksum: true, ChecksumAlgorithm: "SHA256", Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	body[0] = 'X'
	metadata["state"] = "changed"
	if _, err := store.HeadObject(ctx, "assets", "target"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("staged bytes visible: %v", err)
	}
	meta, err := pending.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Commit(ctx); err == nil {
		t.Fatal("staged object committed twice")
	}
	if err := pending.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	meta.Metadata["state"] = "result mutation"
	reader, saved, err := store.GetObject(ctx, "assets", "target")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	checksum, _ := storage.ComputeChecksum("SHA256", []byte("payload"))
	if err != nil || string(data) != "payload" || saved.Metadata["state"] != "original" || saved.ChecksumValue != checksum {
		t.Fatalf("owned publication: %q %+v %v", data, saved, err)
	}
}

func TestMemoryStagingRechecksConditionAndAbort(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	for _, abort := range []bool{false, true} {
		pending, err := store.StageObject(ctx, "assets", "target", strings.NewReader("staged"), storage.PutOptions{IfNoneMatch: "*"})
		if err != nil {
			t.Fatal(err)
		}
		if abort {
			if err := pending.Abort(ctx); err != nil {
				t.Fatal(err)
			}
			if err := pending.Abort(ctx); err != nil {
				t.Fatal(err)
			}
		} else if _, err := store.PutObject(ctx, "assets", "target", strings.NewReader("other"), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		_, err = pending.Commit(ctx)
		if err == nil || (!abort && !errors.Is(err, storage.ErrPreconditionFailed)) {
			t.Fatalf("staged commit after state change: %v", err)
		}
		if _, err := pending.Commit(ctx); err == nil {
			t.Fatal("failed commit reused pending body")
		}
		if !abort {
			if err := store.DeleteObject(ctx, "assets", "target"); err != nil {
				t.Fatal(err)
			}
		}
	}
}
