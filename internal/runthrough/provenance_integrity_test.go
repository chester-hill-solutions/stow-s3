package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestProvenanceSurvivesRestartAndEviction(t *testing.T) {
	for _, firstWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "write"}[firstWrite], func(t *testing.T) {
			ctx := context.Background()
			local := storage.NewMemoryStore()
			if err := local.CreateBucket(ctx, "bucket"); err != nil {
				t.Fatal(err)
			}
			up := conflictUpstream(t, ctx, "original")
			path := filepath.Join(t.TempDir(), "outbox.json")
			open := func() *runthrough.Adapter {
				outbox, err := runthrough.NewFileOutbox(path)
				if err != nil {
					t.Fatal(err)
				}
				return runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true, Cache: runthrough.CachePolicy{MaxBytes: 1}}, local, storage.NewMemoryStore(), up, outbox)
			}
			a := open()
			readThrough(t, a, ctx)
			if a.CacheEvictions() == 0 {
				t.Fatal("expected cache eviction")
			}
			if firstWrite {
				if _, err := a.PutObject(ctx, "bucket", "key", bytes.NewBufferString("agent first"), storage.PutOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			a = open()
			if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewBufferString("device update"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := a.PutObject(ctx, "bucket", "key", bytes.NewBufferString("agent edit"), storage.PutOptions{}); !errors.Is(err, runthrough.ErrUpstreamConflict) {
				t.Fatalf("lost durable provenance: %v", err)
			}
		})
	}
}

func TestEveryMutationPreservesConcurrentUpstreamChanges(t *testing.T) {
	for _, operation := range []string{"put", "copy", "multipart", "delete", "batch-delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			local := storage.NewMemoryStore()
			if err := local.CreateBucket(ctx, "bucket"); err != nil {
				t.Fatal(err)
			}
			up := conflictUpstream(t, ctx, "original")
			a := mirroringAdapter(t, local, up)
			readThrough(t, a, ctx)
			if _, err := local.PutObject(ctx, "bucket", "key", bytes.NewBufferString("original"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := up.PutObject(ctx, "bucket", "key", bytes.NewBufferString("device update"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			err := mutateKey(ctx, a, operation)
			if !errors.Is(err, runthrough.ErrUpstreamConflict) {
				t.Fatalf("%s error = %v", operation, err)
			}
			if string(up.bodies[objectKey("bucket", "key")]) != "device update" {
				t.Fatal("overwrote other writer")
			}
		})
	}
}

func mutateKey(ctx context.Context, a *runthrough.Adapter, operation string) error {
	switch operation {
	case "put":
		_, err := a.PutObject(ctx, "bucket", "key", bytes.NewBufferString("agent"), storage.PutOptions{})
		return err
	case "copy":
		_, err := a.CopyObject(ctx, "bucket", "key", "bucket", "key")
		return err
	case "delete":
		return a.DeleteObject(ctx, "bucket", "key")
	case "batch-delete":
		_, err := a.DeleteObjects(ctx, "bucket", []string{"key"})
		return err
	case "multipart":
		u, err := a.CreateMultipartUpload(ctx, "bucket", "key", storage.MultipartOptions{})
		if err != nil {
			return err
		}
		p, err := a.UploadPart(ctx, u.UploadID, 1, bytes.NewBufferString("agent"))
		if err != nil {
			return err
		}
		_, err = a.CompleteMultipartUpload(ctx, u.UploadID, []storage.PartInfo{*p})
		return err
	}
	return errors.New("unknown operation")
}

func TestLegacyIntentWithoutProvenanceIsRefused(t *testing.T) {
	ctx := context.Background()
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := local.PutObject(ctx, "bucket", "key", bytes.NewBufferString("agent"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "bucket", Key: "key"}); err != nil {
		t.Fatal(err)
	}
	up := conflictUpstream(t, ctx, "other writer")
	a := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, local, local, up, outbox)
	if err := a.RetryPending(ctx); err == nil {
		t.Fatal("unguarded legacy intent accepted")
	}
	if string(up.bodies[objectKey("bucket", "key")]) != "other writer" {
		t.Fatal("legacy intent overwrote upstream")
	}
}
