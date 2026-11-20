package adaptertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence"
)

func Run(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	t.Helper()
	t.Run("put and get", func(t *testing.T) { runPutAndGet(t, newAdapter) })
	t.Run("missing object", func(t *testing.T) { runMissing(t, newAdapter) })
	t.Run("overwrite changes the entity tag", func(t *testing.T) { runOverwrite(t, newAdapter) })
	t.Run("conditional create", func(t *testing.T) { runConditionalCreate(t, newAdapter) })
	t.Run("conditional update", func(t *testing.T) { runConditionalUpdate(t, newAdapter) })
	t.Run("delete", func(t *testing.T) { runDelete(t, newAdapter) })
	t.Run("metadata and content type", func(t *testing.T) { runMetadata(t, newAdapter) })
	t.Run("bucket isolation", func(t *testing.T) { runBucketIsolation(t, newAdapter) })
	t.Run("list prefix", func(t *testing.T) { runListPrefix(t, newAdapter) })
	t.Run("list delimiter", func(t *testing.T) { runListDelimiter(t, newAdapter) })
	t.Run("list pagination", func(t *testing.T) { runListPagination(t, newAdapter) })
}

type fixture struct {
	t       *testing.T
	adapter persistence.Adapter
}

func newFixture(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) fixture {
	t.Helper()
	adapter := newAdapter(t)
	if admin, ok := adapter.(persistence.BucketAdmin); ok {
		ctx := context.Background()
		for _, name := range []string{"alpha", "beta"} {
			if err := admin.CreateBucket(ctx, name); err != nil && !errors.Is(err, persistence.ErrBucketExists) {
				t.Fatalf("create bucket %s: %v", name, err)
			}
		}
	}
	return fixture{t: t, adapter: adapter}
}

func (f fixture) put(bucket, key, body string) *persistence.Meta {
	return f.putWith(bucket, key, body, persistence.PutOptions{})
}

func (f fixture) putWith(bucket, key, body string, options persistence.PutOptions) *persistence.Meta {
	f.t.Helper()
	meta, err := f.adapter.Put(context.Background(), bucket, key, bytes.NewReader([]byte(body)), options)
	if err != nil {
		f.t.Fatalf("put %s/%s: %v", bucket, key, err)
	}
	return meta
}

func (f fixture) get(bucket, key string) ([]byte, *persistence.Meta) {
	f.t.Helper()
	object, err := f.adapter.Get(context.Background(), bucket, key)
	if err != nil {
		f.t.Fatalf("get %s/%s: %v", bucket, key, err)
	}
	defer object.Body.Close()
	body, err := io.ReadAll(object.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return body, &object.Meta
}

func (f fixture) conditional(bucket, key, body string, condition persistence.Condition) error {
	f.t.Helper()
	_, err := f.adapter.ConditionalPut(context.Background(), bucket, key, bytes.NewReader([]byte(body)),
		persistence.ConditionalPutOptions{Condition: condition})
	return err
}

func runPutAndGet(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	meta := f.put("alpha", "note.txt", "hello")
	if meta.ETag == "" {
		t.Fatal("put returned an empty ETag")
	}
	if meta.Size != int64(len("hello")) {
		t.Fatalf("put size = %d", meta.Size)
	}
	body, got := f.get("alpha", "note.txt")
	if string(body) != "hello" {
		t.Fatalf("get = %q", body)
	}
	if got.ETag != meta.ETag {
		t.Fatalf("get ETag %q != put ETag %q", got.ETag, meta.ETag)
	}
}

func runMissing(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	if _, err := f.adapter.Get(context.Background(), "alpha", "absent"); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("get absent = %v", err)
	}
}

func runOverwrite(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	first := f.put("alpha", "note.txt", "one")
	second := f.put("alpha", "note.txt", "two")
	if first.ETag == second.ETag {
		t.Fatalf("entity tag did not change on overwrite: %q", first.ETag)
	}
	body, _ := f.get("alpha", "note.txt")
	if string(body) != "two" {
		t.Fatalf("get after overwrite = %q", body)
	}
}

func runConditionalCreate(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	absent := persistence.Condition{IfAbsent: true}
	if err := f.conditional("alpha", "once.txt", "first", absent); err != nil {
		t.Fatalf("create-if-absent = %v", err)
	}
	if err := f.conditional("alpha", "once.txt", "second", absent); !errors.Is(err, persistence.ErrPreconditionFailed) {
		t.Fatalf("second create-if-absent = %v", err)
	}
	body, _ := f.get("alpha", "once.txt")
	if string(body) != "first" {
		t.Fatalf("create-if-absent overwrote: %q", body)
	}
}

func runConditionalUpdate(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	meta := f.put("alpha", "note.txt", "one")
	if err := f.conditional("alpha", "note.txt", "two", persistence.Condition{IfMatch: meta.ETag}); err != nil {
		t.Fatalf("update with the current tag = %v", err)
	}
	if err := f.conditional("alpha", "note.txt", "three", persistence.Condition{IfMatch: meta.ETag}); !errors.Is(err, persistence.ErrPreconditionFailed) {
		t.Fatalf("update with a stale tag = %v", err)
	}
	body, _ := f.get("alpha", "note.txt")
	if string(body) != "two" {
		t.Fatalf("stale update changed the body: %q", body)
	}
	if err := f.conditional("alpha", "absent.txt", "x", persistence.Condition{IfMatch: meta.ETag}); !errors.Is(err, persistence.ErrPreconditionFailed) {
		t.Fatalf("update of an absent object = %v", err)
	}
}

func runDelete(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	f.put("alpha", "note.txt", "hello")
	if err := f.adapter.Delete(context.Background(), "alpha", "note.txt"); err != nil {
		t.Fatalf("delete = %v", err)
	}
	if _, err := f.adapter.Get(context.Background(), "alpha", "note.txt"); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
	if err := f.adapter.Delete(context.Background(), "alpha", "note.txt"); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}

func runMetadata(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	f.putWith("alpha", "note.txt", "hello", persistence.PutOptions{
		ContentType: "text/plain", Metadata: map[string]string{"owner": "adaptertest"},
	})
	_, meta := f.get("alpha", "note.txt")
	if meta.ContentType != "text/plain" || meta.Metadata["owner"] != "adaptertest" {
		t.Fatalf("round-trip metadata = %q %v", meta.ContentType, meta.Metadata)
	}
}

func runBucketIsolation(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	f.put("alpha", "same.txt", "left")
	f.put("beta", "same.txt", "right")
	left, _ := f.get("alpha", "same.txt")
	right, _ := f.get("beta", "same.txt")
	if string(left) != "left" || string(right) != "right" {
		t.Fatalf("buckets shared an object: %q %q", left, right)
	}
}

func runListPrefix(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	f.put("alpha", "a/one.txt", "1")
	f.put("alpha", "a/two.txt", "2")
	f.put("alpha", "b/three.txt", "3")
	page, err := f.adapter.List(context.Background(), "alpha", persistence.ListOptions{Prefix: "a/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(page.Objects); !equal(got, []string{"a/one.txt", "a/two.txt"}) {
		t.Fatalf("prefix listing = %v", got)
	}
}

func runListDelimiter(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	f.put("alpha", "a/one.txt", "1")
	f.put("alpha", "b/two.txt", "2")
	f.put("alpha", "top.txt", "3")
	page, err := f.adapter.List(context.Background(), "alpha", persistence.ListOptions{Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(page.Objects); !equal(got, []string{"top.txt"}) {
		t.Fatalf("delimiter objects = %v", got)
	}
	if got := append([]string(nil), page.CommonPrefixes...); !equal(got, []string{"a/", "b/"}) {
		t.Fatalf("delimiter prefixes = %v", got)
	}
}

func runListPagination(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	f := newFixture(t, newAdapter)
	var want []string
	for index := 0; index < 5; index++ {
		key := fmt.Sprintf("page/%02d.txt", index)
		want = append(want, key)
		f.put("alpha", key, "x")
	}
	var got []string
	token := ""
	for pages := 0; ; pages++ {
		if pages > len(want) {
			t.Fatal("listing did not terminate")
		}
		page, err := f.adapter.List(context.Background(), "alpha", persistence.ListOptions{
			Prefix: "page/", MaxKeys: 2, ContinuationToken: token,
		})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, keys(page.Objects)...)
		if !page.IsTruncated {
			break
		}
		if page.NextContinuationToken == "" {
			t.Fatal("truncated page carried no continuation token")
		}
		token = page.NextContinuationToken
	}
	sort.Strings(got)
	if !equal(got, want) {
		t.Fatalf("paginated listing = %v, want %v", got, want)
	}
}

func keys(objects []persistence.Meta) []string {
	out := make([]string, 0, len(objects))
	for _, object := range objects {
		out = append(out, object.Key)
	}
	sort.Strings(out)
	return out
}

func equal(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
