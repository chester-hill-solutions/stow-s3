package auth_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

// A signed header can carry more than one value, and the signature covers all
// of them. The AWS SDK signs the whole set, so a request it signed has to
// verify here too, and a request whose set changed after signing must not.
func TestARepeatedSignedHeaderAuthenticates(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		values []string
	}{
		{"single", []string{"first"}},
		{"repeated", []string{"first", "second"}},
		{"three", []string{"first", "second", "third"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := sdkSignedRequest(t, now, test.values)
			if err := auth.NewVerifier("us-east-1").AuthenticateAt(req, strictCreds, now); err != nil {
				t.Fatalf("a valid SDK signature over %d header values was refused: %v", len(test.values), err)
			}
		})
	}
}

// A signature covers the values that were signed. Adding or changing a value
// afterwards changes the request without changing the signature, so it must be
// refused rather than accepted on the strength of the first value.
func TestChangingASignedHeaderValueWithoutResigningIsRefused(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		change func(req *http.Request)
	}{
		{"appended", func(req *http.Request) { req.Header.Add("X-Amz-Meta-Label", "unsigned second") }},
		{"changed", func(req *http.Request) { req.Header.Set("X-Amz-Meta-Label", "changed") }},
		{"removed", func(req *http.Request) { req.Header.Del("X-Amz-Meta-Label") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := sdkSignedRequest(t, now, []string{"first"})
			verifier := auth.NewVerifier("us-east-1")
			if err := verifier.AuthenticateAt(req, strictCreds, now); err != nil {
				t.Fatalf("the request was refused before it was changed: %v", err)
			}
			test.change(req)
			if err := verifier.AuthenticateAt(req, strictCreds, now); err == nil {
				t.Fatal("a signed header changed after signing authenticated")
			}
		})
	}
}

// The values are joined after each one is normalized, so the whitespace inside
// a value does not change the signature and the whitespace between values
// does not either.
func TestRepeatedSignedHeaderValuesAreNormalizedPerValue(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	req := sdkSignedRequest(t, now, []string{"  spaced  value  "})
	if err := auth.NewVerifier("us-east-1").AuthenticateAt(req, strictCreds, now); err != nil {
		t.Fatalf("a valid SDK signature over a padded header value was refused: %v", err)
	}
}

// sdkSignedRequest builds a request the pinned AWS SDK signs over the given
// header values. The SDK is the reference: its signature is what a real client
// produces.
func sdkSignedRequest(t *testing.T, now time.Time, values []string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9000/demo/key", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	for _, value := range values {
		req.Header.Add("X-Amz-Meta-Label", value)
	}
	credentials := aws.Credentials{
		AccessKeyID:     strictCreds.AccessKeyID,
		SecretAccessKey: strictCreds.SecretAccessKey,
	}
	if err := v4.NewSigner().SignHTTP(context.Background(), credentials, req, "UNSIGNED-PAYLOAD", "s3", "us-east-1", now); err != nil {
		t.Fatal(err)
	}
	return req
}
