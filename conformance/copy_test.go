package conformance_test

// Copy semantics at the wire: a copy publishes one version of its source, and a
// conditional copy refuses rather than publishing a version the caller never
// asked for.
//
// The store contract covers this per-backend; this is the S3 surface, where the
// source conditions are the x-amz-copy-source-* headers and the question is
// whether the version they were checked against is the version that got copied.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// A conditional copy names the source version it wants, and a source that has
// moved on is a refusal.
//
// The hazard is a window, not a check. If the condition is evaluated against a
// version the server read and the bytes come from a later read, then a source
// overwritten in between produces a copy of the newer version — under a
// condition naming the older one. The client asked for specific bytes and
// received different ones, and nothing in the response says so.
func TestConditionalCopyRefusesWhenTheSourceHasMovedOn(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "copy-conditional")
	createBucket(ctx, t, env.Client, bucket)

	first, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String("source"),
		Body:        strings.NewReader("the original bytes"),
		ContentType: aws.String("text/plain"),
		Metadata:    map[string]string{"generation": "one"},
	})
	if err != nil {
		t.Fatalf("put source: %v", err)
	}
	second, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String("source"),
		Body:        strings.NewReader("the replacement bytes, which are longer"),
		ContentType: aws.String("application/json"),
		Metadata:    map[string]string{"generation": "two"},
	})
	if err != nil {
		t.Fatalf("overwrite source: %v", err)
	}
	if first.ETag == second.ETag {
		t.Fatal("the two versions share an ETag, so the case cannot distinguish them")
	}

	// A condition naming the version that is no longer there.
	_, err = env.Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String("stale-target"),
		CopySource:        aws.String(bucket + "/source"),
		CopySourceIfMatch: first.ETag,
	})
	if err == nil {
		t.Fatal("a conditional copy naming a version that is gone succeeded")
	}
	var responseErr *smithyhttp.ResponseError
	if !errors.As(err, &responseErr) || responseErr.HTTPStatusCode() != http.StatusPreconditionFailed {
		t.Fatalf("copy with a stale condition = %v, want 412", err)
	}
	if _, headErr := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("stale-target"),
	}); headErr == nil {
		t.Fatal("a refused conditional copy published the destination")
	}

	// A condition naming the version that is there succeeds, and what lands is
	// that version — the metadata is how it is checked, because the bodies
	// differ in length and the two are only distinguishable by which
	// generation's properties arrived.
	if _, err := env.Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String("current-target"),
		CopySource:        aws.String(bucket + "/source"),
		CopySourceIfMatch: second.ETag,
	}); err != nil {
		t.Fatalf("copy with a current condition: %v", err)
	}
	head, err := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("current-target"),
	})
	if err != nil {
		t.Fatalf("head copied object: %v", err)
	}
	if got := head.Metadata["generation"]; got != "two" {
		t.Fatalf("copied generation = %q, want two; the copy published a version the condition did not name", got)
	}
	if got := aws.ToString(head.ContentType); got != "application/json" {
		t.Fatalf("copied content type = %q, want application/json", got)
	}
	body := readObject(t, ctx, env, bucket, "current-target")
	if !strings.HasPrefix(body, "the replacement bytes") {
		t.Fatalf("copied body = %q, want the version the condition named", body)
	}
}

// If-None-Match is the same rule from the other side: refuse when the source is
// the version the caller wanted to avoid.
func TestCopyWithAMatchingIfNoneMatchIsRefused(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "copy-if-none-match")
	createBucket(ctx, t, env.Client, bucket)
	put, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("source"), Body: strings.NewReader("body"),
	})
	if err != nil {
		t.Fatalf("put source: %v", err)
	}
	_, err = env.Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:                aws.String(bucket),
		Key:                   aws.String("target"),
		CopySource:            aws.String(bucket + "/source"),
		CopySourceIfNoneMatch: put.ETag,
	})
	var responseErr *smithyhttp.ResponseError
	if !errors.As(err, &responseErr) || responseErr.HTTPStatusCode() != http.StatusPreconditionFailed {
		t.Fatalf("copy with a matching If-None-Match = %v, want 412", err)
	}
}

// A copy taken while its source is being rewritten is a whole version of it.
//
// Every interleaving here is a race, so the assertion is a property checked
// afterwards, and the case repeats: a copy is either the old version or the new
// one, never a body from one with the content type or metadata of the other.
func TestCopyWhileTheSourceIsRewrittenPublishesOneVersion(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "copy-race")
	createBucket(ctx, t, env.Client, bucket)

	versions := []wireVersion{
		{body: strings.Repeat("a", 4096), contentType: "text/plain", generation: "one"},
		{body: strings.Repeat("b", 8192), contentType: "application/json", generation: "two"},
	}
	putVersion := func(index int) {
		version := versions[index]
		if _, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String("source"),
			Body:        strings.NewReader(version.body),
			ContentType: aws.String(version.contentType),
			Metadata:    map[string]string{"generation": version.generation},
		}); err != nil {
			t.Fatalf("put version %d: %v", index, err)
		}
	}
	putVersion(0)

	for attempt := 0; attempt < 10; attempt++ {
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]string, 2)
		for i := range results {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				_, err := env.Client.CopyObject(ctx, &s3.CopyObjectInput{
					Bucket:     aws.String(bucket),
					Key:        aws.String([]string{"copy-a", "copy-b"}[index]),
					CopySource: aws.String(bucket + "/source"),
				})
				if err != nil {
					results[index] = err.Error()
				}
			}(i)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			putVersion(1)
		}()
		close(start)
		wg.Wait()

		for index, failure := range results {
			if failure != "" {
				t.Fatalf("attempt %d, copy %d: %s", attempt, index, failure)
			}
		}
		cohesion := copyCohesion{t: t, ctx: ctx, env: env, bucket: bucket, versions: versions}
		for _, key := range []string{"copy-a", "copy-b"} {
			cohesion.assertKey(key)
		}
		putVersion(0)
	}
}

type wireVersion struct {
	body        string
	contentType string
	generation  string
}

// copyCohesion is the context a coherence check needs: where the copies are, what
// the two versions are, and the environment to ask.
type copyCohesion struct {
	t        *testing.T
	ctx      context.Context
	env      *testEnv
	bucket   string
	versions []wireVersion
}

// assertKey checks that a copied key is a whole version: the body, the content
// type and the metadata all belong to the same one.
func (c copyCohesion) assertKey(key string) {
	c.t.Helper()
	head, err := c.env.Client.HeadObject(c.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket), Key: aws.String(key),
	})
	if err != nil {
		c.t.Fatalf("head %s: %v", key, err)
	}
	generation := head.Metadata["generation"]
	var want wireVersion
	var matched bool
	for _, version := range c.versions {
		if version.generation == generation {
			want, matched = version, true
		}
	}
	if !matched {
		c.t.Fatalf("%s: generation %q names no version", key, generation)
	}
	if got := aws.ToString(head.ContentType); got != want.contentType {
		c.t.Fatalf("%s: generation %q has content type %q; a copy that mixed two versions looks like this", key, generation, got)
	}
	if aws.ToInt64(head.ContentLength) != int64(len(want.body)) {
		c.t.Fatalf("%s: generation %q is %d bytes, want %d", key, generation, aws.ToInt64(head.ContentLength), len(want.body))
	}
	if body := readObject(c.t, c.ctx, c.env, c.bucket, key); body != want.body {
		c.t.Fatalf("%s: body is %d bytes, want the %d of generation %q", key, len(body), len(want.body), generation)
	}
}

func readObject(t *testing.T, ctx context.Context, env *testEnv, bucket, key string) string {
	t.Helper()
	get, err := env.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer get.Body.Close()
	body, err := io.ReadAll(get.Body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return string(body)
}

// A source deleted before the copy is reported as gone rather than published as
// an empty object.
func TestCopyOfADeletedSourceIsRefused(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "copy-deleted-source")
	createBucket(ctx, t, env.Client, bucket)
	if _, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("source"), Body: strings.NewReader("body"),
	}); err != nil {
		t.Fatalf("put source: %v", err)
	}
	if _, err := env.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("source"),
	}); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	_, err := env.Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String("target"),
		CopySource: aws.String(bucket + "/source"),
	})
	var responseErr *smithyhttp.ResponseError
	if !errors.As(err, &responseErr) || responseErr.HTTPStatusCode() != http.StatusNotFound {
		t.Fatalf("copy of a deleted source = %v, want 404", err)
	}
	if _, headErr := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("target"),
	}); headErr == nil {
		t.Fatal("a copy of a missing source published the destination")
	}
}

// Copying a key onto itself is a new version of it, not a deletion.
//
// It is the case that a naive "read the source, then write the destination"
// implementation gets wrong in the most damaging way available: if the write
// invalidates the read, the object the caller asked to preserve is the one that
// disappears.
func TestCopyOntoItselfPreservesTheObject(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "copy-self")
	createBucket(ctx, t, env.Client, bucket)
	body := strings.Repeat("s", 2048)
	put, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String("key"),
		Body:        strings.NewReader(body),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := env.Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String("key"),
		CopySource: aws.String(bucket + "/key"),
	}); err != nil {
		t.Fatalf("copy onto itself: %v", err)
	}
	head, err := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("key"),
	})
	if err != nil {
		t.Fatalf("head after a self-copy: %v", err)
	}
	if got := aws.ToString(head.ContentType); got != "text/plain" {
		t.Errorf("content type after a self-copy = %q, want text/plain", got)
	}
	if got := readObject(t, ctx, env, bucket, "key"); got != body {
		t.Errorf("body after a self-copy is %d bytes, want %d", len(got), len(body))
	}
	// The ETag is a function of the bytes, and the bytes are unchanged, so the
	// copy published the same object rather than a different one.
	if aws.ToString(head.ETag) != aws.ToString(put.ETag) {
		t.Errorf("ETag after a self-copy = %q, want %q", aws.ToString(head.ETag), aws.ToString(put.ETag))
	}
}
