package conformance_test

// Presigned URLs at the wire, with real SigV4 signatures.
//
// The properties here are about the verifier's reading of a URL rather than the
// storage behind it, which is why they are separate from the round-trip cases.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestPresignedGetPut(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t)
	getKey := "presign-get.txt"
	putKey := "presign-put.txt"
	createBucket(ctx, t, env.Client, bucket)

	seed := []byte("presigned download")
	_, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(getKey),
		Body:   bytes.NewReader(seed),
	})
	if err != nil {
		t.Fatalf("seed PutObject: %v", err)
	}

	getReq, err := env.Presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(getKey),
	}, s3.WithPresignExpires(5*time.Minute))
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}

	resp, err := http.Get(getReq.URL)
	if err != nil {
		t.Fatalf("presigned GET fetch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned GET status = %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, seed) {
		t.Fatalf("presigned GET body = %q", got)
	}

	putBody := []byte("presigned upload")
	putReq, err := env.Presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(putKey),
		ContentType: aws.String("text/plain"),
	}, s3.WithPresignExpires(5*time.Minute))
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}

	putHTTP, err := http.NewRequestWithContext(ctx, http.MethodPut, putReq.URL, bytes.NewReader(putBody))
	if err != nil {
		t.Fatalf("put request: %v", err)
	}
	putHTTP.Header.Set("Content-Type", "text/plain")
	putHTTP.ContentLength = int64(len(putBody))
	putResp, err := http.DefaultClient.Do(putHTTP)
	if err != nil {
		t.Fatalf("presigned PUT: %v", err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("presigned PUT status = %d", putResp.StatusCode)
	}

	_, err = env.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(putKey),
	})
	if err != nil {
		t.Fatalf("HeadObject after presigned PUT: %v", err)
	}
}

// A presigned URL is refused when its signing time is far ahead of the server's
// clock, at the wire and with a real SDK's signature.
//
// The signing time is inside the URL, so it is the client's to choose, and the
// URL's own expiry is derived from it: a URL signed for a date next year expires
// next year, so an expiry check alone never noticed. Internal auth covers the
// verifier's side of this; what is asserted here is that the property holds
// through a genuine SigV4 signature, because a signature over a future-dated
// request is computed from that same future date and has to be refused on the
// date rather than on the signature.
//
// The signing time is moved rather than the server's clock, because a
// determinate result is worth more than a real one: the skew is 15 minutes, so
// the shift has to be comfortably larger than that to mean anything.
func TestPresignedURLIsRefusedFarBeforeItsSigningTime(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	bucket := uniqueBucket(t, "presigned-skew")
	key := "object.txt"
	createBucket(ctx, t, env.Client, bucket)
	if _, err := env.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader("body"),
	}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	// Signed an hour from now, for five minutes. Its own window is entirely in the
	// future, so nothing about it has expired. The signature is a real one, computed
	// at that future time — otherwise the request would be refused for a mismatched
	// signature and the case would prove nothing about the time.
	signed, err := presignAt(t, env, objectLocation{bucket, key}, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("presign at a future time: %v", err)
	}
	resp, err := http.Get(signed)
	if err != nil {
		t.Fatalf("presigned GET fetch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a presigned URL signed an hour ahead was accepted")
	}
	// The whole point is that the refusal is about the time and not the signature:
	// a valid signature over a far-future date is what a client could produce, and
	// a SignatureDoesNotMatch would mean the URL never got as far as being checked.
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "RequestTimeTooSkewed") {
		t.Fatalf("status %d, body %q; want RequestTimeTooSkewed", resp.StatusCode, body)
	}
}

// objectLocation names one object, so a helper that needs a bucket, a key and an
// environment does not need five parameters.
type objectLocation struct {
	bucket string
	key    string
}

// presignAt produces a presigned GET URL for one object, signed at a chosen time.
//
// The SDK's presigner signs with its own clock, so a future signing time needs
// the signer directly. It is the same signer the presigner uses, which is what
// makes the resulting URL a real one rather than a plausible-looking forgery.
func presignAt(t *testing.T, env *testEnv, ref objectLocation, signingTime time.Time) (string, error) {
	t.Helper()
	creds := env.Creds
	req, err := http.NewRequest(http.MethodGet, env.Endpoint+"/"+ref.bucket+"/"+ref.key, nil)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(env.Endpoint)
	if err != nil {
		return "", err
	}
	req.Host = parsed.Host
	req.URL.Host = parsed.Host
	req.Header.Set("Host", parsed.Host)
	// X-Amz-Expires is signed like any other query parameter, so it has to be on
	// the request before it is signed rather than added afterwards. The signer
	// does not set it itself; the S3 presigner does, and this is that same step.
	query := req.URL.Query()
	query.Set("X-Amz-Expires", "300")
	req.URL.RawQuery = query.Encode()

	signer := v4.NewSigner()
	signedURL, _, err := signer.PresignHTTP(
		context.Background(),
		aws.Credentials{AccessKeyID: creds.AccessKeyID, SecretAccessKey: creds.SecretAccessKey},
		req,
		"UNSIGNED-PAYLOAD",
		"s3",
		testRegion,
		signingTime,
	)
	return signedURL, err
}
