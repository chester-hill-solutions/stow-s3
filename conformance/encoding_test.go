package conformance_test

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// escapeKeyPath percent-encodes a key for use in a header that is supposed to
// carry the encoded form, leaving the separators intact.
func escapeKeyPath(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// A key that needs encoding must be signable by a real SDK.
//
// This case exists because it did not, and the gap was invisible. Every key in the
// conformance corpus was made of characters that survive encoding unchanged, and the
// two hand-written signers in internal/auth build the canonical URI from
// r.URL.EscapedPath() - the same double-encoding the server does. So the server and
// its tests agreed with each other and neither agreed with AWS.
//
// The real SDK is the only independent signer in the repository, which makes this
// the test that can catch a signing bug the other signers cannot.
//
// The cases are the ones a caller actually types: a space, a percent sign, a plus,
// and a non-ASCII character. A plus is the sharp one - in a path it is a literal
// plus, not an encoded space, so an implementation that reuses the query-string
// rules rewrites "a+b" as "a b" and signs a different object than the one it reads.
func TestKeysNeedingEncodingAreSignable(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t)
	createBucket(ctx, t, env.Client, bucket)

	cases := []struct {
		name string
		key  string
	}{
		{"space", "a b.txt"},
		{"percent", "100%.txt"},
		{"literal plus", "a+b.txt"},
		{"non-ascii", "café.txt"},
		{"space and slash", "dir name/with space.txt"},
		{"encoded-looking", "already%20encoded.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte("body for " + tc.key)

			putOut, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(tc.key),
				Body:   bytes.NewReader(body),
			})
			if err != nil {
				t.Fatalf("PutObject(%q): %v", tc.key, err)
			}

			getOut, err := env.Client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(tc.key),
			})
			if err != nil {
				t.Fatalf("GetObject(%q): %v", tc.key, err)
			}
			defer getOut.Body.Close()
			got, err := io.ReadAll(getOut.Body)
			if err != nil {
				t.Fatalf("read %q: %v", tc.key, err)
			}
			if string(got) != string(body) {
				t.Fatalf("GetObject(%q) = %q, want %q", tc.key, got, body)
			}

			headOut, err := env.Client.HeadObject(ctx, &s3.HeadObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(tc.key),
			})
			if err != nil {
				t.Fatalf("HeadObject(%q): %v", tc.key, err)
			}
			if aws.ToString(headOut.ETag) != aws.ToString(putOut.ETag) {
				t.Errorf("Head ETag %q != Put ETag %q for %q", aws.ToString(headOut.ETag), aws.ToString(putOut.ETag), tc.key)
			}
		})
	}
}

// A copy whose source key needs encoding addresses the same object. CopyObject
// takes the source in a header rather than the path, so it exercises a different
// signer path from PutObject and a fix that only handled the path would miss it.
func TestCopyObjectWithAnEncodedSourceKey(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t)
	createBucket(ctx, t, env.Client, bucket)

	const source = "copy me/café + 100%.txt"
	const destination = "copied/destination.txt"
	body := []byte("copy me body")
	if _, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(source),
		Body:   bytes.NewReader(body),
	}); err != nil {
		t.Fatalf("PutObject(%q): %v", source, err)
	}

	// The SDK writes x-amz-copy-source verbatim and its documentation says the
	// value must be URL-encoded, so the caller encodes it: each segment escaped,
	// the separators left alone. This case first failed with "Invalid copy source"
	// because it passed the key through raw and the server's PathUnescape rejected
	// the bare "100%" as a malformed escape. The server was right and the test was
	// wrong, which is worth recording because the wrong fix here - making the
	// server tolerate an unencoded source - would accept a key it could not
	// round-trip.
	if _, err := env.Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(destination),
		CopySource: aws.String(bucket + "/" + escapeKeyPath(source)),
	}); err != nil {
		t.Fatalf("CopyObject from %q: %v", source, err)
	}

	out, err := env.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(destination),
	})
	if err != nil {
		t.Fatalf("GetObject(%q): %v", destination, err)
	}
	defer out.Body.Close()
	got, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "copy me body") {
		t.Fatalf("copied body = %q, want the source bytes", got)
	}
}
