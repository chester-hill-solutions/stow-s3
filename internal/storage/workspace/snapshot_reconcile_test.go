package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSnapshotMultipartMetadataDoesNotDependOnPriorLiveReads(t *testing.T) {
	for _, mutation := range []string{"none", "timestamp", "content", "legacy"} {
		t.Run(mutation, func(t *testing.T) {
			store, root, _ := multipartHostFixture(t, 1)
			mutateSnapshotFixture(t, store, mutation)
			indexBefore, err := os.ReadFile(indexPath(root))
			if err != nil {
				t.Fatal(err)
			}
			before, err := ReadLogicalSnapshot(context.Background(), root, SnapshotOptions{})
			if err != nil || len(before.Objects) != 1 {
				t.Fatalf("snapshot=%+v err=%v", before, err)
			}
			indexAfter, err := os.ReadFile(indexPath(root))
			if err != nil || !bytes.Equal(indexBefore, indexAfter) {
				t.Fatalf("snapshot modified the index: %v", err)
			}
			meta, err := store.HeadObject(context.Background(), multipartHostBucket, "artifact")
			if err != nil {
				t.Fatal(err)
			}
			object := before.Objects[0]
			if object.ContentType != meta.ContentType || !reflect.DeepEqual(object.Metadata, meta.Metadata) {
				t.Fatalf("snapshot=%+v live=%+v", object, meta)
			}
			after, err := ReadLogicalSnapshot(context.Background(), root, SnapshotOptions{})
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("live read changed snapshot: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}

func mutateSnapshotFixture(t *testing.T, store *Store, mutation string) {
	t.Helper()
	path := filepath.Join(store.root, "artifact")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mutation == "content" {
		if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	modified := info.ModTime()
	if mutation == "timestamp" {
		modified = modified.Add(time.Hour)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	if mutation == "legacy" {
		entry, _ := store.objectIndex.entry(multipartHostBucket, "artifact")
		entry.ContentETag = ""
		if err := store.recordLocked(multipartHostBucket, "artifact", entry); err != nil {
			t.Fatal(err)
		}
	}
}
