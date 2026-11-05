package fs

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestListObjectsV2ReadsOnlyRecordsOnRequestedPage(t *testing.T) {
	ctx := context.Background()
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "z"} {
		if _, err := store.PutObject(ctx, "bucket", key, strings.NewReader(key), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(store.objectPath("bucket", "z"), []byte("corrupt record"), 0o600); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListObjectsV2(ctx, "bucket", storage.ListOptions{MaxKeys: 1})
	if err != nil {
		t.Fatalf("first page read an unrequested record: %v", err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Key != "a" || !page.IsTruncated || page.NextContinuationToken != "a" {
		t.Fatalf("first page = %+v, want key a and a continuation token", page)
	}
	if _, err := store.ListObjectsV2(ctx, "bucket", storage.ListOptions{MaxKeys: 1, ContinuationToken: page.NextContinuationToken}); err == nil {
		t.Fatal("page containing the corrupt record succeeded")
	}
}

func TestListObjectsV2KeepsSortedContinuationAcrossPathLayouts(t *testing.T) {
	ctx := context.Background()
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	keys := []string{"z", "a", "m", strings.Repeat("q", 350)}
	want := append([]string(nil), keys...)
	sort.Strings(want)
	for _, key := range keys {
		if _, err := store.PutObject(ctx, "bucket", key, strings.NewReader(key), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	var got []string
	token := ""
	for {
		page, err := store.ListObjectsV2(ctx, "bucket", storage.ListOptions{MaxKeys: 2, ContinuationToken: token})
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range page.Objects {
			got = append(got, object.Key)
		}
		if !page.IsTruncated {
			break
		}
		if page.NextContinuationToken == "" || page.NextContinuationToken == token {
			t.Fatalf("invalid continuation token after %v: %q", got, page.NextContinuationToken)
		}
		token = page.NextContinuationToken
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("paginated keys = %q, want sorted keys %q", got, want)
	}
}

func TestListObjectsV2KeepsDelimiterContinuation(t *testing.T) {
	ctx := context.Background()
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a/1", "a/2", "b/1", "z"} {
		if _, err := store.PutObject(ctx, "bucket", key, strings.NewReader(key), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	wantPrefixes := []string{"a/", "b/"}
	token := ""
	for _, wantPrefix := range wantPrefixes {
		page, err := store.ListObjectsV2(ctx, "bucket", storage.ListOptions{Delimiter: "/", MaxKeys: 1, ContinuationToken: token})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.CommonPrefixes) != 1 || page.CommonPrefixes[0] != wantPrefix || !page.IsTruncated {
			t.Fatalf("prefix page = %+v, want %q and truncation", page, wantPrefix)
		}
		token = page.NextContinuationToken
	}
	page, err := store.ListObjectsV2(ctx, "bucket", storage.ListOptions{Delimiter: "/", MaxKeys: 1, ContinuationToken: token})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Key != "z" || page.IsTruncated {
		t.Fatalf("final page = %+v, want object z and no truncation", page)
	}
}
