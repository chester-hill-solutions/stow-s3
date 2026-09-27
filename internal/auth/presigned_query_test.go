package auth_test

// Presigned-query ambiguity: the authentication parameters have exactly one
// interpretation, and anything else is refused.
//
// Separate from the rest of the strictness cases because the ambiguity here is
// between two readings of the same query string — the one that was signed and the
// one the verifier consumes — rather than between the client's intent and stow's
// parse of a header.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

// Presigned authentication parameters get one deterministic interpretation.
//
// The signed query and the values consumed here are two readings of the same
// string, so a query carrying two spellings of one parameter can be signed as one
// and read as the other. Each case below is refused rather than resolved: there is
// no correct choice between two claims, and picking one would make what stow
// authenticates depend on iteration order.
func TestPresignedAuthenticationParametersAreUnambiguous(t *testing.T) {
	reference := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	verifier := &auth.Verifier{Region: "us-east-1", MaxSkew: 15 * time.Minute}
	base := presignedAt(t, reference, 3600)

	cases := []struct {
		name   string
		mutate func(t *testing.T, rawURL string) string
	}{
		{
			name: "a differently cased X-Amz-Date",
			mutate: func(t *testing.T, rawURL string) string {
				// The canonical parameter plus a lower-case duplicate carrying a
				// different value. Either reading leaves the query ambiguous.
				parsed, _ := url.Parse(rawURL)
				existing := parsed.Query().Get("X-Amz-Date")
				parsed.RawQuery = parsed.RawQuery + "&x-amz-date=" + url.QueryEscape(fmt.Sprintf("%s00", existing))
				return parsed.String()
			},
		},
		{
			name: "a differently cased X-Amz-Signature",
			mutate: func(t *testing.T, rawURL string) string {
				parsed, _ := url.Parse(rawURL)
				existing := parsed.Query().Get("X-Amz-Signature")
				parsed.RawQuery = parsed.RawQuery + "&x-amz-signature=" + url.QueryEscape(strings.Repeat("cd", 32))
				_ = existing
				return parsed.String()
			},
		},
		{
			name: "a differently cased X-Amz-Credential",
			mutate: func(t *testing.T, rawURL string) string {
				parsed, _ := url.Parse(rawURL)
				parsed.RawQuery = parsed.RawQuery + "&x-amz-credential=" + url.QueryEscape(strictCreds.AccessKeyID+"/20260301/us-east-1/s3/aws4_request")
				return parsed.String()
			},
		},
		{
			name: "a repeated X-Amz-Expires",
			mutate: func(t *testing.T, rawURL string) string {
				return appendQueryParam(t, rawURL, "X-Amz-Expires", "60")
			},
		},
		{
			name: "a repeated X-Amz-SignedHeaders",
			mutate: func(t *testing.T, rawURL string) string {
				return appendQueryParam(t, rawURL, "X-Amz-SignedHeaders", "host")
			},
		},
		{
			name: "a repeated X-Amz-Signature",
			mutate: func(t *testing.T, rawURL string) string {
				return appendQueryParam(t, rawURL, "X-Amz-Signature", strings.Repeat("ef", 32))
			},
		},
		{
			name: "a repeated X-Amz-Date",
			mutate: func(t *testing.T, rawURL string) string {
				return appendQueryParam(t, rawURL, "X-Amz-Date", "20260301T120000Z")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptestRequest(t, http.MethodGet, tc.mutate(t, base), nil)
			req.Header.Set("Host", "127.0.0.1:9000")
			err := verifier.AuthenticateAt(req, strictCreds, reference)
			if err == nil {
				t.Fatal("a query with an ambiguous authentication parameter was accepted")
			}
			var authErr *auth.Error
			if !errors.As(err, &authErr) || authErr.Code != "AccessDenied" {
				t.Fatalf("error = %v, want AccessDenied", err)
			}
		})
	}

	// The same URL without the extra parameter still authenticates, so the
	// refusal above is about the ambiguity rather than about the URL.
	req := httptestRequest(t, http.MethodGet, base, nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	if err := verifier.AuthenticateAt(req, strictCreds, reference); err != nil {
		t.Fatalf("the unambiguous URL was refused: %v", err)
	}
}
